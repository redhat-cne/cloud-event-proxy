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
	} else if strings.Contains(output, " port ") && processName == ptp4lProcessName { // ignore anything reported by other process
		portID, role, syncState := extractPTP4lEventState(output)
		if portID == 0 || role == types.UNKNOWN {
			return
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
			log.Infof("update interface %s with portid %d from role %s to role %s  out %s", ptpIFace, portID, lastRole, role, output)
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
				ptpStats[master].SetLastSyncState(syncState)
				p.PublishEvent(syncState, ptpStats[master].LastOffset(), masterResource, ptp.PtpStateChange)
				UpdateSyncStateMetrics(ptpStats[master].ProcessName(), alias, syncState)
				if ptpOpts, ok := p.PtpConfigMapUpdates.PtpProcessOpts[profileName]; ok && ptpOpts != nil {
					p.maybePublishOSClockSyncStateChangeEvent(ptpOpts, configName, profileName)
					threshold := p.PtpThreshold(profileName, true)
					if p.mock {
						log.Infof("mock holdover is set to %s", ptpStats[MasterClockType].Alias())
					} else {
						go handleHoldOverState(p, ptpOpts, configName, profileName, threshold.HoldOverTimeout, ptpStats[MasterClockType].Alias(), threshold.Close)
					}
				}
			}
		}
	}
}

func handleHoldOverState(ptpManager *PTPEventManager,
	ptpOpts *ptpConfig.PtpProcessOpts, configName,
	ptpProfileName string, holdoverTimeout int64,
	ptpIFace string, c chan struct{}) {
	defer func() {
		log.Infof("exiting holdover for profile %s with interface %s", ptpProfileName, ptpIFace)
		if r := recover(); r != nil {
			fmt.Println("Recovered in f", r)
		}
	}()
	select {
	case <-c:
		log.Infof("call received to close holderover timeout")
		return
	case <-time.After(time.Duration(holdoverTimeout) * time.Second):
		log.Infof("holdover time expired for interface %s", ptpIFace)
		ptpStats := ptpManager.GetStats(types.ConfigName(configName))
		if mStats, found := ptpStats[master]; found {
			if mStats.LastSyncState() == ptp.HOLDOVER { // if it was still in holdover while timing out then switch to FREERUN
				log.Infof("HOLDOVER timeout after %d secs,setting clock state to FREERUN from HOLDOVER state for %s",
					holdoverTimeout, master)
				masterResource := fmt.Sprintf("%s/%s", mStats.Alias(), MasterClockType)
				ptpStats[MasterClockType].SetLastSyncState(ptp.FREERUN)
				ptpManager.PublishEvent(ptp.FREERUN, ptpStats[MasterClockType].LastOffset(), masterResource, ptp.PtpStateChange)
				UpdateSyncStateMetrics(mStats.ProcessName(), mStats.Alias(), ptp.FREERUN)
				// don't check of os clock sync state if phc2 not enabled
				ptpManager.maybePublishOSClockSyncStateChangeEvent(ptpOpts, configName, ptpProfileName)
			}
		} else {
			log.Errorf("failed to switch from holdover, could not find ptpStats for interface %s", ptpIFace)
		}
	}
}

func (p *PTPEventManager) maybePublishOSClockSyncStateChangeEvent(
	ptpOpts *ptpConfig.PtpProcessOpts, configName, ptpProfileName string) {
	if ptpOpts == nil {
		log.Error("No profile found in configuration; OS clock sync state change event not published.")
		return
	}

	// Already in a synced state, check if we need to emit FREERUN event
	publish := false
	haProfile, haProfiles := p.ListHAProfilesWith(ptpProfileName)

	if ptpOpts.Phc2SysEnabled() {
		publish = true
	} else if len(haProfiles) > 0 { // Check if we are in a HA profile
		haConfigName := p.GetPTPConfigByProfile(haProfile)

		// Proceed only if we were able to retrieve the system clock config
		if len(haConfigName) > 0 {
			configName = haConfigName // change to phc2sys config name
			for _, hProfile := range haProfiles {
				if hProfile == ptpProfileName {
					continue // Skip current (already known to be faulty)
				}
				checkConfig := p.GetPTPConfigByProfile(hProfile)
				if len(checkConfig) == 0 {
					continue
				}

				if haStats, exists := p.GetStats(types.ConfigName(checkConfig))[master]; exists && haStats.Role() == types.SLAVE {
					log.Infof("HA profile %s is still in SLAVE state, not setting CLOCK_REALTIME to FREERUN", hProfile)
					return
				}
			}
			// If all other HA profiles are non-SLAVE or missing, we can publish
			publish = true
			// set to the one that is in the HA profile
			ptpProfileName = haProfile
		} else {
			return // No HA profile found, nothing to publish
		}
	}

	ptpStats := p.GetStats(types.ConfigName(configName))
	cStats, ok := ptpStats[ClockRealTime]
	if !ok {
		// ClockRealTime stats not available, nothing to publish
		return
	}
	if cStats.LastSyncState() == ptp.FREERUN {
		// Already in FREERUN, no need to publish again
		return
	}

	if publish {
		if p.mock {
			p.mockEvent = []ptp.EventType{ptp.OsClockSyncStateChange}
			log.Infof("PublishEvent state=%s, ptpOffset=%d, source=%s, eventType=%s", ptp.FREERUN, FreeRunOffsetValue, ClockRealTime, ptp.OsClockSyncStateChange)
			return
		}
		p.GenPTPEvent(ptpProfileName, cStats, ClockRealTime, FreeRunOffsetValue, ptp.FREERUN, ptp.OsClockSyncStateChange)
		UpdateSyncStateMetrics(phc2sysProcessName, ClockRealTime, ptp.FREERUN)
	}
}
