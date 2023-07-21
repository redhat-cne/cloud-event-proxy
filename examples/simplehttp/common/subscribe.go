package common

import (
	"github.com/google/uuid"
	"github.com/redhat-cne/sdk-go/pkg/channel"
	ptpEvent "github.com/redhat-cne/sdk-go/pkg/event/ptp"
	"github.com/redhat-cne/sdk-go/pkg/pubsub"
	"github.com/redhat-cne/sdk-go/pkg/subscriber"
	log "github.com/sirupsen/logrus"
)

// GetResources ... get resource types
func GetResources() map[string]string {
	subscribeTo := make(map[string]string)
	subscribeTo[string(ptpEvent.OsClockSyncStateChange)] = string(ptpEvent.OsClockSyncState)
	subscribeTo[string(ptpEvent.PtpClockClassChange)] = string(ptpEvent.PtpClockClass)
	subscribeTo[string(ptpEvent.PtpStateChange)] = string(ptpEvent.PtpLockState)
	return subscribeTo
}

// Subscribe create subscription
func Subscribe(clientID uuid.UUID, subs []pubsub.PubSub, nodeName, publisherURL, returnEndPoint string) error {
	// Post it to the address that has been specified : to target URL
	eventSubscriber := subscriber.New(clientID)
	//Self URL
	_ = eventSubscriber.SetEndPointURI(returnEndPoint) // where you want events to be posted
	eventSubscriber.Action = channel.NEW               //0=new
	// create a subscriber model
	eventSubscriber.AddSubscription(subs...)
	log.Infof("subscription data%s", eventSubscriber.String())
	ce, _ := eventSubscriber.CreateCloudEvents()
	ce.SetSubject(channel.NEW.String())
	ce.SetSource(returnEndPoint)
	log.Infof("posting %s for node %s", ce.String(), nodeName)
	_, err := Post(publisherURL, *ce)
	return err
}

// DeleteSubscription ... delete all subscription data
func DeleteSubscription(publisherURL string, clientID uuid.UUID) error {
	// Post it to the address that has been specified : to target URL
	log.Infof("Deleting subscriptions for client %s", clientID.String())
	eventSubscriber := subscriber.New(clientID)
	eventSubscriber.Action = channel.DELETE //2 == delete
	eventSubscriber.Status = subscriber.InActive
	ce, _ := eventSubscriber.CreateCloudEvents()
	ce.SetSubject(channel.DELETE.String())
	_, err := Post(publisherURL, *ce)
	return err
}
