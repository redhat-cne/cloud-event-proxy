package metrics

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/types"
	"github.com/redhat-cne/sdk-go/pkg/event/ptp"
	log "github.com/sirupsen/logrus"
)

var ptpProcessStatusIdentifier = "PTP_PROCESS_STATUS"

func extractSummaryMetrics(processName, output string) (iface string, ptpOffset, maxPtpOffset, frequencyAdjustment, delay float64) {
	// remove everything before the rms string
	// This makes the output to equal
	// 0            1       2              3     4   5      6     7     8       9
	// phc2sys 5196755.139 ptp4l.0.config ens7f1 rms 3151717 max 3151717 freq -6085106 +/-   0 delay  2746 +/-   0
	// phc2sys 5196804.326 ptp4l.0.config CLOCK_REALTIME rms 9452637 max 9452637 freq +1196097 +/-   0 delay  1000
	// ptp4l[74737.942]: [ptp4l.0.config] rms   53 max   74 freq -16642 +/-  40 delay  1089 +/-  20

	indx := strings.Index(output, "rms")
	if indx < 0 {
		return
	}

	replacer := strings.NewReplacer("[", " ", "]", " ", ":", " ")
	output = replacer.Replace(output)
	indx = FindInLogForCfgFileIndex(output)
	if indx == -1 {
		log.Errorf("config name is not found in log outpt")
		return
	}
	output = output[indx:]
	fields := strings.Fields(output)

	// ptp4l.0.config CLOCK_REALTIME rms   31 max   31 freq -77331 +/-   0 delay  1233 +/-   0
	if len(fields) < 8 {
		return
	}

	// when ptp4l log is missing interface name
	if fields[1] == rms {
		fields = append(fields, "") // Making space for the new element
		//  0             1     2
		// ptp4l.0.config rms   53 max   74 freq -16642 +/-  40 delay  1089 +/-  20
		copy(fields[2:], fields[1:]) // Shifting elements
		fields[1] = MasterClockType  // Copying/inserting the value
		//  0             0       1   2
		// ptp4l.0.config master rms   53 max   74 freq -16642 +/-  40 delay  1089 +/-  20
	} else if fields[1] != ClockRealTime {
		// phc2sys[5196755.139]: [ptp4l.0.config] ens5f0 rms 3152778 max 3152778 freq -6083928 +/-   0 delay  2791 +/-   0
		return // do not register offset value for master (interface) port reported by phc2sys
	}
	iface = fields[1]

	ptpOffset, err := strconv.ParseFloat(fields[3], 64)
	if err != nil {
		log.Errorf("%s failed to parse offset from the output %s error %v", processName, fields[3], err)
	}

	maxPtpOffset, err = strconv.ParseFloat(fields[5], 64)
	if err != nil {
		log.Errorf("%s failed to parse max offset from the output %s error %v", processName, fields[5], err)
	}

	frequencyAdjustment, err = strconv.ParseFloat(fields[7], 64)
	if err != nil {
		log.Errorf("%s failed to parse frequency adjustment output %s error %v", processName, fields[7], err)
	}

	if len(fields) >= 11 {
		delay, err = strconv.ParseFloat(fields[11], 64)
		if err != nil {
			log.Errorf("%s failed to parse delay from the output %s error %v", processName, fields[11], err)
		}
	} else {
		// If there is no delay from this mean we are out of sync
		log.Warningf("no delay from the process %s out of sync", processName)
	}

	return
}

func extractRegularMetrics(processName, output string) (interfaceName string, ptpOffset, maxPtpOffset, frequencyAdjustment, delay float64, clockState ptp.SyncState) {
	// remove everything before the rms string
	// This makes the out to equal
	// ptp4l[5196819.100]: [ptp4l.0.config] master offset   -2162130 s2 freq +22451884 path delay    374976
	// phc2sys[4268818.286]: [] CLOCK_REALTIME phc offset       -62 s0 freq  -78368 delay   1100
	// phc2sys[4268818.287]: [] ens5f1 phc offset       -92 s0 freq    -890 delay   2464   ( this is down)
	// phc2sys[4268818.287]: [] ens5f0 phc offset       -47 s2 freq   -2047 delay   2438

	// 0     1            2              3       4         5    6   7     8         9   10       11
	//                                  1       2           3   4   5     6        7    8         9
	// ptp4l 5196819.100 ptp4l.0.config master offset   -2162130 s2 freq +22451884 path delay    374976
	output = strings.Replace(output, "path", "", 1)
	replacer := strings.NewReplacer("[", " ", "]", " ", ":", " ", "phc", "", "sys", "")
	output = replacer.Replace(output)

	index := FindInLogForCfgFileIndex(output)
	if index == -1 {
		log.Errorf("config name is not found in log outpt")
		return
	}
	output = output[index:]
	fields := strings.Fields(output)

	//       0         1      2          3    4   5    6          7     8
	// ptp4l.0.config master offset   -2162130 s2 freq +22451884  delay 374976
	if len(fields) < 7 {
		return
	}

	if fields[2] != offset {
		log.Errorf("%s failed to parse offset from master output %s error %s", processName, fields[1], "offset is not in right order")
		return
	}

	interfaceName = fields[1]

	ptpOffset, err := strconv.ParseFloat(fields[3], 64)
	if err != nil {
		log.Errorf("%s failed to parse offset from master output %s error %v", processName, fields[3], err)
	}

	maxPtpOffset, err = strconv.ParseFloat(fields[3], 64)
	if err != nil {
		log.Errorf("%s failed to parse max offset from master output %s error %v", processName, fields[3], err)
	}

	frequencyAdjustment, err = strconv.ParseFloat(fields[6], 64)
	if err != nil {
		log.Errorf("%s failed to parse frequency adjustment output %s error %v", processName, fields[6], err)
	}

	state := fields[4]

	switch state {
	case unLocked:
		clockState = ptp.FREERUN
	case clockStep:
		clockState = ptp.FREERUN
	case locked:
		clockState = ptp.LOCKED
	default:
		log.Errorf("%s -  failed to parse clock state output `%s` ", processName, fields[2])
	}

	if len(fields) >= 8 {
		delay, err = strconv.ParseFloat(fields[8], 64)
		if err != nil {
			log.Errorf("%s failed to parse delay from master output %s error %v", processName, fields[8], err)
		}
	} else {
		// If there is no delay from master this mean we are out of sync
		clockState = ptp.HOLDOVER
		log.Warningf("no delay from master process %s out of sync", processName)
	}

	return
}

// ExtractPTP4lEvent ... extract event form ptp4l logs
//
//	    "INITIALIZING to LISTENING on INIT_COMPLETE"
//		"LISTENING to UNCALIBRATED on RS_SLAVE"
//		"UNCALIBRATED to SLAVE on MASTER_CLOCK_SELECTED"
//		"SLAVE to FAULTY on FAULT_DETECTED"
//		"LISTENING to FAULTY on FAULT_DETECTED"
//		"FAULTY to LISTENING on INIT_COMPLETE"
//		"FAULTY to SLAVE on INIT_COMPLETE"
//		"SLAVE to UNCALIBRATED on SYNCHRONIZATION_FAULT"
//	     "MASTER to PASSIVE"
func extractPTP4lEventState(output string) (portID int, role types.PtpPortRole, clockState ptp.SyncState) {
	// This makes the out to equal
	// phc2sys[187248.740]:[ens5f0] CLOCK_REALTIME phc offset        12 s2 freq   +6879 delay    49
	// phc2sys[187248.740]:[ens5f1] CLOCK_REALTIME phc offset        12 s2 freq   +6879 delay    49
	// ptp4l[5199193.712]: [ptp4l.0.config] port 1: SLAVE to UNCALIBRATED on SYNCHRONIZATION_FAULT
	replacer := strings.NewReplacer("[", " ", "]", " ", ":", " ")
	output = replacer.Replace(output)

	index := strings.Index(output, " port ")
	if index == -1 {
		return
	}
	output = output[index:]
	fields := strings.Fields(output)
	// port 1: delay timeout
	if len(fields) < 2 {
		return
	}

	portIndex := fields[1]
	role = types.UNKNOWN

	var e error
	portID, e = strconv.Atoi(portIndex)
	if e != nil {
		log.Errorf("error parsing port id %s", e)
		portID = 0
		return
	}

	clockState = ptp.FREERUN
	if strings.Contains(output, "UNCALIBRATED to SLAVE") {
		role = types.SLAVE
	} else if strings.Contains(output, "UNCALIBRATED to PASSIVE") || strings.Contains(output, "MASTER to PASSIVE") ||
		strings.Contains(output, "SLAVE to PASSIVE") || strings.Contains(output, "LISTENING to PASSIVE") {
		role = types.PASSIVE
	} else if strings.Contains(output, "UNCALIBRATED to MASTER") || strings.Contains(output, "LISTENING to MASTER") {
		role = types.MASTER
	} else if strings.Contains(output, "FAULT_DETECTED") || strings.Contains(output, "SYNCHRONIZATION_FAULT") {
		role = types.FAULTY
		clockState = ptp.HOLDOVER
	}
	return
}

// FindInLogForCfgFileIndex ... find config name from the log
func FindInLogForCfgFileIndex(out string) int {
	match := ptpConfigFileRegEx.FindStringIndex(out)
	if len(match) == 2 {
		return match[0]
	}
	return -1
}

func isOffsetInRange(ptpOffset, maxOffsetThreshold, minOffsetThreshold int64) bool {
	if ptpOffset < maxOffsetThreshold && ptpOffset > minOffsetThreshold {
		return true
	}
	return false
}

func parsePTPStatus(output string, fields []string) (int64, error) {
	// ptp4l 5196819.100 ptp4l.0.config PTP_PROCESS_STOPPED:0/1
	if len(fields) < 5 {
		e := fmt.Errorf("ptp process status is not in right format %s", output)
		log.Println(e)
		return PtpProcessDown, e
	}
	status, err := strconv.ParseInt(fields[4], 10, 64)
	if err != nil {
		log.Error("error process status value")
		return PtpProcessDown, err
	}
	// ptp4l 5196819.100 ptp4l.0.config PTP_PROCESS_STOPPED:0/1
	UpdateProcessStatusMetrics(fields[0], fields[2], status)
	return status, nil
}
