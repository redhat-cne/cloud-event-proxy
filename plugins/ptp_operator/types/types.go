package types

import (
	"fmt"

	"github.com/redhat-cne/sdk-go/pkg/event/ptp"
	"github.com/redhat-cne/sdk-go/pkg/pubsub"
)

type (
	// ProcessName ... process name either ptp4l or phc2sys
	ProcessName string
	// PtpPortRole ...ptp port role
	PtpPortRole int
	// IFace ... network interface name
	IFace string
	// ConfigName ... config name
	ConfigName string
)

const (
	// PASSIVE when two slave are configure other will be passive
	PASSIVE PtpPortRole = iota
	// SLAVE interface
	SLAVE
	// MASTER interface
	MASTER
	// FAULTY Interface role
	FAULTY
	// UNKNOWN role
	UNKNOWN
)

// PtpRoleMappings ... set ptp role mapping
var PtpRoleMappings = map[string]PtpPortRole{
	"PASSIVE": PASSIVE,
	"SLAVE":   SLAVE,
	"MASTER":  MASTER,
	"FAULTY":  FAULTY,
	"UNKNOWN": UNKNOWN,
}

func (r PtpPortRole) String() string {
	switch r {
	case PASSIVE:
		return "PASSIVE"
	case SLAVE:
		return "SLAVE"
	case MASTER:
		return "MASTER"
	case FAULTY:
		return "FAULTY"
	case UNKNOWN:
		return "UNKNOWN"
	default:
		return fmt.Sprintf("%d", int(r))
	}
}

// EventPublisherType ... define types of publishers
type EventPublisherType struct {
	EventType ptp.EventType
	Resource  ptp.EventResource
	PubID     string
	Pub       *pubsub.PubSub
}
