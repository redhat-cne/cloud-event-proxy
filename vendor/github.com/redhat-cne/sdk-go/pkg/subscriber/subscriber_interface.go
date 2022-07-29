package subscriber

import (
	"github.com/redhat-cne/sdk-go/pkg/pubsub"
	"github.com/redhat-cne/sdk-go/pkg/store"
)

// Copyright 2022 The Cloud Native Events Authors
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

// Reader is the interface for reading through an event from attributes.
type Reader interface {
	// GetClientID returns event.GetResource()
	GetClientID() string
	// GetStatus Get Status of teh subcribers
	GetStatus() Status
	// String returns a pretty-printed representation of the PubSub.
	String() string
	// GetSubStore return pubsub sdata
	GetSubStore() *store.PubSubStore
	// EndPointURI return   endpoint
	GetEndPointURI() string
}

// Writer is the interface for writing through an event onto attributes.
// If an error is thrown by a sub-component, Writer caches the error
// internally and exposes errors with a call to Writer.Validate().
type Writer interface {
	// Resource performs event.SetResource()
	SetClientID(string)

	// SetID performs event.SetID.
	SetStatus(status Status)
	// SetSubStore persist the store data
	SetSubStore(store store.PubSubStore)

	// SetHealthEndPoint set health endpoint
	SetEndPointURI(url string) error

	AddSubscription(sub ...pubsub.PubSub)
}
