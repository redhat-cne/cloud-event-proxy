package event

import (
	"github.com/golang/glog"
	ceevent "github.com/redhat-cne/sdk-go/pkg/event"
	log "github.com/sirupsen/logrus"
	"strconv"
	"strings"
)

const (
	unLocked                    = "s0"
	clockStep                   = "s1"
	locked                      = "s2"
	ptp4lProcessName            = "ptp4l"
	phc2sysProcessName          = "phc2sys"
	maxOffsetThrushHold float64 = 100
	minOffsetThrushHold float64 = -100
)

/*
ExtractEvent ...
INITIALIZING to LISTENING on INIT_COMPLETE
	(ptp4l[357.214]: port 1: LISTENING to UNCALIBRATED on RS_SLAVE)
	If port 1: Print event Status Event ens5f1 == LISTENING (FREERUN)

LISTENING to UNCALIBRATED on RS_SLAVE
    (ptp4l[361.224]: port 1: UNCALIBRATED to SLAVE on MASTER_CLOCK_SELECTED)
	If port 1 : Print event Status Event ens5f1 == CALIBRATING (FREERUN)


UNCALIBRATED to SLAVE on MASTER_CLOCK_SELECTED
	(ptp4l[361.224]: port 1: UNCALIBRATED to SLAVE on MASTER_CLOCK_SELECTED)
 	If port 1 : Print event Status Event ens5f1 == SLAVE (FREERUN) But don’t have status yet so, no event.

SLAVE to FAULTY on FAULT_DETECTED
    (ptp4l[432313.222]: [ens5f1] port 1: SLAVE to FAULTY on FAULT_DETECTED (FT_UNSPECIFIED))
	If port 1: Event is Interface down .Event is Interface (salve) FAULTY
LISTENING to FAULTY on FAULT_DETECTED (FT_UNSPECIFIED)
   ptp4l[432313.222]: [ens5f1] LISTENING to FAULTY on FAULT_DETECTED (FT_UNSPECIFIED)
 	If port 1: Event is Interface (master) FAULTY
ptp4l[432407.135]: [ens5f1] master offset        517 s2 freq    -850 path delay        84
    Parse string : CLOCK_REALTIME offset   s[1]== s0|s1|s2 compare s[0]
	If s0/s1 Event=FREERUN
	If s2 and offset <= threshold Event=LOCKED elseif s2 and offset > threshold for 1 secs Then HOLDOVER else if s2 and > threshold for 2 secs then FREERUN
*/
// ExtractEvent .. Extract events  from the logs
func ExtractEvent(processName, iface, time string, output string) (offsetFromMaster, frequencyAdjustment, delayFromMaster float64, clockState ceevent.SyncState) {
	if processName == phc2sysProcessName {
		offsetFromMaster, frequencyAdjustment, delayFromMaster, clockState = ExtractPhc2sysEvent(processName, iface, output)
	} else if processName==ptp4lProcessName {
		clockState = ExtractPTP4lEvent(processName, iface, output)
	}
	return
}

// GenEvent ... generate events form the logs
func GenEvent(iface string, offsetFromMaster float64, clockState, lastClockState ceevent.SyncState) {
	//TODO: Decide from HOLDOVER  to FREERUN
	log.Infof("lastClockState: %s\n curent clock state: %s\n offsetFromMaster %f\n  maxOffsetThrushHold %f\n ",lastClockState,clockState,offsetFromMaster,maxOffsetThrushHold)
	if clockState == ceevent.LOCKED {
		if offsetFromMaster != 0 { //ptp4l will  return 0 offset
			if (offsetFromMaster > maxOffsetThrushHold || offsetFromMaster < minOffsetThrushHold) &&
				lastClockState != ceevent.HOLDOVER {
				log.Printf("EVENT DATA FOR %s IS %#v",iface, eventData(ceevent.HOLDOVER))
			} else if offsetFromMaster > maxOffsetThrushHold || offsetFromMaster < minOffsetThrushHold  &&
					lastClockState != ceevent.FREERUN{
				log.Printf("EVENT DATA FOR %s  IS %#v", iface, eventData(ceevent.FREERUN))
			} else if lastClockState !=clockState {
				log.Printf("EVENT DATA FOR %s  IS %#v",iface, eventData(clockState))
			}
		} else if clockState != lastClockState {
			log.Printf("EVENT DATA FOR %s  IS %#v", iface,eventData(clockState))
		}
	} else if clockState != lastClockState {
		log.Printf("EVENT DATA FOR %s  IS %#v", iface,eventData(clockState))
	}
}

func eventData(state ceevent.SyncState) ceevent.Data {
	// create an event
	data := ceevent.Data{
		Version: "v1",
		Values: []ceevent.DataValue{{
			Resource:  "/cluster/node/ptp",
			DataType:  ceevent.NOTIFICATION,
			ValueType: ceevent.ENUMERATION,
			Value:     state,
		},
		},
	}
	return data
}

// ExtractPhc2sysEvent ... extract events from  phc2sys logs
func ExtractPhc2sysEvent(processName, iface string, output string) (offsetFromMaster , frequencyAdjustment, delayFromMaster float64, clockState ceevent.SyncState) {
	// This makes the out to equals
	//phc2sys[187248.740]:[ens5f0] CLOCK_REALTIME phc offset        12 s2 freq   +6879 delay    49
	var err error
	output = strings.Replace(output, "path", "", 1)
	indx := strings.Index(output, "offset")
	if indx == -1 { // no offset field , log is useless
		return
	}
	output = output[indx:]
	fields := strings.Fields(output)

	if len(fields) < 5 {
		glog.Errorf("%s - %s failed to parse output %s: unexpected number of fields", processName, iface, output)
		return
	}
	offsetFromMaster, err = strconv.ParseFloat(fields[1], 64)
	if err != nil {
		glog.Errorf("%s - %s failed to parse offset from phc output %s error %v", processName, iface, fields[1], err)
	}

	frequencyAdjustment, err = strconv.ParseFloat(fields[4], 64)
	if err != nil {
		glog.Errorf("%s failed to parse frequency adjustment output %s error %v", processName, fields[4], err)
	}

	if len(fields) >= 6 {
		delayFromMaster, err = strconv.ParseFloat(fields[6], 64)
		if err != nil {
			glog.Errorf("%s failed to parse delay from master output %s error %v", processName, fields[6], err)
		}
	} else {
		// If there is no delay from master this mean we are out of sync
		glog.Warningf("no delay from master process %s out of sync", processName)
	}

	state := fields[2]

	switch state {
	case unLocked:
		clockState = ceevent.FREERUN
	case clockStep:
		clockState = ceevent.FREERUN
	case locked:
		clockState = ceevent.LOCKED
	default:
		glog.Errorf("%s - %s failed to parse clock state output `%s` ", processName, iface, fields[2])
	}

	return
}

// ExtractPTP4lEvent ... extract event form ptp4l logs
func ExtractPTP4lEvent(processName, iface, output string) (clockState ceevent.SyncState) {
	// This makes the out to equals
	//phc2sys[187248.740]:[ens5f0] CLOCK_REALTIME phc offset        12 s2 freq   +6879 delay    49
	// phc2sys[187248.740]:[ens5f1] CLOCK_REALTIME phc offset        12 s2 freq   +6879 delay    49
	//ptp4l[3535499.740]: [ens5f0] master offset         -6 s2 freq   -1879 path delay        88
	if strings.Contains(output, "INITIALIZING to LISTENING on INIT_COMPLETE") {
		clockState = ceevent.FREERUN
	} else if strings.Contains(output, "LISTENING to UNCALIBRATED on RS_SLAVE") {
		clockState = ceevent.FREERUN
	} else if strings.Contains(output, "UNCALIBRATED to SLAVE on MASTER_CLOCK_SELECTED") {
		clockState = ceevent.FREERUN
	} else if strings.Contains(output, "SLAVE to FAULTY on FAULT_DETECTED") {
		clockState = ceevent.FREERUN
	} else if strings.Contains(output, "LISTENING to FAULTY on FAULT_DETECTED") {
		clockState = ceevent.FREERUN
	} else if strings.Contains(output, "FAULTY to LISTENING on INIT_COMPLETE") {
		clockState = ceevent.FREERUN
	} else if strings.Contains(output, "FAULTY to SLAVE on INIT_COMPLETE") {
		clockState = ceevent.FREERUN
	} else if strings.Contains(output, "SLAVE to UNCALIBRATED on SYNCHRONIZATION_FAULT") {
		clockState = ceevent.HOLDOVER
	}
	return
}
