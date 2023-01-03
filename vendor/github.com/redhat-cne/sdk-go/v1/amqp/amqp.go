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

package amqp

import (
	"context"
	"sync"
	"time"

	"github.com/Azure/go-amqp"
	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/redhat-cne/sdk-go/pkg/channel"
	"github.com/redhat-cne/sdk-go/pkg/errorhandler"
	amqp1 "github.com/redhat-cne/sdk-go/pkg/protocol/amqp"
	log "github.com/sirupsen/logrus"
)

var (
	instance      *AMQP
	retryTimeout  = 500 * time.Millisecond
	cancelTimeout = 300 * time.Second
)

// AMQP exposes amqp api methods
type AMQP struct {
	Router *amqp1.Router
}

// GetAMQPInstance get event instance
func GetAMQPInstance(amqpHost string, dataIn <-chan *channel.DataChan, dataOut chan<- *channel.DataChan, closeCh <-chan struct{}) (*AMQP, error) {
	ctx, cancel := context.WithTimeout(context.Background(), cancelTimeout)
	defer cancel()
	var router *amqp1.Router
	var err error
	for {
		select {
		case <-ctx.Done():
			return nil, errorhandler.AMQPConnectionError{}
		default:
		}
		router, err = amqp1.InitServer(amqpHost, dataIn, dataOut, closeCh)
		if err != nil {
			log.Info("retrying connecting to amqp.")
			time.Sleep(retryTimeout)
			continue
		}

		instance = &AMQP{
			Router: router,
		}
		break
	}
	if instance == nil || instance.Router == nil {
		return nil, errorhandler.AMQPConnectionError{}
	}

	if instance.Router.Client == nil {
		client, err := instance.Router.NewClient(amqpHost, []amqp.ConnOption{})
		if err != nil {
			return nil, errorhandler.AMQPConnectionError{Desc: err.Error()}
		}
		instance.Router.Client = client
	}
	return instance, nil
}

// Start start amqp processors
func (a *AMQP) Start(wg *sync.WaitGroup) {
	go instance.Router.QDRRouter(wg)
}

// NewSender - create new sender independent of the framework
func NewSender(hostName string, port int, address string) (*amqp1.Protocol, error) {
	return amqp1.NewSender(hostName, port, address)
}

// NewReceiver create new receiver independent of the framework
func NewReceiver(hostName string, port int, address string) (*amqp1.Protocol, error) {
	return amqp1.NewReceiver(hostName, port, address)
}

// DeleteSender send publisher address information  on a channel to delete its sender object
func DeleteSender(inChan chan<- *channel.DataChan, address string) {
	// go ahead and create QDR to this address
	inChan <- &channel.DataChan{
		Address: address,
		Type:    channel.PUBLISHER,
		Status:  channel.DELETE,
	}
}

// CreateSender send publisher address information  on a channel to create it's sender object
func CreateSender(inChan chan<- *channel.DataChan, address string) {
	// go ahead and create QDR to this address
	inChan <- &channel.DataChan{
		Address: address,
		Type:    channel.PUBLISHER,
		Status:  channel.NEW,
	}
}

// DeleteListener send subscription address information  on a channel to delete its listener object
func DeleteListener(inChan chan<- *channel.DataChan, address string) {
	// go ahead and create QDR listener to this address
	inChan <- &channel.DataChan{
		Address: address,
		Type:    channel.SUBSCRIBER,
		Status:  channel.DELETE,
	}
}

// CreateListener send subscription address information  on a channel to create its listener object
func CreateListener(inChan chan<- *channel.DataChan, address string) {
	// go ahead and create QDR listener to this address
	inChan <- &channel.DataChan{
		Address: address,
		Type:    channel.SUBSCRIBER,
		Status:  channel.NEW,
	}
}

// CreateNewStatusListener send status address information  on a channel to create it's listener object
func CreateNewStatusListener(inChan chan<- *channel.DataChan, address string,
	onReceiveOverrideFn func(e cloudevents.Event, dataChan *channel.DataChan) error,
	processEventFn func(e interface{}) error) {
	// go ahead and create QDR listener to this address
	inChan <- &channel.DataChan{
		Address:             address,
		Data:                nil,
		Status:              channel.NEW,
		Type:                channel.SUBSCRIBER,
		OnReceiveOverrideFn: onReceiveOverrideFn,
		ProcessEventFn:      processEventFn,
	}
}
