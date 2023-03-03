package common

import (
	"fmt"
	"strings"
	"time"

	cneevent "github.com/redhat-cne/sdk-go/pkg/event"
	"github.com/redhat-cne/sdk-go/pkg/event/ptp"
)

const (
	// ColorRed ...
	ColorRed = "\033[31m"
	// ColorGreen ...
	ColorGreen = "\033[32m"
	// ColorYellow ...
	ColorYellow = "\033[33m"
)

// PrintEvent ...
func PrintEvent(data *cneevent.Data, eventType string, eTime time.Time) {
	b := strings.Builder{}
	lockState := "LOCKED"
	b.WriteString("| ")
	b.WriteString(eTime.Format(time.RFC3339))
	var resourceAddress string
	if eventType == string(ptp.PtpClockClassChange) {
		b.WriteString(" | ")
		b.WriteString("CLOCK_CLASS")
	}

	for _, v := range data.Values {
		resourceAddress = v.GetResource()
		if v.DataType == cneevent.NOTIFICATION {
			lockState = fmt.Sprintf("%v", v.Value)
			b.WriteString(" | ")
			b.WriteString(fmt.Sprintf("%v", v.Value))
		} else if v.DataType == cneevent.METRIC {
			b.WriteString(" | ")
			b.WriteString(fmt.Sprintf("%v", v.Value))
		}
		if resourceAddress != v.GetResource() {
			b.WriteString(" | ")
			b.WriteString(eventType)
			b.WriteString(" | ")
			b.WriteString(resourceAddress)
			b.WriteString("\t")
			resourceAddress = ""
		}
	}
	if resourceAddress != "" {
		b.WriteString(" | ")
		b.WriteString(eventType + "\t")
		b.WriteString(" | ")
		b.WriteString(resourceAddress + "\t")
		b.WriteString("\t")
	}

	if lockState == string(ptp.LOCKED) {
		Print(ColorGreen, b.String())
	} else if lockState == string(ptp.HOLDOVER) {
		Print(ColorYellow, b.String())
	} else {
		Print(ColorRed, b.String())
	}
}

// Print ...
func Print(color string, data string) {
	fmt.Println(color, data)
}

// PrintHeader ...
func PrintHeader() {
	b := strings.Builder{}
	b.WriteString("Time\t\t\t |  State | Offset\\Class \t| Type\t|\t Resource\n")
	b.WriteString("---------------------------------------------------------------------------------------------\n")
	fmt.Println(ColorGreen, b.String())
}
