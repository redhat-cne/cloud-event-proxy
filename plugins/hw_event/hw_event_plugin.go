// Copyright 2021 The Cloud Native Events Authors
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

package main

import (
	"sync"

	"github.com/redhat-cne/cloud-event-proxy/pkg/common"
	hwproxy "github.com/redhat-cne/hw-event-proxy/hw-event-proxy"
	log "github.com/sirupsen/logrus"
)

// Start hw event plugin to process events,metrics and status, expects rest api available to create publisher and subscriptions
func Start(wg *sync.WaitGroup, config *common.SCConfiguration, fn func(e interface{}) error) error { //nolint:deadcode,unused
	scConfig := hwproxy.SCConfiguration{}
	scConfig.EventInCh = config.EventInCh
	scConfig.PubSubAPI = config.PubSubAPI
	scConfig.BaseURL = config.BaseURL

	err := hwproxy.Start(wg, &scConfig, fn) //nocheck
	if err != nil {
		log.Errorf("failed to start hw-event-proxy: %v", err)
		return err
	}
	return nil
}
