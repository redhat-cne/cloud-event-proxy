package metrics

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/ptp4lconf"
	"github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/stats"
	"github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/types"

	"github.com/redhat-cne/sdk-go/pkg/event/ptp"
	log "github.com/sirupsen/logrus"
)

var (
	configFileRegEx = regexp.MustCompile(`(ptp4l|ts2phc|phc2sys|synce4l)\.[0-9]*\.config`)
	// NodeName from the env
	ptpNodeName        = ""
	masterOffsetSource = ""
)

const (
	ptpNamespace = "openshift"
	ptpSubsystem = "ptp"

	phc2sysProcessName = "phc2sys"
	ptp4lProcessName   = "ptp4l"
	ts2phcProcessName  = "ts2phc"
	gnssProcessName    = "gnss"
	dpllProcessName    = "dpll"
	gmProcessName      = "GM"
	syncE4lProcessName = "synce4l"

	unLocked     = "s0"
	clockStep    = "s1"
	locked       = "s2"
	lockedStable = "s3"

	// FreeRunOffsetValue when sync state is FREERUN
	FreeRunOffsetValue = -9999999999999999
	// ClockRealTime is the slave
	ClockRealTime = "CLOCK_REALTIME"
	// MasterClockType is the slave sync slave clock to master
	MasterClockType = "master"
	// GNSS ...
	GNSS = "GNSS"
	// DPLL ...
	DPLL = "DPLL"
	// ClockClass number
	ClockClass = "CLOCK_CLASS"

	// from the logs
	processNameIndex = 0
	configNameIndex  = 2

	// from the logs
	offset = "offset"
	rms    = "rms"

	// offset source
	phc    = "phc"
	sys    = "sys"
	master = "master"
	// PtpProcessDown ... process is down
	PtpProcessDown int64 = 0
	// PtpProcessUp process is up
	PtpProcessUp int64 = 1

	// UNAVAILABLE Nmea and Pps status
	UNAVAILABLE float64 = 0
	// AVAILABLE Nmea and Pps status
	AVAILABLE float64 = 1

	gnssStatus      = "gnss_status"
	frequencyStatus = "frequency_status"
	phaseStatus     = "phase_status"
	ppsStatus       = "pps_status"
)

// ExtractMetrics ... extract metrics from ptp logs.
func (p *PTPEventManager) ExtractMetrics(msg string) {
	defer func() {
		if err := recover(); err != nil {
			log.Errorf("restored from extract metrics and events: %s", err)
			log.Errorf("failed to extract %s", msg)
		}
	}()
	replacer := strings.NewReplacer("[", " ", "]", " ", ":", " ")
	output := replacer.Replace(msg)
	fields := strings.Fields(output)
	// make sure configName is found in logs
	index := FindInLogForCfgFileIndex(output)
	if index == -1 {
		log.Errorf("config name is not found in log output %s", output)
		return
	}
	if len(fields) < 3 {
		log.Errorf("ignoring log:log is not in required format ptp4l/phc2sys[time]: [config] %s", output)
		return
	}
	processName := fields[processNameIndex]
	configName := fields[configNameIndex]
	// if log is having ts2phc then it will replace it with ptp4l
	configName = strings.Replace(configName, ts2phcProcessName, ptp4lProcessName, 1)
	ptp4lCfg := p.GetPTPConfig(types.ConfigName(configName))
	// ptp stats goes by config either ptp4l ot pch2sys
	// master (slave) interface can be configured in ptp4l but  offset provided by ts2phc
	ptpStats := p.GetStats(types.ConfigName(configName))
	profileName := ptp4lCfg.Profile
	//TODO: need better validation here
	if processName == syncE4lProcessName && configName == "" { // hack to skip for synce4l
		log.Infof("%s skipped parsing %s output %s\n", processName, configName, output)
		return
	} else if processName != syncE4lProcessName &&
		!p.validLogToProcess(profileName, processName, len(ptp4lCfg.Interfaces)) {
		log.Infof("%s skipped parsing %s output %s\n", processName, ptp4lCfg.Name, output)
		return
	}
	var ptpInterface ptp4lconf.PTPInterface
	// Initialize master and clock_realtime offset stats for the config
	if _, found := ptpStats[master]; found {
		// initialize master offset
		masterOffsetSource = ptpStats[master].ProcessName()
	}
	// if process is down fire an event
	if strings.Contains(output, ptpProcessStatusIdentifier) {
		if status, e := parsePTPStatus(output, fields); e == nil {
			if status == PtpProcessDown {
				p.processDownEvent(profileName, processName, ptpStats)
			}
		} else {
			log.Errorf("error in process status %s", output)
		}
		return
	}
	switch processName {
	case gnssProcessName:
		p.ParseGNSSLogs(processName, configName, output, fields, ptpStats)
	case dpllProcessName:
		p.ParseDPLLLogs(processName, configName, output, fields, ptpStats)
	case gmProcessName:
		p.ParseGMLogs(processName, configName, output, fields, ptpStats)
	case syncE4lProcessName:
		p.ParseSyncELogs(processName, configName, output, fields, ptpStats)
	default:
		if strings.Contains(output, " max ") { // this get generated in case -u is passed as an option to phy2sys opts
			//TODO: ts2phc rms is validated
			interfaceName, ptpOffset, maxPtpOffset, frequencyAdjustment, delay := extractSummaryMetrics(processName, output)
			switch interfaceName {
			case ClockRealTime:
				UpdatePTPMetrics(phc, processName, interfaceName, ptpOffset, maxPtpOffset, frequencyAdjustment, delay)
			case MasterClockType:
				ptpInterface, _ = ptp4lCfg.ByRole(types.SLAVE)
				if ptpInterface.Name != "" {
					alias := getAlias(ptpInterface.Name)
					ptpStats[master].SetAlias(alias)
					UpdatePTPMetrics(master, processName, alias, ptpOffset, maxPtpOffset, frequencyAdjustment, delay)
				} else {
					// this should not happen
					log.Errorf("metrics found for empty interface %s", output)
				}
			default:
				if processName == ts2phcProcessName {
					alias := getAlias(interfaceName)
					ptpStats[master].SetAlias(alias)
					UpdatePTPMetrics(master, processName, alias, ptpOffset, maxPtpOffset, frequencyAdjustment, delay)
				}
			}
		} else if strings.Contains(output, "nmea_status") &&
			processName == ts2phcProcessName {
			// ts2phc[1699929121]:[ts2phc.0.config] ens2f0 nmea_status 0 offset 999999 s0
			interfaceName, status, _, _ := extractNmeaMetrics(processName, output)
			// ts2phc return actual interface name unlike ptp4l
			ptpInterface = ptp4lconf.PTPInterface{Name: interfaceName}
			alias := getAlias(interfaceName)
			// no event for nmeas status , change in GM will manage ptp events and sync states
			UpdateNmeaStatusMetrics(processName, alias, status)
		} else if strings.Contains(output, "process_status") &&
			processName == ts2phcProcessName {
			// do nothing processDown identifier will update  metrics and stats
			// but prevent further from reading offsets
			return
		} else if strings.Contains(output, " offset ") { //  DPLL has Offset too
			// ptp4l[5196819.100]: [ptp4l.0.config] master offset   -2162130 s2 freq +22451884 path delay    374976
			// phc2sys[4268818.286]: [ptp4l.0.config] CLOCK_REALTIME phc offset       -62 s0 freq  -78368 delay   1100
			// phc2sys[4268818.287]: [ptp4l.0.config] ens5f0 phc offset       -47 s2 freq   -2047 delay   2438
			// ts2phc[82674.465]: [ts2phc.0.cfg] nmea delay: 88403525 ns
			// ts2phc[82674.465]: [ts2phc.0.cfg] ens2f1 extts index 0 at 1673031129.000000000 corr 0 src 1673031129.911642976 diff 0
			// ts2phc[82674.465]: [ts2phc.0.cfg] ens2f1 master offset          0 s2 freq      -0
			// ts2phc[82674.465]: ts2phc.0.config] ens7f0       offset         1  s3 freq      +1 holdover
			// Use threshold to CLOCK_REALTIME==SLAVE, rest send clock state to metrics no events
			// db                      | oc/bc/dual | ts2phc wo/GNSS     | ts2phc w/GNSS       | two card
			// --------------------------------------------------------------------------------------------
			// stats[master]           | No deps    |has ts2phc state/of | has GNSS deps+state | same
			// stats[CLOCK_REALTIME]   | no deps    |has clock_realtime  | same                | same
			// stats[ens01]            |  NA        | NA                 | has dpll deps + ts2phc offset + sate | same
			// | ineterfacename | Process
			// | master         | ptp4l
			// | CLOCK_REALTIME | phc2sys
			// | ens01          |  ts2phc (mostly)
			interfaceName, ptpOffset, _, frequencyAdjustment, delay, syncState := extractRegularMetrics(processName, output)
			if interfaceName == "" {
				return // don't do if iface not known
			}
			//  only ts2phc process will return actual interface name- allow all ts2phcprocess or iface in (master or  clock realtime)
			if processName != ts2phcProcessName && !(interfaceName == master || interfaceName == ClockRealTime) {
				return // only master and clock_realtime are supported
			}
			offsetSource := master
			if strings.Contains(output, "sys offset") {
				offsetSource = sys
			} else if strings.Contains(output, "phc offset") {
				offsetSource = phc
			}
			// here interfaceName will be master , clock_realtime and ens2f0
			// interfaceType will be of only two kind master and clock_realtime
			// ts2phc process always reports interface as of now
			if processName == ts2phcProcessName { // if current offset is read from ts2phc
				// ts2phc return actual interface name unlike ptp4l
				ptpInterface = ptp4lconf.PTPInterface{Name: interfaceName}
				// create GM interfaces
				ptpStats.CheckSource(master, configName, processName)
				// update process name in master since phc2sys looks for it
				ptpStats[master].SetProcessName(processName)
				ptpStats[master].SetOffsetSource(offsetSource)
			} else {
				// for ts2phc there is no slave interface configuration
				// fort pt4l find the slave configured
				ptpInterface, _ = ptp4lCfg.ByRole(types.SLAVE)
			}
			ptpStats.CheckSource(types.IFace(interfaceName), configName, processName)
			ptpStats[types.IFace(interfaceName)].SetOffsetSource(offsetSource)
			// Process Name will get Updated when master offset is ts2phc for master
			ptpStats[types.IFace(interfaceName)].SetProcessName(processName)
			ptpStats[types.IFace(interfaceName)].SetFrequencyAdjustment(int64(frequencyAdjustment))
			ptpStats[types.IFace(interfaceName)].SetDelay(int64(delay))
			// Handling GM clock state- syncState is used for events
			// IF its GM then synState is last syncState of GM
			// TODO: understand if the config is GM /BC /OC
			switch interfaceName { //note: this is not  interface type
			case ClockRealTime: // CLOCK_REALTIME is active slave interface
				//  for HA we can not rely on master ;since there will be 2 or more leaders; this condition will be skipped
				// ptpStats clock realtime has its own stats objects
				if r, ok := ptpStats[master]; ok && r.Role() == types.SLAVE { // publish event only if the master role is active
					// when related slave is faulty the holdover will make clock clear time as FREERUN
					p.GenPTPEvent(profileName, ptpStats[ClockRealTime], interfaceName, int64(ptpOffset), syncState, ptp.OsClockSyncStateChange)
				} else if masterOffsetSource == ts2phcProcessName {
					//TODO: once ts2phc events are identified we need to add that here, meaning if GM state is FREERUN then set OSClock as FREERUN
					// right now we are not managing os clock state based on GM state
					p.GenPTPEvent(profileName, ptpStats[ClockRealTime], interfaceName, int64(ptpOffset), syncState, ptp.OsClockSyncStateChange)
				}

				if _, ok := ptpStats[master]; ok { // ha wont have both master and clockreal_time
					ptpStats[ClockRealTime].SetAlias(ptpStats[master].Alias())
					p.GenPTPEvent(profileName, ptpStats[ClockRealTime], interfaceName, int64(ptpOffset), syncState, ptp.OsClockSyncStateChange)
				} else { // for HA to send event without monitoring leaders
					//TODO : manage leaders to trigger events in case of HA pick profile name as alias
					ptpStats[ClockRealTime].SetAlias("ptp-ha-enabled")
					p.GenPTPEvent(profileName, ptpStats[ClockRealTime], interfaceName, int64(ptpOffset), syncState, ptp.OsClockSyncStateChange)
				}
				// continue to update metrics regardless and stick to last sync state
				UpdateSyncStateMetrics(processName, interfaceName, ptpStats[ClockRealTime].LastSyncState())
				UpdatePTPMetrics(offsetSource, processName, interfaceName, ptpOffset, float64(ptpStats[ClockRealTime].MaxAbs()), frequencyAdjustment, delay)
			case MasterClockType: // this ptp4l[5196819.100]: [ptp4l.0.config] master offset   -2162130 s2 freq +22451884 path delay
				// Report events for master  by masking the index  number of the slave interface
				if ptpInterface.Name != "" {
					alias := ptpStats[types.IFace(interfaceName)].Alias()
					if alias == "" {
						alias = getAlias(ptpInterface.Name)
						ptpStats[types.IFace(interfaceName)].SetAlias(alias)
					}
					masterResource := fmt.Sprintf("%s/%s", alias, MasterClockType)
					p.GenPTPEvent(profileName, ptpStats[types.IFace(interfaceName)], masterResource, int64(ptpOffset), syncState, ptp.PtpStateChange)
					UpdateSyncStateMetrics(processName, alias, ptpStats[types.IFace(interfaceName)].LastSyncState())
					UpdatePTPMetrics(offsetSource, processName, alias, ptpOffset, float64(ptpStats[types.IFace(interfaceName)].MaxAbs()),
						frequencyAdjustment, delay)
					ptpStats[types.IFace(interfaceName)].AddValue(int64(ptpOffset))
				}
			default: // for ts2phc the master stats are not updated at all, so rely on interface
				if processName == ts2phcProcessName {
					alias := ptpStats[types.IFace(interfaceName)].Alias()
					if alias == "" {
						alias = getAlias(ptpInterface.Name)
						ptpStats[types.IFace(interfaceName)].SetAlias(alias)
					}
					// update ts2phc sync state to GM state if available,since GM State identifies PTP state
					// This identifies sync state of GM and adds ts2phc offset to verify if it has to stay in GM state or set new state
					// based on ts2phc offset threshold : Which is unnecessary but to avoid breaking existing logic
					// let the check happen again : GM state published by linuxptp-daemon already have checked ts2phc offset
					// TO GM State we need to know GM interface ; here MASTER stats will hold data of GM
					// and GM state will be held as dependant of master key
					masterResource := fmt.Sprintf("%s/%s", alias, MasterClockType)
					// use gm state to identify syncState
					// master will hold multiple ts2phc state as one state based on GM state
					// HANDLE case where there is no GM status but only ts2phc
					if !ptpStats[master].HasProcessEnabled(gmProcessName) {
						p.GenPTPEvent(profileName, ptpStats[types.IFace(interfaceName)], masterResource, int64(ptpOffset), syncState, ptp.PtpStateChange)
					} else {
						threshold := p.PtpThreshold(profileName, false)
						if syncState != ptp.HOLDOVER && !isOffsetInRange(int64(ptpOffset), threshold.MaxOffsetThreshold, threshold.MinOffsetThreshold) {
							syncState = ptp.FREERUN
						}
						ptpStats[types.IFace(interfaceName)].SetLastSyncState(syncState)
						ptpStats[types.IFace(interfaceName)].SetLastOffset(int64(ptpOffset))
						ptpStats[types.IFace(interfaceName)].AddValue(int64(ptpOffset))
					}
					UpdateSyncStateMetrics(processName, alias, ptpStats[types.IFace(interfaceName)].LastSyncState())
					UpdatePTPMetrics(offsetSource, processName, alias, ptpOffset, float64(ptpStats[types.IFace(interfaceName)].MaxAbs()),
						frequencyAdjustment, delay)
				}
			}
		} else if processName == phc2sysProcessName &&
			strings.Contains(output, "ptp_ha_profile") {
			if profile, state, _ := extractPTPHaMetrics(processName, output); state > -1 {
				UpdatePTPHaMetrics(profile, state)
			}
		}
	}
	p.ParsePTP4l(processName, configName, profileName, output, fields,
		ptpInterface, ptp4lCfg, ptpStats)
}

func (p *PTPEventManager) processDownEvent(profileName, processName string, ptpStats stats.PTPStats) {
	// if the process is responsible to set master offset
	if processName == ts2phcProcessName {
		//  update metrics for all interface defined by ts2phc
		//  set ts2phc stats to FREERUN
		// this should generate PTP stat as free run by event manager
		for iface := range ptpStats {
			if iface != ClockRealTime && iface != master {
				ptpStats[iface].SetLastOffset(FreeRunOffsetValue)
				ptpStats[iface].SetLastSyncState(ptp.FREERUN)
				alias := ptpStats[iface].Alias()
				if alias == "" {
					alias = getAlias(string(iface))
				}
				// update all ts2phc reported metrics as FREERUN
				UpdateSyncStateMetrics(processName, alias, ptpStats[iface].LastSyncState())
				UpdatePTPMetrics(master, processName, alias, FreeRunOffsetValue, float64(ptpStats[iface].MaxAbs()),
					float64(ptpStats[iface].FrequencyAdjustment()), float64(ptpStats[iface].Delay()))
			}
		}
	} else { // other profiles
		if masterOffsetSource == processName {
			if ptpStats[master].Alias() != "" {
				masterResource := fmt.Sprintf("%s/%s", ptpStats[master].Alias(), MasterClockType)
				p.GenPTPEvent(profileName, ptpStats[master], masterResource, FreeRunOffsetValue, ptp.FREERUN, ptp.PtpStateChange)
			}
		}
		if s, ok := ptpStats[ClockRealTime]; ok {
			if t, ok2 := p.PtpConfigMapUpdates.PtpProcessOpts[profileName]; ok2 && t.Phc2SysEnabled() {
				p.GenPTPEvent(profileName, s, ClockRealTime, FreeRunOffsetValue, ptp.FREERUN, ptp.OsClockSyncStateChange)
			}
		}
	}
}

func (p *PTPEventManager) validLogToProcess(profileName, processName string, iFaceSize int) bool {
	if profileName == "" {
		log.Infof("%s config does not have profile name, skipping. ", processName)
		return false
	}
	// phc2sys config for HA will not have any interface defined
	if iFaceSize == 0 && !p.IsHAProfile(profileName) { //TODO: Use PMC to update port and roles
		log.Errorf("file watcher have not picked the files yet or ptp4l doesn't have config for %s by process %s", profileName, processName)
		return false
	}
	return true
}

func getAlias(iface string) string {
	if iface == "" {
		return iface
	}
	dotIndex := strings.Index(iface, ".")
	if dotIndex == -1 {
		return iface[:len(iface)-1] + "x"
	}
	return iface[:dotIndex-1] + "x" + iface[dotIndex:]
}
