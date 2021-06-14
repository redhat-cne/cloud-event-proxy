package event

import (
	"github.com/golang/glog"
	"strconv"
	"strings"
	ceevent "github.com/redhat-cne/sdk-go/pkg/event"
	"sync"
)

var lock = sync.RWMutex{}
var PTPStates map[string]ClockState


type ClockState struct {
	name string
	state ceevent.SyncState
	syncTime int
}
func( p *PTPStates) Read(name string) PtpClockState{
	return p
}
func( p *PTPStates) Write(name string, state ceevent.SyncState, sTime int){

}

func ExtractEvent(processName, output string) {
	var offsetThreshold float64 = 100
	offsetFromMaster, _, _, clockState := ExtractPhc2sysEvent(processName, output)
	switch clockState {
	case "s0":
		CreateEvent(ceevent.FREERUN)
	case "s1":
		CreateEvent(ceevent.FREERUN)
	case "s2", "s3":
		if offsetFromMaster >= offsetThreshold {
			CreateEvent(ceevent.HOLDOVER)
		} else {
			CreateEvent(ceevent.LOCKED)
		}
	}

}
func ExtractPhc2sysEvent(processName, output string) (offsetFromMaster, frequencyAdjustment, delayFromMaster float64, clockState string) {
	// This makes the out to equals
	//phc2sys[187248.740]: CLOCK_REALTIME phc offset        12 s2 freq   +6879 delay    49
	//ptp4l[3535499.740]: [ens5f0] master offset         -6 s2 freq   -1879 path delay        88

	output = strings.Replace(output, "path", "", 1)
	indx := strings.Index(output, "offset")
	output = output[indx:]
	fields := strings.Fields(output)

	if len(fields) < 5 {
		glog.Errorf("%s failed to parse output %s: unexpected number of fields", processName, output)
		return
	}

	offsetFromMaster, err := strconv.ParseFloat(fields[1], 64)
	if err != nil {
		glog.Errorf("%s failed to parse offset from master output %s error %v", processName, fields[1], err)
	}

	clockState = fields[2]
	if clockState != "s0" || clockState != "s1" || clockState != "s2" || clockState != "s3" {
		glog.Errorf("%s failed to parse clock state output %s error %v", processName, fields[2], err)
	}

	frequencyAdjustment, err = strconv.ParseFloat(fields[4], 64)
	if err != nil {
		glog.Errorf("%s failed to parse frequency adjustment output %s error %v", processName, fields[4], err)
	}

	if len(fields) >= 4 {
		delayFromMaster, err = strconv.ParseFloat(fields[5], 64)
		if err != nil {
			glog.Errorf("%s failed to parse delay from master output %s error %v", processName, fields[5], err)
		}
	} else {
		// If there is no delay from master this mean we are out of sync
		glog.Warningf("no delay from master process %s out of sync", processName)
	}

	return
}
