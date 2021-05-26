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

package main_test

import (
	"os"
	"sync"
	"testing"

	"github.com/redhat-cne/cloud-event-proxy/pkg/common"
	restapi "github.com/redhat-cne/rest-api"
	"github.com/redhat-cne/sdk-go/pkg/channel"

	v1pubsub "github.com/redhat-cne/sdk-go/v1/pubsub"
)

var (
	wg                    sync.WaitGroup
	server                *restapi.Server
	scConfig              *common.SCConfiguration
	channelBufferSize     int    = 10
	storePath                    = "../../.."
	resourceAddress       string = "/cluster/node/hw"
	resourceStatusAddress string = "/cluster/node/hw/status"
	apiPort               int    = 8991
)

func TestMain(m *testing.M) {
	scConfig = &common.SCConfiguration{
		EventInCh:  make(chan *channel.DataChan, channelBufferSize),
		EventOutCh: make(chan *channel.DataChan, channelBufferSize),
		CloseCh:    make(chan struct{}),
		APIPort:    apiPort,
		APIPath:    "/api/test-cloud/",
		PubSubAPI:  v1pubsub.GetAPIInstance(storePath),
		StorePath:  storePath,
		AMQPHost:   "amqp:localhost:5672",
		BaseURL:    nil,
	}

	server, _ = common.StartPubSubService(&wg, scConfig)
	scConfig.APIPort = server.Port()
	scConfig.BaseURL = server.GetHostPath()
	cleanUP()
	os.Exit(m.Run())
}
func cleanUP() {
	_ = scConfig.PubSubAPI.DeleteAllPublishers()
	_ = scConfig.PubSubAPI.DeleteAllSubscriptions()
}
