// Copyright 2020 The Cloud Native Events Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package http

import (
	"fmt"
	"net/http"
	"path"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/redhat-cne/sdk-go/pkg/types"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/redhat-cne/sdk-go/pkg/channel"
	"github.com/redhat-cne/sdk-go/pkg/errorhandler"
	cneHTTP "github.com/redhat-cne/sdk-go/pkg/protocol/http"
	log "github.com/sirupsen/logrus"
)

var (
	instance *HTTP
	once     sync.Once
)

//HTTP exposes http api methods
type HTTP struct {
	server  *cneHTTP.Server
	started int32 // accessed with atomics
}

//GetHTTPInstance get event instance
func GetHTTPInstance(serviceName string, port int, storePath string, dataIn <-chan *channel.DataChan, dataOut chan<- *channel.DataChan,
	closeCh <-chan struct{}, onStatusReceiveOverrideFn func(e cloudevents.Event, dataChan *channel.DataChan) error, processEventFn func(e interface{}) error) (*HTTP, error) {
	once.Do(func() {
		server, err := cneHTTP.InitServer(serviceName, port, storePath, dataIn, dataOut, closeCh, onStatusReceiveOverrideFn, processEventFn)
		if err == nil {
			instance = &HTTP{
				server: server,
			}
		}
	})
	if instance == nil || instance.server == nil {
		return nil, errorhandler.HTTPConnectionError{Desc: "http connection error"}
	}

	return instance, nil
}

//Start start amqp processors
func (h *HTTP) Start(wg *sync.WaitGroup) {
	if atomic.CompareAndSwapInt32(&h.started, 0, 1) { // protection for starting by other instances
		log.Info("Starting http transport....")
		if err := h.server.Start(wg); err != nil {
			log.Errorf("failed to start http transport. implment re-connect")
			atomic.CompareAndSwapInt32(&h.started, 1, 0)
		}
		h.server.HTTPProcessor(wg)
	} else {
		log.Warn("http transport service is already running or couldn't start")
	}
}

// ClientID return clientID that was set by  publisher/subscriber service
func (h *HTTP) ClientID() uuid.UUID {
	if h.server != nil {
		return h.server.ClientID()
	}
	return uuid.Nil
}

// Shutdown ...
func (h *HTTP) Shutdown() {
	log.Warn("Shutting down http transport service")
	h.server.Shutdown()
	atomic.CompareAndSwapInt32(&h.started, 1, 0)
}

// RegisterPublishers ...
func (h *HTTP) RegisterPublishers(publisherURL ...*types.URI) {
	h.server.RegisterPublishers(publisherURL...)
}

// SetOnStatusReceiveOverrideFn ...
func (h *HTTP) SetOnStatusReceiveOverrideFn(fn func(e cloudevents.Event, dataChan *channel.DataChan) error) {
	h.server.SetOnStatusReceiveOverrideFn(fn)
}

// SetProcessEventFn ...
func (h *HTTP) SetProcessEventFn(fn func(e interface{}) error) {
	h.server.SetProcessEventFn(fn)
}

// IsReadyToServe ... check if the server is ready
func (h *HTTP) IsReadyToServe(uri *types.URI) bool {
	uri.Path = path.Join(uri.Path, "health")
	if ok, _ := httpTransportHealthCheck(uri, 2*time.Second); !ok {
		return false
	}
	return true
}

func httpTransportHealthCheck(uri *types.URI, delay time.Duration) (ok bool, err error) {
	log.Printf("checking for http transport health\n")
	for i := 0; i <= 5; i++ {
		log.Infof("health check %s ", uri.String())
		response, errResp := http.Get(uri.String())
		if errResp != nil {
			log.Warnf("try %d, return health check of the http transportfor error  %v", i, errResp)
			time.Sleep(delay)
			err = errResp
			continue
		}
		if response != nil && response.StatusCode == http.StatusOK {
			response.Body.Close()
			log.Info("http transport returned healthy status")
			time.Sleep(delay)
			err = nil
			ok = true
			return
		}
		response.Body.Close()
	}
	if err != nil {
		err = fmt.Errorf("error connecting to http transport %s", err.Error())
	}
	return
}

// DeleteSubscription ...
func DeleteSubscription(inChan chan<- *channel.DataChan, resource string) {
	inChan <- &channel.DataChan{
		Address: resource,
		Status:  channel.DELETE,
		Type:    channel.SUBSCRIBER,
	}
}

// CreateSubscription ...
func CreateSubscription(inChan chan<- *channel.DataChan, subscriptionID, resource string) {
	// create a subscription
	inChan <- &channel.DataChan{
		ID:      subscriptionID,
		Address: resource,
		Status:  channel.NEW,
		Type:    channel.SUBSCRIBER,
	}
}

// CreateStatusPing ...
func CreateStatusPing(inChan chan<- *channel.DataChan, resource string) {
	inChan <- &channel.DataChan{
		Address: resource,
		Status:  channel.NEW,
		Type:    channel.STATUS,
	}
}
