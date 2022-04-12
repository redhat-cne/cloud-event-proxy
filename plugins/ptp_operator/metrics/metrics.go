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
	ptpConfigFileRegEx = regexp.MustCompile(`ptp4l.[0-9]*.config`)
	// NodeName from the env
	ptpNodeName = ""
)

const (
	ptpNamespace = "openshift"
	ptpSubsystem = "ptp"

	phc2sysProcessName = "phc2sys"
	ptp4lProcessName   = "ptp4l"

	unLocked  = "s0"
	clockStep = "s1"
	locked    = "s2"

	// FreeRunOffsetValue when sync state is FREERUN
	FreeRunOffsetValue = -9999999999999999
	// ClockRealTime is the slave
	ClockRealTime = "CLOCK_REALTIME"
	// MasterClockType is the slave sync slave clock to master
	MasterClockType = "master"

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
	ptp4lCfg := p.GetPTPConfig(types.ConfigName(configName))
	ptpStats := p.GetStats(types.ConfigName(configName))
	profileName := ptp4lCfg.Profile

	if len(ptp4lCfg.Interfaces) == 0 { //TODO: Use PMC to update port and roles
		log.Errorf("file watcher have not picked the files yet")
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
				if m, ok := ptpStats[master]; ok {
					masterResource := fmt.Sprintf("%s/%s", m.Alias(), MasterClockType)
					p.GenPTPEvent(profileName, m, masterResource, FreeRunOffsetValue, ptp.FREERUN, ptp.PtpStateChange)
				}
				if s, ok := ptpStats[ClockRealTime]; ok {
					if t, ok := p.PtpConfigMapUpdates.PtpProcessOpts[profileName]; ok && t.Phc2SysEnabled() {
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
		}
	} else if strings.Contains(output, " offset ") {
		// ptp4l[5196819.100]: [ptp4l.0.config] master offset   -2162130 s2 freq +22451884 path delay    374976
		// phc2sys[4268818.286]: [ptp4l.0.config] CLOCK_REALTIME phc offset       -62 s0 freq  -78368 delay   1100
		// phc2sys[4268818.287]: [ptp4l.0.config] ens5f1 phc offset       -92 s0 freq    -890 delay   2464   ( this is down)
		// phc2sys[4268818.287]: [ptp4l.0.config] ens5f0 phc offset       -47 s2 freq   -2047 delay   2438
		// Use threshold to CLOCK_REALTIME==SLAVE, rest send clock state to metrics no events
		interfaceName, ptpOffset, _, frequencyAdjustment, delay, syncState := extractRegularMetrics(processName, output)
		eventType := ptp.PtpStateChange
		if interfaceName == "" {
			return // don't do if iface not known
		}
		if !(interfaceName == master || interfaceName == ClockRealTime) {
			return // only master and clock_realtime are supported
		}
		offsetSource := master
		if strings.Contains(output, "sys offset") {
			offsetSource = sys
		} else if strings.Contains(output, "phc offset") {
			offsetSource = phc
			eventType = ptp.OsClockSyncStateChange
		}

		interfaceType := types.IFace(interfaceName)

		if _, found := ptpStats[interfaceType]; !found {
			ptpStats[interfaceType] = stats.NewStats(configName)
		}
		// update metrics
		ptpStats[interfaceType].SetOffsetSource(offsetSource)
		ptpStats[interfaceType].SetProcessName(processName)
		ptpStats[interfaceType].SetFrequencyAdjustment(int64(frequencyAdjustment))
		ptpStats[interfaceType].SetDelay(int64(delay))

		ptpInterface, _ = ptp4lCfg.ByRole(types.SLAVE)

		switch interfaceName {
		case ClockRealTime: // CLOCK_REALTIME is active slave interface
			// copy  ClockRealTime value to current slave interface
			p.GenPTPEvent(profileName, ptpStats[interfaceType], interfaceName, int64(ptpOffset), syncState, eventType)
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
		}
	}
	if ptp4lProcessName == processName { // all we get from ptp4l is stats
		p.ParsePTP4l(processName, configName, profileName, output, fields,
			ptpInterface, ptp4lCfg, ptpStats)
	}

}
