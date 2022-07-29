package subscriber

import (
	"fmt"
	"github.com/redhat-cne/sdk-go/pkg/pubsub"
	"github.com/redhat-cne/sdk-go/pkg/store"
	"github.com/redhat-cne/sdk-go/pkg/types"
	"net/url"
	"strings"
)

var _ Writer = (*Subscriber)(nil)

// SetClientID  ...
func (s *Subscriber) SetClientID(clientID string) {
	s.ClientID = clientID
}

// SetSubStore ...
func (s *Subscriber) SetSubStore(store store.PubSubStore) {
	s.SubStore = &store
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
		newS := &pubsub.PubSub{
			ID:          ss.GetID(),
			EndPointURI: nil,
			URILocation: nil,
			Resource:    ss.Resource,
		}
		s.SubStore.Store[ss.GetID()] = newS
	}
}
