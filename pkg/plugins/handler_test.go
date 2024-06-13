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

//go:build unittests
// +build unittests

package plugins

import (
	"fmt"
	"sync"
	"testing"

	"os"

	"github.com/redhat-cne/cloud-event-proxy/pkg/common"
	"github.com/redhat-cne/sdk-go/pkg/channel"
	v1pubsub "github.com/redhat-cne/sdk-go/v1/pubsub"
	"github.com/stretchr/testify/assert"
)

var (
	pLoader Handler = Handler{Path: "../../plugins"}

	scConfig          *common.SCConfiguration
	channelBufferSize int = 10
)

func init() {
	var storePath = "."
	if sPath, ok := os.LookupEnv("STORE_PATH"); ok && sPath != "" {
		storePath = sPath
	}

	scConfig = &common.SCConfiguration{
		EventInCh:  make(chan *channel.DataChan, channelBufferSize),
		EventOutCh: make(chan *channel.DataChan, channelBufferSize),
		CloseCh:    make(chan struct{}),
		APIPort:    8989,
		APIPath:    "/api/cne/",
		PubSubAPI:  v1pubsub.GetAPIInstance("../.."),
		StorePath:  storePath,
		BaseURL:    nil,
		TransportHost: &common.TransportHost{
			Type: 0,
			URL:  "",
			Host: "",
			Port: 0,
			Err:  nil,
		},
	}
}

func TestLoadPTPPlugin(t *testing.T) {
	os.Setenv("NODE_NAME", "test_node")
	scConfig.CloseCh = make(chan struct{})
	wg := &sync.WaitGroup{}
	_, err := common.StartPubSubService(scConfig)
	assert.Nil(t, err)

	testCases := map[string]struct {
		pgPath  string
		wantErr error
	}{
		"Invalid Plugin Path": {
			pgPath:  "wrong",
			wantErr: fmt.Errorf("ptp plugin not found in the path wrong"),
		},
		"Valid Plugin Path": {
			pgPath:  "../../plugins",
			wantErr: nil,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			pLoader = Handler{Path: tc.pgPath}
			err := pLoader.LoadPTPPlugin(wg, scConfig, nil)
			if tc.wantErr != nil && err != nil {
				assert.EqualError(t, err, tc.wantErr.Error())
			} else if tc.wantErr == nil {
				assert.Nil(t, err)
			}
		})
	}
}

func TestLoadHTTPPlugin(t *testing.T) {
	os.Setenv("NODE_NAME", "test_node")
	scConfig.CloseCh = make(chan struct{})
	wg := &sync.WaitGroup{}
	_, err := common.StartPubSubService(scConfig)
	assert.Nil(t, err)

	testCases := map[string]struct {
		pgPath  string
		wantErr error
	}{
		"Invalid Plugin Path": {
			pgPath:  "wrong",
			wantErr: fmt.Errorf("http plugin not found in the path wrong"),
		},
		"Valid Plugin Path": {
			pgPath:  "../../plugins",
			wantErr: nil,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			pLoader = Handler{Path: tc.pgPath}
			_, err := pLoader.LoadHTTPPlugin(wg, scConfig, nil, nil)
			if tc.wantErr != nil && err != nil {
				assert.EqualError(t, err, tc.wantErr.Error())
			} else if tc.wantErr == nil {
				assert.Nil(t, err)
			}
		})
	}
}

func Test_End(t *testing.T) {
	close(scConfig.CloseCh)
}
