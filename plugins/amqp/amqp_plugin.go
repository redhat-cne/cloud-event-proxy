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

// Package main ...
package main

import (
	"sync"
	"time"

	"github.com/redhat-cne/cloud-event-proxy/pkg/common"

	v1amqp "github.com/redhat-cne/sdk-go/v1/amqp"
)

// Start amqp  services to process events,metrics and status
func Start(wg *sync.WaitGroup, config *common.SCConfiguration, amqInitTimeout time.Duration) (amqpInstance *v1amqp.AMQP, err error) { //nolint:deadcode,unused
	if amqpInstance, err = v1amqp.GetAMQPInstance(config.TransportHost.URL, config.EventInCh, config.EventOutCh, config.CloseCh, amqInitTimeout); err != nil {
		return
	}
	amqpInstance.Router.CancelTimeOut(2 * time.Second)
	amqpInstance.Router.RetryTime(1 * time.Second)
	amqpInstance.Start(wg)
	return
}
