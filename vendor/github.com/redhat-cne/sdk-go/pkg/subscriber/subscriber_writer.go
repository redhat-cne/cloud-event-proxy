package subscriber

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/google/uuid"

	"github.com/redhat-cne/sdk-go/pkg/pubsub"
	"github.com/redhat-cne/sdk-go/pkg/types"
)

var _ Writer = (*Subscriber)(nil)

// SetClientID  ...
func (s *Subscriber) SetClientID(clientID uuid.UUID) {
	s.ClientID = clientID
}

// SetEndPointURI set uri location  (return url)
func (s *Subscriber) SetEndPointURI(endPointURI string) error {
	sEndPointURI := strings.TrimSpace(endPointURI)
	if sEndPointURI == "" {
		s.EndPointURI = nil
		err := fmt.Errorf("EndPointURI is given empty string,should be valid url")
		return err
	}
	endPointURL, err := url.Parse(sEndPointURI)
	if err != nil {
		return err
	}
	s.EndPointURI = &types.URI{URL: *endPointURL}
	return nil
}

// SetStatus set status of the connection
func (s *Subscriber) SetStatus(status Status) {
	s.Status = status
}

// AddSubscription ...
func (s *Subscriber) AddSubscription(subs ...pubsub.PubSub) {
	for _, ss := range subs {
		s.SubStore.Store[ss.GetID()] = &ss
	}
}
