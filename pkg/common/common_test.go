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

package common_test

import (
	"net"
	"testing"
	"time"

	"github.com/redhat-cne/sdk-go/pkg/channel"
	ceevent "github.com/redhat-cne/sdk-go/pkg/event"
	"github.com/redhat-cne/sdk-go/pkg/types"
	v1pubsub "github.com/redhat-cne/sdk-go/v1/pubsub"
	log "github.com/sirupsen/logrus"

	"github.com/redhat-cne/cloud-event-proxy/pkg/common"
	"github.com/stretchr/testify/assert"
)

func TestTransportHost_ParseTransportHost(t *testing.T) {
	type test struct {
		input *common.TransportHost
		desc  string
		want  common.TransportHost
	}
	tests := []test{
		{input: &common.TransportHost{URL: "localhost:5672"}, desc: "valid http", want: common.TransportHost{Type: common.HTTP, URL: "localhost:5672", Scheme: "http", Port: 5672, URI: types.ParseURI("http://localhost:5672"), Host: "localhost", Err: nil}},
		{input: &common.TransportHost{URL: "invalid"}, desc: "invalid http", want: common.TransportHost{Type: common.UNKNOWN, URL: "invalid", URI: types.ParseURI("http://invalid"), Scheme: "http", Err: &net.AddrError{
			Err:  "missing port in address",
			Addr: "invalid",
		}}},
		{input: &common.TransportHost{URL: "cluster.app.servicename:8080"}, desc: "valid http", want: common.TransportHost{Type: common.HTTP, URL: "cluster.app.servicename:8080", URI: types.ParseURI("http://cluster.app.servicename:8080"),
			Scheme: "http", Port: 8080, Host: "cluster.app.servicename"}},
		{input: &common.TransportHost{URL: "http://cluster.app.servicename:8080"}, desc: "valid http", want: common.TransportHost{Type: common.HTTP, URL: "http://cluster.app.servicename:8080",
			URI: types.ParseURI("http://cluster.app.servicename:8080"), Scheme: "http", Port: 8080, Host: "cluster.app.servicename"}},
		{input: &common.TransportHost{URL: "http://ptp.event.publisher.svc.cluster.local:9043"}, desc: "valid http", want: common.TransportHost{Type: common.HTTP, URL: "http://ptp.event.publisher.svc.cluster.local:9043",
			URI: types.ParseURI("http://ptp.event.publisher.svc.cluster.local:9043"), Scheme: "http", Port: 9043, Host: "ptp.event.publisher.svc.cluster.local"}},
	}

	for _, tc := range tests {
		tc.input.ParseTransportHost()
		log.Infof("testing %s", tc.desc)
		assert.Equal(t, tc.want.Host, tc.input.Host, tc.input.String())
		assert.Equal(t, tc.want.URL, tc.input.URL, tc.input.String())
		assert.Equal(t, tc.want.Port, tc.input.Port, tc.input.String())
		assert.Equal(t, tc.want.Scheme, tc.input.Scheme, tc.input.String())
		assert.Equal(t, tc.want.URI.String(), tc.input.URI.String())
		if tc.want.Err != nil {
			assert.Equal(t, tc.want.Err.Error(), tc.input.Err.Error(), tc.input.String())
		} else {
			assert.Nil(t, tc.want.Err, tc.input.Err, tc.input.String())
		}
	}
}

func TestPublishEventViaAPI_NonBlockingWhenChannelFull(t *testing.T) {
	// Create a channel with buffer size 1 and fill it
	eventOutCh := make(chan *channel.DataChan, 1)
	eventOutCh <- &channel.DataChan{} // fill the buffer

	pubSubAPI := v1pubsub.GetAPIInstance("/tmp/test-store")
	pub, _ := pubSubAPI.CreatePublisher(v1pubsub.NewPubSub(
		types.ParseURI("http://localhost/dummy"),
		"/test/resource",
	))

	scConfig := &common.SCConfiguration{
		EventOutCh: eventOutCh,
		PubSubAPI:  pubSubAPI,
	}

	// Create event with matching publisher ID
	event := ceevent.Event{ID: pub.ID}

	// PublishEventViaAPI should return after the 5s timeout (not block forever)
	// even though the channel is full
	done := make(chan struct{})
	go func() {
		_ = common.PublishEventViaAPI(scConfig, event, "/test/resource")
		close(done)
	}()

	select {
	case <-done:
		// success — returned after timeout, did not block forever
	case <-time.After(10 * time.Second):
		t.Fatal("PublishEventViaAPI blocked on full EventOutCh — should return after 5s timeout")
	}

	// Channel should still have exactly 1 item (the original, not the new one)
	assert.Equal(t, 1, len(eventOutCh))
}
