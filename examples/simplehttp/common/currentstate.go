package common

import (
	"fmt"

	"github.com/google/uuid"
)

const (
	// CURRENTSTATE  resource path name
	CURRENTSTATE = "CurrentState"
)

// GetCurrentState ... get current ptp event state
func GetCurrentState(clientID uuid.UUID, publisherURL, resourceAddress string) ([]byte, int, error) {
	stateURL := fmt.Sprintf("%s%s/%s/%s", publisherURL, resourceAddress, clientID, CURRENTSTATE)
	return GetByte(stateURL)
}
