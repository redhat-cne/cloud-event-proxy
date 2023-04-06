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

	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/redhat-cne/cloud-event-proxy/pkg/common"
	"github.com/redhat-cne/sdk-go/pkg/channel"
	v1http "github.com/redhat-cne/sdk-go/v1/http"
)

// Start http transport services to process events,metrics and status
func Start(wg *sync.WaitGroup, config *common.SCConfiguration, onStatusReceiveOverrideFn func(e cloudevents.Event, dataChan *channel.DataChan) error, processEventFn func(e interface{}) error) (httpInstance *v1http.HTTP, err error) { //nolint:deadcode,unused
	if httpInstance, err = v1http.GetHTTPInstance(config.TransportHost.URI.String(), config.TransportHost.Port, config.StorePath,
		config.EventInCh, config.EventOutCh, config.CloseCh, onStatusReceiveOverrideFn, processEventFn); err != nil {
		return
	}
	_ = config.SetClientID(httpInstance.ClientID())
	httpInstance.Start(wg)
	return
}
