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
	ptpConfigFileRegEx    = regexp.MustCompile(`ptp4l.[0-9]*.config`)
	ts2phcConfigFileRegEx = regexp.MustCompile(`ts2phc.[0-9]*.config`)
	// NodeName from the env
	ptpNodeName        = ""
	masterOffsetSource = ptp4lProcessName
)

const (
	ptpNamespace = "openshift"
	ptpSubsystem = "ptp"

	phc2sysProcessName = "phc2sys"
	ptp4lProcessName   = "ptp4l"
	ts2phcProcessName  = "ts2phc"

	unLocked  = "s0"
	clockStep = "s1"
	locked    = "s2"

	// FreeRunOffsetValue when sync state is FREERUN
	FreeRunOffsetValue = -9999999999999999
	// ClockRealTime is the slave
	ClockRealTime = "CLOCK_REALTIME"
	// MasterClockType is the slave sync slave clock to master
	MasterClockType = "master"
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
		log.Errorf("config name is not found in log outpt")
		return
	}

	if len(fields) < 3 {
		log.Errorf("ignoring log:log is not in required format ptp4l/phc2sys[time]: [config] %s", output)
		return
	}

	processName := fields[processNameIndex]
	configName := fields[configNameIndex]
	// if  is has ts2phc then it will replace it with ptp4l
	configName = strings.Replace(configName, ts2phcProcessName, ptp4lProcessName, 1)
	ptp4lCfg := p.GetPTPConfig(types.ConfigName(configName))
	// ptp stats goes by config either ptp4l ot pch2sys
	// master (slave) interface can be configured in ptp4l but  offset provided by ts2phc

	ptpStats := p.GetStats(types.ConfigName(configName))
	profileName := ptp4lCfg.Profile

	if len(ptp4lCfg.Interfaces) == 0 { //TODO: Use PMC to update port and roles
		log.Errorf("file watcher have not picked the files yet or ptp4l doesn't have config")
		return
	}
	if profileName == "" {
		log.Errorf("ptp4l config does not have profile name, aborting. ")
		return
	}

	var ptpInterface ptp4lconf.PTPInterface

	//  check if ptp4l or phc2sys process are dead
	// if process is down fire an event
	if strings.Contains(output, ptpProcessStatusIdentifier) {
		if status, e := parsePTPStatus(output, fields); e == nil {
			if status == PtpProcessDown {
				if masterOffsetSource == processName { // if the process is responsible to set master offset
					if m, ok := ptpStats[master]; ok {
						if m.Alias() != "" {
							masterResource := fmt.Sprintf("%s/%s", m.Alias(), MasterClockType)
							p.GenPTPEvent(profileName, m, masterResource, FreeRunOffsetValue, ptp.FREERUN, ptp.PtpStateChange)
						}
					}
				}
				if s, ok := ptpStats[ClockRealTime]; ok {
					if t, ok2 := p.PtpConfigMapUpdates.PtpProcessOpts[profileName]; ok2 && t.Phc2SysEnabled() {
						p.GenPTPEvent(profileName, s, ClockRealTime, FreeRunOffsetValue, ptp.FREERUN, ptp.OsClockSyncStateChange)
					}
				}
			}
		} else {
			log.Errorf("error in process status %s", output)
		}
		return
	}

	if strings.Contains(output, " max ") { // this get generated in case -u is passed as an option to phy2sys opts
		//TODO: ts2phc rms is validated
		interfaceName, ptpOffset, maxPtpOffset, frequencyAdjustment, delay := extractSummaryMetrics(processName, output)
		switch interfaceName {
		case ClockRealTime:
			UpdatePTPMetrics(phc, processName, interfaceName, ptpOffset, maxPtpOffset, frequencyAdjustment, delay)
		case MasterClockType:
			ptpInterface, _ = ptp4lCfg.ByRole(types.SLAVE)
			var alias string
			if ptpInterface.Name != "" {
				r := []rune(ptpInterface.Name)
				alias = string(r[:len(r)-1]) + "x"
				ptpStats[master].SetAlias(alias)
				UpdatePTPMetrics(master, processName, alias, ptpOffset, maxPtpOffset, frequencyAdjustment, delay)
			} else {
				// this should not happen
				log.Errorf("metrics found for empty interface %s", output)
			}
		default:
			if processName == ts2phcProcessName {
				var alias string
				r := []rune(interfaceName)
				alias = string(r[:len(r)-1]) + "x"
				ptpStats[master].SetAlias(alias)
				UpdatePTPMetrics(master, processName, alias, ptpOffset, maxPtpOffset, frequencyAdjustment, delay)
			}
		}
	} else if strings.Contains(output, " offset ") {
		// ptp4l[5196819.100]: [ptp4l.0.config] master offset   -2162130 s2 freq +22451884 path delay    374976
		// phc2sys[4268818.286]: [ptp4l.0.config] CLOCK_REALTIME phc offset       -62 s0 freq  -78368 delay   1100
		// phc2sys[4268818.287]: [ptp4l.0.config] ens5f1 phc offset       -92 s0 freq    -890 delay   2464   ( this is down)
		// phc2sys[4268818.287]: [ptp4l.0.config] ens5f0 phc offset       -47 s2 freq   -2047 delay   2438
		// ts2phc[82674.465]: [ts2phc.0.cfg] nmea delay: 88403525 ns
		// ts2phc[82674.465]: [ts2phc.0.cfg] ens2f1 extts index 0 at 1673031129.000000000 corr 0 src 1673031129.911642976 diff 0
		// ts2phc[82674.465]: [ts2phc.0.cfg] ens2f1 master offset          0 s2 freq      -0
		// Use threshold to CLOCK_REALTIME==SLAVE, rest send clock state to metrics no events

		interfaceName, ptpOffset, _, frequencyAdjustment, delay, syncState := extractRegularMetrics(processName, output)
		eventType := ptp.PtpStateChange
		if interfaceName == "" {
			return // don't do if iface not known
		}
		if processName != ts2phcProcessName && !(interfaceName == master || interfaceName == ClockRealTime) {
			return // only master and clock_realtime are supported
		}

		offsetSource := master
		if strings.Contains(output, "sys offset") {
			offsetSource = sys
		} else if strings.Contains(output, "phc offset") {
			offsetSource = phc
			eventType = ptp.OsClockSyncStateChange
		} else if !strings.Contains(output, ClockRealTime) {
			// set who is producing  master offset
			masterOffsetSource = processName //decided between ptp4l and ts2phc
		}
		// if it was ts2phc then it was considered as master offset
		interfaceType := types.IFace(master)
		if masterOffsetSource == ts2phcProcessName {
			ptpInterface = ptp4lconf.PTPInterface{Name: interfaceName}
		} else {
			// for ts2phc there is no slave interface configuration
			interfaceType = types.IFace(interfaceName)
			ptpInterface, _ = ptp4lCfg.ByRole(types.SLAVE)
		}

		if _, found := ptpStats[interfaceType]; !found {
			ptpStats[interfaceType] = stats.NewStats(configName)
		}
		// update metrics
		ptpStats[interfaceType].SetOffsetSource(offsetSource)
		ptpStats[interfaceType].SetProcessName(processName)
		ptpStats[interfaceType].SetFrequencyAdjustment(int64(frequencyAdjustment))
		ptpStats[interfaceType].SetDelay(int64(delay))

		switch interfaceName {
		case ClockRealTime: // CLOCK_REALTIME is active slave interface
			// copy  ClockRealTime value to current slave interface
			if r, ok := ptpStats[master]; ok && r.Role() == types.SLAVE { // publish event only if the master role is active
				// when related slave is faulty the holdover will make clock clear time as FREERUN
				p.GenPTPEvent(profileName, ptpStats[interfaceType], interfaceName, int64(ptpOffset), syncState, eventType)
			}
			// continue to update metrics regardless
			UpdateSyncStateMetrics(processName, interfaceName, ptpStats[interfaceType].LastSyncState())
			UpdatePTPMetrics(offsetSource, processName, interfaceName, ptpOffset, float64(ptpStats[interfaceType].MaxAbs()), frequencyAdjustment, delay)
		case MasterClockType: // this ptp4l[5196819.100]: [ptp4l.0.config] master offset   -2162130 s2 freq +22451884 path delay
			// Report events for master  by masking the index  number of the slave interface
			if ptpInterface.Name != "" {
				alias := ptpStats[interfaceType].Alias()
				if alias == "" {
					r := []rune(ptpInterface.Name)
					alias = string(r[:len(r)-1]) + "x"
					ptpStats[interfaceType].SetAlias(alias)
				}
				masterResource := fmt.Sprintf("%s/%s", alias, MasterClockType)
				p.GenPTPEvent(profileName, ptpStats[interfaceType], masterResource, int64(ptpOffset), syncState, eventType)
				UpdateSyncStateMetrics(processName, alias, ptpStats[interfaceType].LastSyncState())
				UpdatePTPMetrics(offsetSource, processName, alias, ptpOffset, float64(ptpStats[interfaceType].MaxAbs()),
					frequencyAdjustment, delay)
				ptpStats[interfaceType].AddValue(int64(ptpOffset))
			}
		default:
			if masterOffsetSource == ts2phcProcessName {
				var alias string
				r := []rune(interfaceName)
				alias = string(r[:len(r)-1]) + "x"
				ptpStats[master].SetAlias(alias)
				masterResource := fmt.Sprintf("%s/%s", alias, MasterClockType)
				p.GenPTPEvent(profileName, ptpStats[interfaceType], masterResource, int64(ptpOffset), syncState, eventType)
				UpdateSyncStateMetrics(processName, alias, ptpStats[interfaceType].LastSyncState())
				UpdatePTPMetrics(offsetSource, processName, alias, ptpOffset, float64(ptpStats[interfaceType].MaxAbs()),
					frequencyAdjustment, delay)
				ptpStats[interfaceType].AddValue(int64(ptpOffset))
			}
		}
	}
	if ptp4lProcessName == processName { // all we get from ptp4l is stats
		p.ParsePTP4l(processName, configName, profileName, output, fields,
			ptpInterface, ptp4lCfg, ptpStats)
	}
}
