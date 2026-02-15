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
var bcStatusIdentifier = "T-BC-STATUS"

// clockClassFreeRunning is the default clock class indicating the clock is free-running
// (not locked to any reference). Per IEEE 1588, class 248 = default, 255 = slave-only.
const clockClassFreeRunning float64 = 248

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
		} else if ptpStats[master].ClockClass() != int64(clockClass) { // only if there is a change
			var alias string
			if m, ok := ptpStats[master]; ok {
				alias = m.Alias()
			}
			if alias == "" {
				alias, _ = ptp4lCfg.GetUnknownAlias()
			}
			masterResource := fmt.Sprintf("%s/%s", alias, MasterClockType)

			previousClockClass := ptpStats[master].ClockClass()
			ptpStats[master].SetClockClass(int64(clockClass))
			ClockClassMetrics.With(prometheus.Labels{
				"process": processName, "config": configName, "node": ptpNodeName}).Set(clockClass)
			p.PublishClockClassEvent(clockClass, masterResource, ptp.PtpClockClassChange)

			// Safety net for regular OC/BC only (not T-BC or T-GM which have their own state
			// management via T-BC-STATUS and T-GM-STATUS logs): if clock class degrades to
			// free-running (248+) and the sync state is stuck at HOLDOVER (e.g. because the
			// port event was missed or the holdover goroutine was never spawned), force the
			// transition to FREERUN.
			// ProfileType != NONE excludes T-BC; HasProcessEnabled("GM") excludes T-GM
			// (whose ProfileType is also NONE but has the GM process active).
			if ptp4lCfg.ProfileType == ptp4lconf.NONE &&
				!ptpStats[master].HasProcessEnabled(gmProcessName) &&
				clockClass >= clockClassFreeRunning && previousClockClass < int64(clockClassFreeRunning) {
				currentSyncState := ptpStats[master].LastSyncState()
				if currentSyncState == ptp.HOLDOVER || currentSyncState == ptp.LOCKED {
					log.Infof("clock class changed from %d to %.0f (free-running), current sync state is %s, forcing FREERUN for %s",
						previousClockClass, clockClass, currentSyncState, masterResource)
					ptpStats[master].SetLastSyncState(ptp.FREERUN)
					UpdateSyncStateMetrics(ptp4lProcessName, alias, ptp.FREERUN)
					p.PublishEvent(ptp.FREERUN, FreeRunOffsetValue, masterResource, ptp.PtpStateChange)
					// Also update CLOCK_REALTIME if phc2sys stats exist
					if _, ok := ptpStats[ClockRealTime]; ok {
						ptpStats[ClockRealTime].SetLastSyncState(ptp.FREERUN)
						UpdateSyncStateMetrics(phc2sysProcessName, ClockRealTime, ptp.FREERUN)
						p.PublishEvent(ptp.FREERUN, FreeRunOffsetValue, ClockRealTime, ptp.OsClockSyncStateChange)
					}
				}
			}
		}
	} else if strings.Contains(output, " port ") && processName == ptp4lProcessName { // ignore anything reported by other process
		log.Infof("output: %s", output)
		portID, role, syncState := extractPTP4lEventState(output)
		if portID == 0 || role == types.UNKNOWN {
			// DEBUG: Log ignored port events (like Announce timeout)
			log.Infof("DEBUG_HOLDOVER: Port event ignored (portID=%d, role=UNKNOWN) - output: %s", portID, output)
			return
		}
		// DEBUG: Log parsed port event
		log.Infof("DEBUG_HOLDOVER: Port event parsed - portID=%d, role=%d, syncState=%s, config=%s, profile=%s",
			portID, role, syncState, configName, ptp4lCfg.Profile)

		if ptpInterface, err = ptp4lCfg.ByPortID(portID); err != nil {
			log.Errorf("DEBUG_HOLDOVER: ByPortID(%d) FAILED for config=%s, profile=%s, numInterfaces=%d - %v",
				portID, configName, ptp4lCfg.Profile, len(ptp4lCfg.Interfaces), err)
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
		log.Infof("DEBUG_HOLDOVER: lastRole=%d, newRole=%d, iface=%s, masterOffsetSource=%s", lastRole, role, ptpIFace, masterOffsetSource)
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
		if ptp4lCfg.ProfileType == ptp4lconf.TBC {
			log.Infof("TBC: ptp4lCfg.ProfileType: %s", ptp4lCfg.ProfileType)
			// For TBC: set ptp4l clock_state to FREERUN when port goes FAULTY
			if role == types.FAULTY && lastRole == types.SLAVE {
				if _, ok := ptpStats[master]; ok {
					alias := ptpStats[master].Alias()
					if alias != "" {
						ptpStats[master].SetLastSyncState(ptp.FREERUN)
						UpdateSyncStateMetrics(processName, alias, ptp.FREERUN)
						log.Infof("TBC: ptp4l port %s went FAULTY, setting clock_state to FREERUN", ptpIFace)
					}
				}
			}
			log.Infof("DEBUG_HOLDOVER: Returning early - profileType is TBC for config=%s", configName)
			return // no need to go to holdover state for TBC
		}
		if lastRole != types.SLAVE { //tsphc doesn't have slave port and doesnt have fault state yet
			log.Infof("DEBUG_HOLDOVER: Returning early - lastRole=%d is not SLAVE for iface=%s, config=%s", lastRole, ptpIFace, configName)
			return // no need to go to holdover state if the Fault was not in master(slave) port
		}

		if _, ok := ptpStats[master]; !ok { //
			log.Errorf("DEBUG_HOLDOVER: Returning early - no ptpStats[master] for portID=%d, role=%d, config=%s", portID, role, configName)
			log.Errorf("no offset stats found for master for  portid %d with role %s (the port started in fault state)", portID, role)
			return
		}
		// Enter the HOLDOVER state: If current sycState is HOLDOVER(Role is at FAULTY) ,then spawn a go routine to hold the state until
		// holdover timeout, always put only master offset from ptp4l to HOLDOVER,when this goes to FREERUN
		// make any slave interface master offset to FREERUN
		// Only if master (slave port ) offset was reported by ptp4l
		// DEBUG: Log the holdover entry condition check
		log.Infof("DEBUG_HOLDOVER: Port event holdover check - syncState=%s, LastSyncState=%s, condition(syncState!=LastSyncState)=%v, role=%d, lastRole=%d",
			syncState, ptpStats[master].LastSyncState(), syncState != ptpStats[master].LastSyncState(), role, lastRole)
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
				log.Infof("DEBUG_HOLDOVER: PublishEvent syncState: %s, lastOffset: %d, masterResource: %s", syncState, ptpStats[master].LastOffset(), masterResource)
				if ptpOpts, ok := p.PtpConfigMapUpdates.PtpProcessOpts[profileName]; ok && ptpOpts != nil {

					p.maybePublishOSClockSyncStateChangeEvent(ptpOpts, configName, profileName)
					threshold := p.PtpThreshold(profileName, true)
					log.Infof("DEBUG_HOLDOVER: threshhold: %+v, ptpOpts: %+v, ok %v", threshold, ptpOpts, ok)
					if p.mock {
						log.Infof("mock holdover is set to %s", ptpStats[MasterClockType].Alias())
					} else {
						// DEBUG: Log holdover goroutine spawn
						log.Infof("DEBUG_HOLDOVER: Spawning holdover goroutine for config=%s, profile=%s, iface=%s, timeout=%d",
							configName, profileName, ptpStats[MasterClockType].Alias(), threshold.HoldOverTimeout)
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
	// DEBUG: Log goroutine start
	log.Infof("DEBUG_HOLDOVER: Holdover goroutine started for config=%s, profile=%s, iface=%s, timeout=%d",
		configName, ptpProfileName, ptpIFace, holdoverTimeout)
	select {
	case <-c:
		log.Infof("DEBUG_HOLDOVER: Holdover cancelled via channel for config=%s, profile=%s, iface=%s",
			configName, ptpProfileName, ptpIFace)
		return
	case <-time.After(time.Duration(holdoverTimeout) * time.Second):
		log.Infof("DEBUG_HOLDOVER: Holdover timeout expired for config=%s, profile=%s, iface=%s",
			configName, ptpProfileName, ptpIFace)
		ptpStats := ptpManager.GetStats(types.ConfigName(configName))
		// DEBUG: Log what we found
		if ptpStats == nil {
			log.Errorf("DEBUG_HOLDOVER: GetStats returned nil for config=%s", configName)
			return
		}
		log.Infof("DEBUG_HOLDOVER: Stats found for config=%s, checking master entry", configName)
		if mStats, found := ptpStats[master]; found {
			currentState := mStats.LastSyncState()
			log.Infof("DEBUG_HOLDOVER: Master stats found - alias=%s, process=%s, currentState=%s",
				mStats.Alias(), mStats.ProcessName(), currentState)
			if currentState == ptp.HOLDOVER { // if it was still in holdover while timing out then switch to FREERUN
				log.Infof("HOLDOVER timeout after %d secs,setting clock state to FREERUN from HOLDOVER state for %s",
					holdoverTimeout, master)
				masterResource := fmt.Sprintf("%s/%s", mStats.Alias(), MasterClockType)
				ptpStats[MasterClockType].SetLastSyncState(ptp.FREERUN)
				ptpManager.PublishEvent(ptp.FREERUN, ptpStats[MasterClockType].LastOffset(), masterResource, ptp.PtpStateChange)
				log.Infof("DEBUG_HOLDOVER: About to update SyncState metric - process=%s, iface=%s, state=FREERUN",
					mStats.ProcessName(), mStats.Alias())
				UpdateSyncStateMetrics(mStats.ProcessName(), mStats.Alias(), ptp.FREERUN)
				// don't check of os clock sync state if phc2 not enabled
				ptpManager.maybePublishOSClockSyncStateChangeEvent(ptpOpts, configName, ptpProfileName)
			} else {
				log.Infof("DEBUG_HOLDOVER: Master state is %s (not HOLDOVER), no action needed", currentState)
			}
		} else {
			log.Errorf("DEBUG_HOLDOVER: failed to switch from holdover, could not find ptpStats[master] for config=%s, iface=%s. Available keys: %v",
				configName, ptpIFace, getStatsKeys(ptpStats))
		}
	}
}

// DEBUG helper function
func getStatsKeys(ptpStats stats.PTPStats) []string {
	keys := make([]string, 0, len(ptpStats))
	for k := range ptpStats {
		keys = append(keys, string(k))
	}
	return keys
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
