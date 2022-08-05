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
	"sync"
	"sync/atomic"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/redhat-cne/sdk-go/pkg/channel"
	"github.com/redhat-cne/sdk-go/pkg/errorhandler"
	cneHTTP "github.com/redhat-cne/sdk-go/pkg/protocol/http"
)

var (
	instance *HTTP
	once     sync.Once
)

//HTTP exposes http api methods
type HTTP struct {
	server  *cneHTTP.Server
	started *int32 // accessed with atomics
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
	if h.started != nil { // protection for starting by other instances
		atomic.AddInt32(h.started, 1)
		go instance.server.Start(wg)
		go instance.server.HTTPProcessor(wg)
	}

}

// Shutdown ...
func (h *HTTP) Shutdown() {
	h.server.Shutdown()
	atomic.AddInt32(h.started, 0)
}

// RegisterPublisher ...
func (h *HTTP) RegisterPublisher(publisherURL ...string) {
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
