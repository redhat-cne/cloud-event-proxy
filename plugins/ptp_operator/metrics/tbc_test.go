package metrics_test

import (
	"strings"
	"testing"

	"github.com/redhat-cne/cloud-event-proxy/pkg/common"
	ptpConfig "github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/config"
	"github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/metrics"
	"github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/ptp4lconf"
	"github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/stats"
	"github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/types"
	"github.com/redhat-cne/sdk-go/pkg/event/ptp"
	"github.com/stretchr/testify/assert"
	"k8s.io/utils/pointer"
)

// TestTBCProfileDetection tests that TBC profiles are correctly identified
func TestTBCProfileDetection(t *testing.T) {
	tests := []struct {
		name           string
		ts2phcOpts     *string
		ptpSettings    map[string]string
		expectedInList bool
	}{
		{
			name:           "Profile with external_pps",
			ts2phcOpts:     pointer.String("-s generic -m -f /var/run/ptp4l.0.config --external_pps"),
			ptpSettings:    map[string]string{},
			expectedInList: true,
		},
		{
			name:           "Profile with controllingProfile setting",
			ts2phcOpts:     nil,
			ptpSettings:    map[string]string{"controllingProfile": "01-tbc-tr"},
			expectedInList: true,
		},
		{
			name:           "Profile without TBC markers",
			ts2phcOpts:     pointer.String("-s generic -m -f /var/run/ptp4l.0.config"),
			ptpSettings:    map[string]string{},
			expectedInList: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configUpdate := ptpConfig.NewLinuxPTPConfUpdate()

			profile := ptpConfig.PtpProfile{
				Name:        pointer.String("test-profile"),
				TS2PhcOpts:  tt.ts2phcOpts,
				PtpSettings: tt.ptpSettings,
			}
			configUpdate.NodeProfiles = []ptpConfig.PtpProfile{profile}

			// Simulate the update flow
			configUpdate.UpdatePTPProcessOptions()
			configUpdate.UpdatePTPSetting()

			found := false
			for _, profileName := range configUpdate.TBCProfiles {
				if profileName == "test-profile" {
					found = true
					break
				}
			}

			assert.Equal(t, tt.expectedInList, found, "TBC profile detection mismatch")
		})
	}
}

// TestGetProfileType tests the GetProfileType function
func TestGetProfileType(t *testing.T) {
	eventManager := metrics.NewPTPEventManager("", initPubSubTypes(), "testnode", &common.SCConfiguration{StorePath: "/tmp/store"})

	// Setup TBCProfiles
	eventManager.PtpConfigMapUpdates.TBCProfiles = []string{"tbc-profile-1", "tbc-profile-2"}

	tests := []struct {
		name         string
		profileName  string
		expectedType ptp4lconf.PtpProfileType
	}{
		{
			name:         "TBC profile in list",
			profileName:  "tbc-profile-1",
			expectedType: ptp4lconf.TBC,
		},
		{
			name:         "Another TBC profile in list",
			profileName:  "tbc-profile-2",
			expectedType: ptp4lconf.TBC,
		},
		{
			name:         "Non-TBC profile",
			profileName:  "regular-profile",
			expectedType: ptp4lconf.NONE,
		},
		{
			name:         "Empty profile name",
			profileName:  "",
			expectedType: ptp4lconf.NONE,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := eventManager.GetProfileType(tt.profileName)
			assert.Equal(t, tt.expectedType, result)
		})
	}
}

// TestTBCPtp4lMasterOffsetNoHoldover tests that TBC profiles don't enter HOLDOVER from ptp4l master offset
func TestTBCPtp4lMasterOffsetNoHoldover(t *testing.T) {
	eventManager := metrics.NewPTPEventManager("", initPubSubTypes(), "testnode", &common.SCConfiguration{StorePath: "/tmp/store"})
	eventManager.MockTest(true)

	configName = "ptp4l.0.config" //nolint:goconst // reused test config name
	tbcProfile := "tbc-test-profile"

	// Setup TBC profile
	eventManager.PtpConfigMapUpdates.TBCProfiles = []string{tbcProfile}

	ptp4lCfg := &ptp4lconf.PTP4lConfig{
		Name:        configName,
		Profile:     tbcProfile,
		ProfileType: ptp4lconf.TBC,
		Interfaces: []*ptp4lconf.PTPInterface{
			{
				Name:     "ens2f0",
				PortID:   1,
				PortName: "port 1",
				Role:     types.SLAVE,
			},
		},
	}
	eventManager.AddPTPConfig(types.ConfigName(configName), ptp4lCfg)

	ptpStats := eventManager.GetStats(types.ConfigName(configName))
	ptpStats[metrics.MasterClockType] = &stats.Stats{}
	ptpStats[metrics.MasterClockType].SetAlias("ens2f0")

	tests := []struct {
		name          string
		logLine       string
		expectedState ptp.SyncState
	}{
		{
			name:          "TBC ptp4l with s0 should be FREERUN",
			logLine:       "ptp4l[5196819.100]: [ptp4l.0.config] master offset -100 s0 freq +1000 path delay 1000",
			expectedState: ptp.FREERUN,
		},
		{
			name:          "TBC ptp4l with s2 should be LOCKED",
			logLine:       "ptp4l[5196819.100]: [ptp4l.0.config] master offset -50 s2 freq +500 path delay 1000",
			expectedState: ptp.LOCKED,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eventManager.ExtractMetrics(tt.logLine)

			// Verify the state was set correctly
			lastState := ptpStats[metrics.MasterClockType].LastSyncState()
			assert.Equal(t, tt.expectedState, lastState, "TBC ptp4l clock state should match log state")

			// Verify it's never HOLDOVER
			assert.NotEqual(t, ptp.HOLDOVER, lastState, "TBC ptp4l should never be in HOLDOVER")
		})
	}
}

// TestTBCProfileTypeSetBeforeTBCProfilesPopulated reproduces the startup race where
// loadInitialPtp4lConfigs calls GetProfileType before UpdatePTPSetting has populated
// TBCProfiles. This causes the profile to be classified as NONE even when the config
// has rh_external_pps in ts2phcOpts.
func TestTBCProfileTypeSetBeforeTBCProfilesPopulated(t *testing.T) {
	profileName := "tsc-holdover"
	cfgName := "ptp4l.0.config"

	eventManager := metrics.NewPTPEventManager("", initPubSubTypes(), "testnode", &common.SCConfiguration{StorePath: "/tmp/store"})

	eventManager.PtpConfigMapUpdates.NodeProfiles = []ptpConfig.PtpProfile{
		{
			Name:        pointer.String(profileName),
			Phc2sysOpts: pointer.String("-r -n 24 -N 8 -R 16 -u 0 -m -s ens3f2"),
			TS2PhcOpts:  pointer.String("-s generic -a --ts2phc.rh_external_pps 1"),
			PtpSettings: map[string]string{"logReduce": "false"},
		},
	}
	t.Logf("NodeProfiles configured: name=%s, ts2phcOpts=%q", profileName,
		*eventManager.PtpConfigMapUpdates.NodeProfiles[0].TS2PhcOpts)
	t.Logf("TBCProfiles before UpdatePTPSetting: %v", eventManager.PtpConfigMapUpdates.TBCProfiles)

	// Step 1: Simulate loadInitialPtp4lConfigs — register the ptp4l config and set
	// ProfileType BEFORE UpdatePTPSetting has run (TBCProfiles is still nil).
	ptp4lCfg := &ptp4lconf.PTP4lConfig{
		Name:    cfgName,
		Profile: profileName,
		Interfaces: []*ptp4lconf.PTPInterface{
			{Name: "ens3f2", PortID: 1, PortName: "port 1", Role: types.SLAVE},
		},
	}
	ptp4lCfg.ProfileType = eventManager.GetProfileType(ptp4lCfg.Profile)
	t.Logf("GetProfileType(%q) returned: %v (TBCProfiles is %v)",
		profileName, ptp4lCfg.ProfileType, eventManager.PtpConfigMapUpdates.TBCProfiles)
	eventManager.AddPTPConfig(types.ConfigName(cfgName), ptp4lCfg)

	storedCfg := eventManager.GetPTPConfig(types.ConfigName(cfgName))
	t.Logf("Stored ProfileType after loadInitialPtp4lConfigs: %v", storedCfg.ProfileType)
	assert.Equal(t, ptp4lconf.NONE, storedCfg.ProfileType,
		"BUG REPRODUCED: ProfileType is NONE because GetProfileType ran before UpdatePTPSetting")

	// Step 2: Now simulate the configmap handler goroutine — runs AFTER loadInitialPtp4lConfigs
	eventManager.PtpConfigMapUpdates.UpdatePTPProcessOptions()
	eventManager.PtpConfigMapUpdates.UpdatePTPSetting()
	t.Logf("TBCProfiles after UpdatePTPSetting: %v", eventManager.PtpConfigMapUpdates.TBCProfiles)

	profileType := eventManager.GetProfileType(profileName)
	t.Logf("GetProfileType(%q) now returns: %v", profileName, profileType)
	assert.Equal(t, ptp4lconf.TBC, profileType,
		"GetProfileType should return TBC after UpdatePTPSetting runs")

	storedCfg = eventManager.GetPTPConfig(types.ConfigName(cfgName))
	t.Logf("Stored ProfileType after UpdatePTPSetting: %v (never re-evaluated)", storedCfg.ProfileType)
	assert.Equal(t, ptp4lconf.NONE, storedCfg.ProfileType,
		"BUG REPRODUCED: stored ProfileType remains NONE even after TBCProfiles is populated")
}

// TestTBCFaultyPortSpuriousFreerun reproduces the end-to-end bug: when a ptp4l port
// goes SLAVE to FAULTY on a TBC profile that wasn't detected as TBC (due to the startup
// race), a spurious FREERUN event is published for CLOCK_REALTIME even though phc2sys
// is still locked.
func TestTBCFaultyPortSpuriousFreerun(t *testing.T) {
	profileName := "tsc-holdover"
	cfgName := "ptp4l.0.config"

	eventManager := metrics.NewPTPEventManager("", initPubSubTypes(), "testnode", &common.SCConfiguration{StorePath: "/tmp/store"})
	eventManager.MockTest(true)

	eventManager.PtpConfigMapUpdates.NodeProfiles = []ptpConfig.PtpProfile{
		{
			Name:        pointer.String(profileName),
			Phc2sysOpts: pointer.String("-r -n 24 -N 8 -R 16 -u 0 -m -s ens3f2"),
			TS2PhcOpts:  pointer.String("-s generic -a --ts2phc.rh_external_pps 1"),
			PtpSettings: map[string]string{"logReduce": "false"},
		},
	}

	// Reproduce startup race: register config before UpdatePTPSetting
	ptp4lCfg := &ptp4lconf.PTP4lConfig{
		Name:    cfgName,
		Profile: profileName,
		Interfaces: []*ptp4lconf.PTPInterface{
			{Name: "ens3f2", PortID: 1, PortName: "port 1", Role: types.SLAVE},
		},
	}
	ptp4lCfg.ProfileType = eventManager.GetProfileType(ptp4lCfg.Profile)
	t.Logf("--- SETUP ---")
	t.Logf("ProfileType after GetProfileType (before UpdatePTPSetting): %v", ptp4lCfg.ProfileType)
	eventManager.AddPTPConfig(types.ConfigName(cfgName), ptp4lCfg)

	eventManager.PtpConfigMapUpdates.UpdatePTPProcessOptions()
	eventManager.PtpConfigMapUpdates.UpdatePTPThreshold()
	eventManager.PtpConfigMapUpdates.UpdatePTPSetting()
	t.Logf("TBCProfiles after UpdatePTPSetting: %v", eventManager.PtpConfigMapUpdates.TBCProfiles)
	t.Logf("Stored ProfileType (never refreshed): %v",
		eventManager.GetPTPConfig(types.ConfigName(cfgName)).ProfileType)

	// Step 1: ptp4l master offset — establishes master stats + masterOffsetSource
	ptp4lMasterLocked := "ptp4l[5196819.100]: [ptp4l.0.config] master offset -5 s2 freq +500 path delay 89"
	t.Logf("--- STEP 1: ptp4l master offset (s2) ---")
	t.Logf("Input: %s", ptp4lMasterLocked)
	eventManager.ExtractMetrics(ptp4lMasterLocked)
	ptpStats := eventManager.GetStats(types.ConfigName(cfgName))
	if mStat, ok := ptpStats[metrics.MasterClockType]; ok {
		t.Logf("master stats: LastSyncState=%v, LastOffset=%d, ProcessName=%s",
			mStat.LastSyncState(), mStat.LastOffset(), mStat.ProcessName())
	} else {
		t.Logf("master stats: NOT CREATED")
	}
	if cStat, ok := ptpStats[metrics.ClockRealTime]; ok {
		t.Logf("CLOCK_REALTIME stats: LastSyncState=%v, LastOffset=%d",
			cStat.LastSyncState(), cStat.LastOffset())
	} else {
		t.Logf("CLOCK_REALTIME stats: NOT YET CREATED")
	}
	t.Logf("Mock events after step 1: %v", eventManager.GetMockEvent())

	// Step 2: phc2sys CLOCK_REALTIME s2 — establishes OS clock stats in LOCKED state
	phc2sysLocked := "phc2sys[3263.065]: [ptp4l.0.config] CLOCK_REALTIME phc offset 3 s2 freq -20217 delay 536"
	t.Logf("--- STEP 2: phc2sys CLOCK_REALTIME (s2) ---")
	t.Logf("Input: %s", phc2sysLocked)
	eventManager.ExtractMetrics(phc2sysLocked)
	if cStat, ok := ptpStats[metrics.ClockRealTime]; ok {
		t.Logf("CLOCK_REALTIME stats: LastSyncState=%v, LastOffset=%d, ProcessName=%s",
			cStat.LastSyncState(), cStat.LastOffset(), cStat.ProcessName())
	} else {
		t.Logf("CLOCK_REALTIME stats: STILL NOT CREATED (parsing failed?)")
	}
	t.Logf("Mock events after step 2: %v", eventManager.GetMockEvent())

	eventManager.ResetMockEvent()
	t.Logf("--- STEP 3: SLAVE to FAULTY ---")

	// Step 3: SLAVE to FAULTY — triggers the bug
	ptp4lFaulty := "ptp4l[3263.061]: [ptp4l.0.config:5] port 1 (ens3f2): SLAVE to FAULTY on FAULT_DETECTED (FT_UNSPECIFIED)"
	t.Logf("Input: %s", ptp4lFaulty)
	t.Logf("ProfileType at time of parsing: %v (should be TBC but is NONE due to race)",
		eventManager.GetPTPConfig(types.ConfigName(cfgName)).ProfileType)
	eventManager.ExtractMetrics(ptp4lFaulty)

	if mStat, ok := ptpStats[metrics.MasterClockType]; ok {
		t.Logf("master stats after FAULTY: LastSyncState=%v, LastOffset=%d",
			mStat.LastSyncState(), mStat.LastOffset())
	}
	if cStat, ok := ptpStats[metrics.ClockRealTime]; ok {
		t.Logf("CLOCK_REALTIME stats after FAULTY: LastSyncState=%v, LastOffset=%d",
			cStat.LastSyncState(), cStat.LastOffset())
	}

	mockEvents := eventManager.GetMockEvent()
	t.Logf("Mock events after FAULTY: %v", mockEvents)

	hasOsClockFreerun := false
	for _, evt := range mockEvents {
		if evt == ptp.OsClockSyncStateChange {
			hasOsClockFreerun = true
			break
		}
	}
	t.Logf("--- RESULT ---")
	t.Logf("Spurious OsClockSyncStateChange emitted: %v", hasOsClockFreerun)

	assert.True(t, hasOsClockFreerun,
		"BUG REPRODUCED: spurious OsClockSyncStateChange FREERUN event emitted on FAULTY "+
			"because the TBC profile was not detected due to startup ordering")
}

// TestTBCOffsetMetricUpdatedEveryLog tests that T-BC offset metric is updated on every log
func TestTBCOffsetMetricUpdatedEveryLog(t *testing.T) {
	eventManager := metrics.NewPTPEventManager("", initPubSubTypes(), "testnode", &common.SCConfiguration{StorePath: "/tmp/store"})
	eventManager.MockTest(true)

	configName = "ptp4l.0.config"
	eventManager.Stats[types.ConfigName(configName)] = make(stats.PTPStats)
	ptpStats := eventManager.GetStats(types.ConfigName(configName))

	replacer := strings.NewReplacer("[", " ", "]", " ", ":", " ")

	tests := []struct {
		name           string
		logLine        string
		expectedOffset int64
		expectedState  ptp.SyncState
	}{
		{
			name:           "T-BC s0 with offset 123",
			logLine:        "T-BC[1743005894]:[ptp4l.0.config] ens2f0 offset 123 T-BC-STATUS s0",
			expectedOffset: 123,
			expectedState:  ptp.FREERUN,
		},
		{
			name:           "T-BC s2 with offset 55",
			logLine:        "T-BC[1743005894]:[ptp4l.0.config] ens2f0 offset 55 T-BC-STATUS s2",
			expectedOffset: 55,
			expectedState:  ptp.LOCKED,
		},
		{
			name:           "T-BC s2 with offset 0 (still locked)",
			logLine:        "T-BC[1743005894]:[ptp4l.0.config] ens2f0 offset 0 T-BC-STATUS s2",
			expectedOffset: 0,
			expectedState:  ptp.LOCKED,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := replacer.Replace(tt.logLine)
			fields := strings.Fields(output)

			eventManager.ParseTBCLogs("T-BC", configName, output, fields, ptpStats)

			// Verify offset was stored
			lastOffset := ptpStats[types.IFace(stats.TBCMainClockName)].LastOffset()
			assert.Equal(t, tt.expectedOffset, lastOffset, "T-BC offset should be updated")

			// Verify state
			lastState, err := ptpStats[types.IFace(stats.TBCMainClockName)].GetStateState("T-BC", pointer.String("ens2f0"))
			assert.NoError(t, err)
			assert.Equal(t, tt.expectedState, lastState, "T-BC state should match log")
		})
	}
}
