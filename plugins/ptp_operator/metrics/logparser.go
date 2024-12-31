package metrics

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/event"
	"github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/stats"
	"k8s.io/utils/pointer"

	"github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/types"
	"github.com/redhat-cne/sdk-go/pkg/event/ptp"
	log "github.com/sirupsen/logrus"
)

var (
	ptpProcessStatusIdentifier = "PTP_PROCESS_STATUS"
)

func extractSummaryMetrics(processName, output string) (iface string, ptpOffset, maxPtpOffset, frequencyAdjustment, delay float64) {
	// remove everything before the rms string
	// This makes the output to equal
	// 0            1       2              3     4   5      6     7     8       9
	// phc2sys 5196755.139 ptp4l.0.config ens7f1 rms 3151717 max 3151717 freq -6085106 +/-   0 delay  2746 +/-   0
	// phc2sys 5196804.326 ptp4l.0.config CLOCK_REALTIME rms 9452637 max 9452637 freq +1196097 +/-   0 delay  1000
	// ptp4l[74737.942]: [ptp4l.0.config] rms   53 max   74 freq -16642 +/-  40 delay  1089 +/-  20
	// ptp4l[365195.391]: [ptp4l.0.config] master offset         -1 s2 freq   -3972 path delay        89
	// ts2phc[82674.465]: [ts2phc.0.cfg] nmea delay: 88403525 ns
	// ts2phc[82674.465]: [ts2phc.0.cfg] ens2f1 extts index 0 at 1673031129.000000000 corr 0 src 1673031129.911642976 diff 0
	// ts2phc[82674.465]: [ts2phc.0.cfg] ens2f1 master offset          0 s2 freq      -0
	// log without rms won't be processed here ts2phc doesn't have rms
	indx := strings.Index(output, "rms")
	if indx < 0 {
		return
	}
	replacer := strings.NewReplacer("[", " ", "]", " ", ":", " ")
	output = replacer.Replace(output)
	indx = FindInLogForCfgFileIndex(output)
	if indx == -1 {
		log.Errorf("config name is not found in log output %s", output)
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
	// phc2sys[4268818.286]: [ptp4l.0.config] CLOCK_REALTIME phc offset       -62 s0 freq  -78368 delay   1100
	// phc2sys[4268818.287]: [ptp4l.0.config] ens5f1 phc offset       -92 s0 freq    -890 delay   2464   ( this is down)
	// phc2sys[4268818.287]: [ptp4l.0.config] ens5f0 phc offset       -47 s2 freq   -2047 delay   2438
	// ts2phc[82674.465]: [ts2phc.0.config] nmea delay: 88403525 ns
	// ts2phc[82674.465]: [ts2phc.0.config] ens2f1 extts index 0 at 1673031129.000000000 corr 0 src 1673031129.911642976 diff 0
	// ts2phc[82674.465]: [ts2phc.0.config] ens2f1 master offset          0 s2 freq      -0
	// ts2phc[521734.693]: [ts2phc.0.config:6] /dev/ptp6 offset          0 s2 freq      -0

	// 0     1            2              3       4         5    6   7     8         9   10       11
	//                                  1       2           3   4   5     6        7    8         9
	// ptp4l 5196819.100 ptp4l.0.config master offset   -2162130 s2 freq +22451884 path delay    374976
	//
	// ts2phc 522946.693    ts2phc.0.config  ens7f0 offset          0 s2 freq      -0
	index := FindInLogForCfgFileIndex(output)
	if index == -1 {
		log.Errorf("config name is not found in log output %s", output)
		return
	}

	output = strings.Replace(output, "path", "", 1)
	// ts2phc 522946.693    ts2phc.0.config  ens7f0 offset          0 s2 freq      -0
	// careful ts2phc --> here phc will be replaced so use empty string around the text
	replacer := strings.NewReplacer("[", " ", "]", " ", ":", " ", " phc ", " ", " sys ", " ")
	output = replacer.Replace(output)

	output = output[index:]
	fields := strings.Fields(output)

	//       0           1      2          3     4   5    6          7     8
	// ptp4l.0.config   master offset   -2162130 s2 freq +22451884  delay 374976
	// ts2phc.0.config  ens2f1  master    offset  0  s2   freq      -0
	//       0           1      2          3      4     5        6
	// ts2phc.0.config  ens7f0  offset     0     s2    freq      -0
	//       0             1              2          3     4        5    6       7
	// ptp4l.0.config  CLOCK_REALTIME  offset       -62 s0 freq  -78368 delay   1100
	if len(fields) < 7 {
		return
	}
	// either master or clock_realtime
	interfaceName = fields[1]
	if fields[2] != offset && processName == ts2phcProcessName {
		// Remove the element at index 1 from fields.
		copy(fields[1:], fields[2:])
		// ts2phc.0.cfg  master    offset          0 s2 freq      -0
		fields = fields[:len(fields)-1] // Truncate slice.
	}
	if fields[2] != offset {
		log.Errorf("%s failed to parse offset from master output %s error %s", processName, fields[2], "offset is not in right order")
		return
	}

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
		log.Errorf("%s -  failed to parse clock state output `%s` ", processName, fields[4])
	}

	if len(fields) >= 8 {
		delay, err = strconv.ParseFloat(fields[8], 64)
		if err != nil {
			log.Errorf("%s failed to parse delay from master output %s error %v", processName, fields[8], err)
		}
	} else if processName != ts2phcProcessName { // there is delay  printed with ts2phc
		// If there is no delay from master this mean we are out of sync
		clockState = ptp.HOLDOVER
		log.Warningf("no delay from master process %s out of sync", processName)
	}
	return
}

func extractNmeaMetrics(processName, output string) (interfaceName string, status, ptpOffset float64, clockState ptp.SyncState) {
	// ts2phc[1699929121]:[ts2phc.0.config] ens2f0 nmea_status 0 offset 999999 s0
	var err error
	index := FindInLogForCfgFileIndex(output)
	if index == -1 {
		log.Errorf("config name is not found in log output %s", output)
		return
	}

	replacer := strings.NewReplacer("[", " ", "]", " ", ":", " ")
	output = replacer.Replace(output)

	output = output[index:]
	fields := strings.Fields(output)

	//       0         1      2           3 4          5      6
	// ts2phc.0.config ens2f0 nmea_status 0 offset     999999 s0
	if len(fields) < 7 {
		return
	}

	interfaceName = fields[1]
	status, err = strconv.ParseFloat(fields[3], 64)
	if err != nil {
		log.Errorf("%s failed to parse nmea status from master output %s error %v", processName, fields[3], err)
	}
	ptpOffset, err = strconv.ParseFloat(fields[5], 64)
	if err != nil {
		log.Errorf("%s failed to parse nmea offset from master output %s error %v", processName, fields[5], err)
	}

	state := fields[6]

	switch state {
	case unLocked:
		clockState = ptp.FREERUN
	case clockStep:
		clockState = ptp.FREERUN
	case locked:
		clockState = ptp.LOCKED
	default:
		log.Errorf("%s -  failed to parse clock state output `%s` ", processName, fields[6])
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
	if strings.Contains(output, "UNCALIBRATED to SLAVE") ||
		strings.Contains(output, "LISTENING to SLAVE") {
		role = types.SLAVE
	} else if strings.Contains(output, "UNCALIBRATED to PASSIVE") ||
		strings.Contains(output, "MASTER to PASSIVE") ||
		strings.Contains(output, "SLAVE to PASSIVE") ||
		strings.Contains(output, "LISTENING to PASSIVE") {
		role = types.PASSIVE
	} else if strings.Contains(output, "UNCALIBRATED to MASTER") ||
		strings.Contains(output, "LISTENING to MASTER") {
		role = types.MASTER
	} else if strings.Contains(output, "FAULT_DETECTED") ||
		strings.Contains(output, "SYNCHRONIZATION_FAULT") ||
		strings.Contains(output, "SLAVE to UNCALIBRATED") ||
		strings.Contains(output, "MASTER to UNCALIBRATED on RS_SLAVE") {
		role = types.FAULTY
		clockState = ptp.HOLDOVER
	} else if strings.Contains(output, "SLAVE to MASTER") ||
		strings.Contains(output, "SLAVE to GRAND_MASTER") {
		role = types.MASTER
		clockState = ptp.HOLDOVER
	} else if strings.Contains(output, "SLAVE to LISTENING") {
		role = types.LISTENING
		clockState = ptp.HOLDOVER
	}
	return
}

// FindInLogForCfgFileIndex ... find config name from the log
func FindInLogForCfgFileIndex(out string) int {
	if matchPtp4l := ptpConfigFileRegEx.FindStringIndex(out); len(matchPtp4l) == 2 {
		return matchPtp4l[0]
	}
	if matchTS2Phc := FindInTS2PhcLogForCfgFileIndex(out); matchTS2Phc > -1 {
		return matchTS2Phc
	}
	if matchPhc2Sys := FindInPhc2SysLogForCfgFileIndex(out); matchPhc2Sys > -1 {
		return matchPhc2Sys
	}
	return -1
}

// FindInTS2PhcLogForCfgFileIndex ... find config name from the log
func FindInTS2PhcLogForCfgFileIndex(out string) int {
	matchPhc2Sys := ts2phcConfigFileRegEx.FindStringIndex(out)
	if len(matchPhc2Sys) == 2 {
		return matchPhc2Sys[0]
	}
	return -1
}

// FindInPhc2SysLogForCfgFileIndex ... find config name from the log
func FindInPhc2SysLogForCfgFileIndex(out string) int {
	matchPhc2Sys := phc2SysConfigFileRegEx.FindStringIndex(out)
	if len(matchPhc2Sys) == 2 {
		return matchPhc2Sys[0]
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

// ParseGMLogs ... parse logs for various events
func (p *PTPEventManager) ParseGMLogs(processName, configName, output string, fields []string,
	ptpStats stats.PTPStats) {
	//GM[1689282762]:[ts2phc.0.config] ens2f1 T-GM-STATUS s0
	// 0        1             2           3          4    5
	// GM  1689014436  ts2phc.0.config ens2f1 T-GM-STATUS s0
	if strings.Contains(output, gmStatusIdentifier) {
		if len(fields) < 4 {
			log.Errorf("GM Status is not in right format %s", output)
			return
		}
		ptpStats.CheckSource(master, configName, ts2phcProcessName)
	} else {
		return
	}
	iface := fields[3]
	syncState := fields[5]
	masterType := types.IFace(MasterClockType)

	clockState := event.ClockState{
		State:       GetSyncState(syncState),
		IFace:       pointer.String(iface),
		Process:     processName,
		ClockSource: event.GM,
		Value:       nil,
		Metric:      nil,
		NodeName:    ptpNodeName,
	}
	alias := getAlias(iface)

	SyncState.With(map[string]string{"process": processName, "node": ptpNodeName, "iface": alias}).Set(GetSyncStateID(syncState))
	// status metrics
	ptpStats[masterType].SetPtpDependentEventState(clockState, ptpStats.HasMetrics(processName), ptpStats.HasMetricHelp(processName))
	ptpStats[masterType].SetAlias(alias)

	// If GM is locked/Freerun/Holdover then ptp state change event
	masterResource := fmt.Sprintf("%s/%s", alias, MasterClockType)
	lastClockState := ptpStats[masterType].LastSyncState()

	// When GM is enabled, there is only one event happening at the GM level for now, so it is not being sent to the state decision routine.
	// LOCKED -->FREERUN
	//LOCKED->HOLDOVER
	/// HOLDOVER-->FREERUN
	// HOLDOVER-->LOCKED

	_, phaseOffset, _, err := ptpStats[types.IFace(iface)].GetDependsOnValueState(dpllProcessName, pointer.String(iface), phaseStatus)
	if err != nil {
		log.Errorf("error parsing phase offset %s", err.Error())
	}
	ptpStats[masterType].SetLastOffset(int64(phaseOffset))
	lastOffset := ptpStats[masterType].LastOffset()

	if clockState.State != lastClockState { // publish directly here
		log.Infof("%s sync state %s, last ptp state is : %s", masterResource, clockState.State, lastClockState)
		p.PublishEvent(clockState.State, lastOffset, masterResource, ptp.PtpStateChange)
		ptpStats[masterType].SetLastSyncState(clockState.State)
		UpdateSyncStateMetrics(processName, alias, ptpStats[masterType].LastSyncState())
		UpdatePTPOffsetMetrics(processName, processName, alias, float64(lastOffset))
	}
}

// ParseDPLLLogs ... parse logs for various events
func (p *PTPEventManager) ParseDPLLLogs(processName, configName, output string, fields []string,
	ptpStats stats.PTPStats) {
	// dpll[1700598434]:[ts2phc.0.config] ens2f0 frequency_status 3 offset 0 phase_status 3 pps_status 1 s2
	// 0        1             2           3             4       5     6    7           8   9 10         11 12
	// dpll 1700598434 ts2phc.0.config ens2f0   frequency_status 3  offset 0  phase_status 3 pps_status 1  s2
	if strings.Contains(output, frequencyStatus) {
		if len(fields) < 12 {
			log.Errorf("DPLL Status is not in right format %s", output)
			return
		}
	} else {
		return
	}
	var phaseStatusValue float64
	var frequencyStatusValue int64
	var dpllOffset float64
	var ppsStatusValue float64
	var err error
	iface := pointer.String(fields[3])
	syncState := fields[12]
	ifaceType := types.IFace(*iface)
	//TODO: try to init once
	ptpStats.CheckSource(ifaceType, configName, ts2phcProcessName)
logStatusLoop:
	// read 4, 6, 8 and 10
	for i := 4; i < 11; i = i + 2 { // the order need to be fixed in linux ptp daemon , this is workaround
		switch fields[i] {
		case frequencyStatus:
			if frequencyStatusValue, err = strconv.ParseInt(fields[i+1], 10, 64); err != nil {
				log.Error("error parsing frequency_status")
				break logStatusLoop
			}
		case phaseStatus:
			if phaseStatusValue, err = strconv.ParseFloat(fields[i+1], 64); err != nil {
				log.Error("error parsing phase_status")
				// exit from loop if error
				break logStatusLoop
			}
		case offset:
			if dpllOffset, err = strconv.ParseFloat(fields[i+1], 64); err != nil {
				log.Errorf("%s failed to parse offset from the output %s error %s", processName, fields[3], err.Error())
				break logStatusLoop
			}
		case ppsStatus:
			if ppsStatusValue, err = strconv.ParseFloat(fields[i+1], 64); err != nil {
				log.Errorf("%s failed to parse offset from the output %s error %s", processName, fields[3], err.Error())
				break logStatusLoop
			}
		}
	}

	if err == nil {
		alias := getAlias(*iface)
		ptpStats[ifaceType].SetPtpDependentEventState(event.ClockState{
			State:   GetSyncState(syncState),
			Offset:  pointer.Float64(dpllOffset),
			Process: dpllProcessName,
			IFace:   iface,
			Value: map[string]int64{frequencyStatus: frequencyStatusValue, phaseStatus: int64(phaseStatusValue),
				ppsStatus: int64(ppsStatusValue)},
			ClockSource: event.DPLL,
			NodeName:    ptpNodeName,
			HelpText: map[string]string{
				frequencyStatus: "-1=UNKNOWN, 0=INVALID, 1=FREERUN, 2=LOCKED, 3=LOCKED_HO_ACQ, 4=HOLDOVER",
				phaseStatus:     "-1=UNKNOWN, 0=INVALID, 1=FREERUN, 2=LOCKED, 3=LOCKED_HO_ACQ, 4=HOLDOVER",
				ppsStatus:       "0=UNAVAILABLE, 1=AVAILABLE",
			},
		}, ptpStats.HasMetrics(processName), ptpStats.HasMetricHelp(processName))
		SyncState.With(map[string]string{"process": processName, "node": ptpNodeName, "iface": alias}).Set(GetSyncStateID(syncState))
		UpdatePTPOffsetMetrics(processName, processName, alias, dpllOffset)
	} else {
		log.Errorf("error parsing dpll %s", err.Error())
	}
}

// ParseGNSSLogs ... parse logs for various events
func (p *PTPEventManager) ParseGNSSLogs(processName, configName, output string, fields []string,
	ptpStats stats.PTPStats) {
	//gnss[1689014431]:[ts2phc.0.config] ens2f1 gnss_status 5 offset 0 s0
	// 0        1             2           3        4       5    6    7   8
	// gnss 1689014431 ts2phc.0.config ens2f1  gnss_status 5  offset 0 s0
	if strings.Contains(output, gnssEventIdentifier) {
		if len(fields) < 8 {
			log.Errorf("GNSS Status is not in right format %s", output)
			return
		}
	} else {
		return
	}
	var gnssState int64
	var gnssOffset float64
	var err error
	//                 0    1             2               3        4        5  6    7   8
	// ParseGNSSLogs: gnss 1692639234   ts2phc.2.config  ens7f0 gnss_status 3 offset 5 s2
	iface := pointer.String(fields[3])
	ifaceType := types.IFace(*iface)
	//TODO: try to init once
	ptpStats.CheckSource(ifaceType, configName, ts2phcProcessName)
	syncState := fields[8]
	if gnssState, err = strconv.ParseInt(fields[5], 10, 64); err != nil {
		log.Errorf("error parsing gnss state %s", processName)
	}

	if gnssOffset, err = strconv.ParseFloat(fields[7], 64); err != nil {
		log.Errorf("%s failed to parse offset from the output %s error %v", processName, fields[7], err)
	}

	//openshift_ptp_offset_ns{from="gnss",iface="ens2f1",node="cnfde21.ptp.lab.eng.bos.redhat.com",process="gnss"} 0
	if err == nil {
		alias := getAlias(*iface)
		// last state of GNSS
		lastState, errState := ptpStats[ifaceType].GetStateState(processName, iface)
		pLabels := map[string]string{"from": processName, "node": ptpNodeName,
			"process": processName, "iface": alias}
		PtpOffset.With(pLabels).Set(gnssOffset)
		SyncState.With(map[string]string{"process": processName, "node": ptpNodeName, "iface": alias}).Set(GetSyncStateID(syncState))
		ptpStats[ifaceType].SetPtpDependentEventState(event.ClockState{
			State:       GetSyncState(syncState),
			Offset:      pointer.Float64(gnssOffset),
			Process:     processName,
			IFace:       iface,
			Value:       map[string]int64{gnssStatus: gnssState},
			ClockSource: event.GNSS,
			NodeName:    ptpNodeName,
			HelpText:    map[string]string{gnssStatus: "0=NOFIX, 1=Dead Reckoning Only, 2=2D-FIX, 3=3D-FIX, 4=GPS+dead reckoning fix, 5=Time only fix"},
		}, ptpStats.HasMetrics(processName), ptpStats.HasMetricHelp(processName))
		// reduce noise ; if state changed then send events
		if lastState != GetSyncState(syncState) || errState != nil {
			log.Infof("%s last state %s and current state %s", processName, lastState, GetSyncState(syncState))
			masterResource := fmt.Sprintf("%s/%s", alias, MasterClockType)
			p.publishGNSSEvent(gnssState, gnssOffset, GetSyncState(syncState), masterResource, ptp.GnssStateChange)
		}
	}
}

// GetGPSFixState ... returns gps state by computing gpsFix and offset derived state
func (p *PTPEventManager) GetGPSFixState(gpsFix int64, syncState ptp.SyncState) (state ptp.SyncState) {
	state = ptp.ANTENNA_DISCONNECTED
	// 0=NOFIX, 1=Dead Reckoning Only, 2=2D-FIX, 3=3D-FIX, 4=GPS+dead reckoning fix, 5=Time only fix
	if syncState == ptp.LOCKED {
		state = ptp.SYNCHRONIZED
	} else if gpsFix >= 3 {
		state = ptp.ACQUIRING_SYNC // if state was declared as FREERUN due to Offset outside threshold set to ACQUIRING_SYNC
	} else if gpsFix == 0 {
		state = ptp.ANTENNA_DISCONNECTED
	} else if gpsFix < 3 {
		state = ptp.ACQUIRING_SYNC
	}
	return
}

// extractPTPHaMetrics ... parse logs for ptp ha
func extractPTPHaMetrics(processName, output string) (profile string, state int64, err error) {
	// phc2sys[1710435400]:[phc2sys.2.config] ptp_ha_profile profile1 state 1
	index := FindInPhc2SysLogForCfgFileIndex(output)
	state = -1
	if index == -1 {
		log.Errorf("config name is not found in log output %s", output)
		return
	}

	replacer := strings.NewReplacer("[", " ", "]", " ", ":", " ")
	output = replacer.Replace(output)

	output = output[index:]
	fields := strings.Fields(output)

	//       0                   1          2      3   4
	// phc2sys.2.config  ptp_ha_profile profile1 state 1
	if len(fields) < 5 {
		return
	}

	profile = fields[2]
	state, err = strconv.ParseInt(fields[4], 0, 64)
	if err != nil {
		log.Errorf("%s failed to parse ptp-ha state from the log %s error %v", processName, fields[4], err)
	}
	return
}

// GetSyncState ... get state id for metrics
func GetSyncState(state string) ptp.SyncState {
	switch state {
	case unLocked:
		return ptp.FREERUN
	case clockStep:
		return ptp.HOLDOVER
	case locked:
		return ptp.LOCKED
	default:
		return ptp.FREERUN
	}
}

// GetSyncStateID ... get state id for metrics
func GetSyncStateID(state string) float64 {
	// "0 = FREERUN, 1 = LOCKED, 2 = HOLDOVER",
	switch state {
	case unLocked:
		return 0
	case clockStep:
		return 2
	case locked:
		return 1
	default:
		return 0
	}
}
