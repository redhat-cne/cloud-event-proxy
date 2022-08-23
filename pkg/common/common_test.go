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

	"github.com/redhat-cne/sdk-go/pkg/types"

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
		{input: &common.TransportHost{URL: "amqp:localhost:5671"}, desc: "valid amqp", want: common.TransportHost{Type: common.AMQ, URL: "amqp:localhost:5671", Host: "amqp:localhost:5671", Scheme: "amqp", Err: nil}},
		{input: &common.TransportHost{URL: "amqp://router.router.svc.cluster.local"}, desc: "valid amqp", want: common.TransportHost{Type: common.AMQ, URL: "amqp://router.router.svc.cluster.local",
			Host: "amqp://router.router.svc.cluster.local", Scheme: "amqp", Err: nil}},
		{input: &common.TransportHost{URL: "localhost:5672"}, desc: "valid http", want: common.TransportHost{Type: common.HTTP, URL: "localhost:5672", Scheme: "http", Port: 5672, URI: types.ParseURI("http://localhost:5672"), Host: "localhost", Err: nil}},
		{input: &common.TransportHost{URL: "invalid"}, desc: "invalid http/amqp", want: common.TransportHost{Type: common.UNKNOWN, URL: "invalid", URI: types.ParseURI("http://invalid"), Scheme: "http", Err: &net.AddrError{
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
