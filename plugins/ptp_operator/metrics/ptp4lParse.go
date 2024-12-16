package metrics

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	ptpConfig "github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/config"
	"github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/ptp4lconf"
	"github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/stats"
	"github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/types"
	"github.com/redhat-cne/sdk-go/pkg/event/ptp"
	log "github.com/sirupsen/logrus"
)

var classChangeIdentifier = "CLOCK_CLASS_CHANGE"
var gnssEventIdentifier = "gnss_status"
var gmStatusIdentifier = "T-GM-STATUS"

// ParsePTP4l ... parse ptp4l for various events
func (p *PTPEventManager) ParsePTP4l(processName, configName, profileName, output string, fields []string,
	ptpInterface ptp4lconf.PTPInterface, ptp4lCfg *ptp4lconf.PTP4lConfig, ptpStats stats.PTPStats) {
	var err error
	if strings.Contains(output, classChangeIdentifier) {
		if len(fields) < 5 {
			log.Errorf("clock class not in right format %s", output)
			return
		}
		ptpStats.CheckSource(master, configName, ptp4lProcessName)
		// ptp4l 1646672953  ptp4l.0.config  CLOCK_CLASS_CHANGE 165.000000
		var clockClass float64
		clockClass, err = strconv.ParseFloat(fields[4], 64)
		if err != nil {
			log.Error("error parsing clock class change")
		} else {
			var alias string
			if m, ok := ptpStats[master]; ok {
				alias = m.Alias()
			}
			if alias == "" {
				alias, _ = ptp4lCfg.GetUnknownAlias()
			}
			masterResource := fmt.Sprintf("%s/%s", alias, MasterClockType)

			ClockClassMetrics.With(prometheus.Labels{
				"process": processName, "node": ptpNodeName}).Set(clockClass)
			if ptpStats[master].ClockClass() != int64(clockClass) {
				ptpStats[master].SetClockClass(int64(clockClass))
				p.PublishClockClassEvent(clockClass, masterResource, ptp.PtpClockClassChange)
			}
		}
	} else if strings.Contains(output, " port ") {
		portID, role, syncState := extractPTP4lEventState(output, isFollowerOnly(ptp4lCfg))
		if portID == 0 || role == types.UNKNOWN {
			return
		}

		if isFollowerOnly(ptp4lCfg) {
			syncState = followerOnlySyncState(role, portID, ptp4lCfg)
		}

		if ptpInterface, err = ptp4lCfg.ByPortID(portID); err != nil {
			log.Error(err)
			log.Errorf("possible error due to file watcher not updated")
			return
		}

		lastRole := ptpInterface.Role
		ptpIFace := ptpInterface.Name

		if role == types.SLAVE || masterOffsetSource == ts2phcProcessName {
			// initialize
			ptpStats.CheckSource(ClockRealTime, configName, phc2sysProcessName)
			ptpStats.CheckSource(master, configName, masterOffsetSource)
			ptpStats[master].SetRole(role)
		}

		if lastRole != role {
			/*
			   If role changes from FAULTY to SLAVE/PASSIVE/MASTER , then cancel HOLDOVER timer
			    and publish metrics
			*/
			/* with ts2phc enabled , when ptp4l reports port goes to faulty
			there is no HOLDOVER State
			*/
			// there are times when ptp is disabled in switch the slave port goes to master
			// based on its last role  as slave port  we should make sure we report HOLDOVER event
			if lastRole == types.FAULTY { // recovery
				if role == types.SLAVE { // cancel any HOLDOVER timeout, if new role is slave
					// Do not cancel any HOLDOVER until holodover times out or PTP sync state is back in locked state
					if masterOffsetSource == ptp4lProcessName && ptpStats[master].LastSyncState() == ptp.HOLDOVER {
						log.Infof("Interface %s is no longer faulty. The holdover state will remain active until the PTP sync state is detected as LOCKED or the holdover times out.", ptpIFace)
						// No events or metrics will  be generated instead will remain in Holdover state, when net iteration finds it in locked state
					}

					ptpStats[master].SetRole(role)
				}
			}
			log.Infof("update interface %s with portid %d from role %s to role %s", ptpIFace, portID, lastRole, role)
			ptp4lCfg.Interfaces[portID-1].UpdateRole(role)

			// update role metrics
			UpdateInterfaceRoleMetrics(processName, ptpIFace, role)
		}
		if lastRole != types.SLAVE { //tsphc doesnt have slave port and doesnt have fault state yet
			return // no need to go to holdover state if the Fault was not in master(slave) port
		}
		if _, ok := ptpStats[master]; !ok { //
			log.Errorf("no offset stats found for master for  portid %d with role %s (the port started in fault state)", portID, role)
			return
		}
		// Enter the HOLDOVER state: If current sycState is HOLDOVER(Role is at FAULTY) ,then spawn a go routine to hold the state until
		// holdover timeout, always put only master offset from ptp4l to HOLDOVER,when this goes to FREERUN
		// make any slave interface master offset to FREERUN
		// Only if master (slave port ) offset was reported by ptp4l
		if syncState != "" && syncState != ptpStats[master].LastSyncState() && syncState == ptp.HOLDOVER {
			// Put master in HOLDOVER state
			ptpStats[master].SetRole(types.FAULTY) // update slave port as faulty
			log.Infof("master process name %s and masteroffsetsource %s", ptpStats[master].ProcessName(), masterOffsetSource)
			if ptpStats[master].ProcessName() == masterOffsetSource {
				alias := ptpStats[master].Alias()
				masterResource := fmt.Sprintf("%s/%s", alias, MasterClockType)
				p.PublishEvent(syncState, ptpStats[master].LastOffset(), masterResource, ptp.PtpStateChange)
				ptpStats[master].SetLastSyncState(syncState)
				UpdateSyncStateMetrics(ptpStats[master].ProcessName(), alias, syncState)
				// Put CLOCK_REALTIME in FREERUN state
				var ptpOpts *ptpConfig.PtpProcessOpts
				var ok bool
				if ptpOpts, ok = p.PtpConfigMapUpdates.PtpProcessOpts[profileName]; ok && ptpOpts != nil && ptpOpts.Phc2SysEnabled() {
					p.PublishEvent(ptp.FREERUN, ptpStats[ClockRealTime].LastOffset(), ClockRealTime, ptp.OsClockSyncStateChange)
					ptpStats[ClockRealTime].SetLastSyncState(ptp.FREERUN)
					UpdateSyncStateMetrics(phc2sysProcessName, ClockRealTime, ptp.FREERUN)
				}
				threshold := p.PtpThreshold(profileName, true)
				go handleHoldOverState(p, ptpOpts, configName, profileName, threshold.HoldOverTimeout, MasterClockType, threshold.Close)
			}
		}
	}
}

func handleHoldOverState(ptpManager *PTPEventManager,
	ptpOpts *ptpConfig.PtpProcessOpts, configName,
	ptpProfileName string, holdoverTimeout int64,
	ptpIFace string, c chan struct{}) {
	defer func() {
		log.Infof("exiting holdover for interface %s", ptpIFace)
		if r := recover(); r != nil {
			fmt.Println("Recovered in f", r)
		}
	}()
	select {
	case <-c:
		log.Infof("call received to close holderover timeout")
		return
	case <-time.After(time.Duration(holdoverTimeout) * time.Second):
		log.Infof("time expired for interface %s", ptpIFace)
		ptpStats := ptpManager.GetStats(types.ConfigName(configName))
		if mStats, found := ptpStats[master]; found {
			if mStats.LastSyncState() == ptp.HOLDOVER { // if it was still in holdover while timing out then switch to FREERUN
				log.Infof("HOLDOVER timeout after %d secs,setting clock state to FREERUN from HOLDOVER state for %s",
					holdoverTimeout, master)
				masterResource := fmt.Sprintf("%s/%s", mStats.Alias(), MasterClockType)
				ptpManager.PublishEvent(ptp.FREERUN, ptpStats[MasterClockType].LastOffset(), masterResource, ptp.PtpStateChange)
				ptpStats[MasterClockType].SetLastSyncState(ptp.FREERUN)
				UpdateSyncStateMetrics(mStats.ProcessName(), mStats.Alias(), ptp.FREERUN)
				// don't check of os clock sync state if phc2 not enabled
				if cStats, ok := ptpStats[ClockRealTime]; ok && ptpOpts != nil && ptpOpts.Phc2SysEnabled() {
					ptpManager.GenPTPEvent(ptpProfileName, cStats, ClockRealTime, FreeRunOffsetValue, ptp.FREERUN, ptp.OsClockSyncStateChange)
					UpdateSyncStateMetrics(phc2sysProcessName, ClockRealTime, ptp.FREERUN)
				} else {
					log.Infof("phc2sys is not enabled for profile %s, skiping os clock syn state ", ptpProfileName)
				}
				// s.reset()
			}
		} else {
			log.Errorf("failed to switch from holdover, could not find ptpStats for interface %s", ptpIFace)
		}
	}
}

func isFollowerOnly(ptp4lCfg *ptp4lconf.PTP4lConfig) bool {
	if section, ok1 := ptp4lCfg.Sections["global"]; ok1 {
		if value, ok2 := section["slaveOnly"]; ok2 {
			log.Errorf("FollowerOnly scenario detected")
			return value == "1"
		}
	}
	return false
}

func followerOnlySyncState(role types.PtpPortRole, portID int, ptp4lCfg *ptp4lconf.PTP4lConfig) (outClockState ptp.SyncState) {
	activeFollower, err := ptp4lCfg.ByRole(types.SLAVE)
	activeFollowerPresent := err == nil
	var listeningFollower ptp4lconf.PTPInterface
	listeningFollower, err = ptp4lCfg.ByRole(types.LISTENING)
	listeningFollowerPresent := err == nil

	// If there is no port in FOLLOWING or SLAVE state after this transition then
	// syncstate is holdover
	if (!activeFollowerPresent || (activeFollower.PortID == portID && role == types.FAULTY)) &&
		(!listeningFollowerPresent || (listeningFollower.PortID == portID && role == types.FAULTY)) {
		return ptp.HOLDOVER
	}

	return ptp.FREERUN
}
