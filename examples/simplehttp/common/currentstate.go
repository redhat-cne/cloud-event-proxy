// Package common ...
package common

import (
	"fmt"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/google/uuid"
	cneevent "github.com/redhat-cne/sdk-go/pkg/event"
)

const (
	// CURRENTSTATE  resource path name
	CURRENTSTATE = "CurrentState"
)

// GetCurrentState ... get current ptp event state
func GetCurrentState(clientID uuid.UUID, publisherURL, resourceAddress string) (*cloudevents.Event, *cneevent.Data, error) {
	stateURL := fmt.Sprintf("%s%s/%s/%s", publisherURL, resourceAddress, clientID, CURRENTSTATE)
	return GetEventData(stateURL)
}
