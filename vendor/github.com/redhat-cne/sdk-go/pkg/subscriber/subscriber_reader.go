package subscriber

import (
	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/google/uuid"
	"github.com/redhat-cne/sdk-go/pkg/channel"
	"github.com/redhat-cne/sdk-go/pkg/store"
)

var _ Reader = (*Subscriber)(nil)

// GetClientID ... Get subscriber ClientID
func (s *Subscriber) GetClientID() uuid.UUID {
	return s.ClientID
}

// GetEndPointURI EndPointURI returns uri location
func (s *Subscriber) GetEndPointURI() string {
	return s.EndPointURI.String()
}

// GetStatus of the client connection
func (s *Subscriber) GetStatus() Status {
	return s.Status
}

// GetSubStore get subscription store
func (s *Subscriber) GetSubStore() *store.PubSubStore {
	return s.SubStore
}

// CreateCloudEvents ...
func (s *Subscriber) CreateCloudEvents() (*cloudevents.Event, error) {
	ce := cloudevents.NewEvent(cloudevents.VersionV03)
	ce.SetDataContentType(cloudevents.ApplicationJSON)
	ce.SetSpecVersion(cloudevents.VersionV03)
	ce.SetType(channel.SUBSCRIBER.String())
	ce.SetSource("subscription-request")
	ce.SetID(uuid.New().String())
	if err := ce.SetData(cloudevents.ApplicationJSON, s); err != nil {
		return nil, err
	}
	return &ce, nil
}
