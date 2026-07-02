package metrics_test

import (
	"fmt"
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
			logLine:       fmt.Sprintf("ptp4l[5196819.100]: [%s] master offset -100 s0 freq +1000 path delay 1000", configName),
			expectedState: ptp.FREERUN,
		},
		{
			name:          "TBC ptp4l with s2 should be LOCKED",
			logLine:       fmt.Sprintf("ptp4l[5196819.100]: [%s] master offset -50 s2 freq +500 path delay 1000", configName),
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

// TestTBCFaultyPortSpuriousFreerun verifies that when a ptp4l port goes
// SLAVE to FAULTY on a TBC profile that wasn't detected as TBC (due to a
// startup race), no spurious OsClockSyncStateChange FREERUN event is emitted.
// This was a known bug when maybePublishOSClockSyncStateChangeEvent existed;
// now that E3 is published exclusively from the phc2sys path, the bug is fixed.
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

	// Step 3: SLAVE to FAULTY — would have triggered the bug before the fix
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

	assert.False(t, hasOsClockFreerun,
		"No spurious OsClockSyncStateChange should be emitted: E3 is published "+
			"exclusively from the phc2sys path, not from the holdover timer")
}

// TestE3IncorporatesE1State verifies that E3 (CLOCK_REALTIME) incorporates
// E1 (PTP Lock State / upstream traceability) per O-RAN O-Cloud API v04.00
// Table 37. ParseTBCLogs does NOT publish E3 directly; only the phc2sys path
// publishes E3, applying worst_of(phc2sys_state, E1_state).
func TestE3IncorporatesE1State(t *testing.T) {
	eventManager := metrics.NewPTPEventManager("", initPubSubTypes(), "testnode", &common.SCConfiguration{StorePath: "/tmp/store"})
	eventManager.MockTest(true)

	configName = "ptp4l.0.config"
	tbcProfile := "tbc-test-profile"

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

	// Step 1: T-BC-STATUS s2 (LOCKED) — establish TBC state
	replacer := strings.NewReplacer("[", " ", "]", " ", ":", " ")
	tbcLockedLog := fmt.Sprintf("T-BC[1743005894]:[%s] ens2f0 offset 5 T-BC-STATUS s2", configName)
	output := replacer.Replace(tbcLockedLog)
	fields := strings.Fields(output)
	eventManager.ParseTBCLogs("T-BC", configName, output, fields, ptpStats)

	// Step 2: phc2sys CLOCK_REALTIME s2 (LOCKED) — establish OS clock state
	phc2sysLocked := fmt.Sprintf("phc2sys[3263.065]: [%s] CLOCK_REALTIME phc offset 3 s2 freq -20217 delay 536", configName)
	eventManager.ExtractMetrics(phc2sysLocked)

	cStat, ok := ptpStats[metrics.ClockRealTime]
	assert.True(t, ok, "CLOCK_REALTIME stats should exist after phc2sys line")
	assert.Equal(t, ptp.LOCKED, cStat.LastSyncState(), "CLOCK_REALTIME should be LOCKED from phc2sys")

	// Step 3: T-BC-STATUS s0 (FREERUN) — upstream lost, but phc2sys still locked
	eventManager.ResetMockEvent()
	tbcFreerunLog := fmt.Sprintf("T-BC[1743005900]:[%s] ens2f0 offset 123 T-BC-STATUS s0", configName)
	output = replacer.Replace(tbcFreerunLog)
	fields = strings.Fields(output)
	eventManager.ParseTBCLogs("T-BC", configName, output, fields, ptpStats)

	// Verify T-BC master resource is FREERUN (the T-BC event itself should fire)
	tbcKey := types.IFace(stats.TBCMainClockName)
	assert.Equal(t, ptp.FREERUN, ptpStats[tbcKey].LastSyncState(),
		"T-BC master resource should be FREERUN")

	// Verify CLOCK_REALTIME was NOT overridden — it must stay LOCKED
	assert.Equal(t, ptp.LOCKED, cStat.LastSyncState(),
		"CLOCK_REALTIME must remain LOCKED; ParseTBCLogs should not override phc2sys")

	// Verify no OsClockSyncStateChange was emitted
	assert.NotContains(t, eventManager.GetMockEvent(), ptp.OsClockSyncStateChange,
		"ParseTBCLogs must not emit OsClockSyncStateChange when T-BC goes FREERUN")

	// Step 4: Next phc2sys sample — E1 is FREERUN, phc2sys reports s2 with
	// small offset. Per O-RAN Table 37 row 3, E3 = worst_of(LOCKED, FREERUN) = FREERUN.
	eventManager.ResetMockEvent()
	phc2sysAfterE1Freerun := fmt.Sprintf(
		"phc2sys[3264.065]: [%s] CLOCK_REALTIME phc offset 3 s2 freq -20217 delay 536",
		configName,
	)
	eventManager.ExtractMetrics(phc2sysAfterE1Freerun)

	assert.Equal(t, ptp.FREERUN, cStat.LastSyncState(),
		"CLOCK_REALTIME must be FREERUN because E1 is FREERUN (O-RAN Table 37 row 3)")
	assert.Contains(t, eventManager.GetMockEvent(), ptp.OsClockSyncStateChange,
		"phc2sys path must emit OsClockSyncStateChange when E3 transitions to FREERUN")
}

// TestE3DerivationORANTable37 is a table-driven test covering all 5 rows
// of O-RAN O-Cloud Notification API v04.00 Table 37 for E3 (CLOCK_REALTIME).
//
// | Row | E1 (PTP Lock State) | phc2sys offset | phc2sys servo | Expected E3         |
// |-----|---------------------|----------------|---------------|---------------------|
// |  1  | LOCKED              | within range   | s2            | LOCKED              |
// |  2  | HOLDOVER            | within range   | s2            | HOLDOVER            |
// |  3  | FREERUN             | within range   | s2            | FREERUN             |
// |  4  | LOCKED              | outside range  | s2            | FREERUN             |
// |  5  | LOCKED              | n/a (phc2sys)  | s0            | FREERUN             |
func TestE3DerivationORANTable37(t *testing.T) {
	tests := []struct {
		name        string
		e1State     ptp.SyncState
		phc2sys     string // phc2sys log line suffix: "offset <N> <servo> freq <F> delay <D>"
		expectedE3  ptp.SyncState
		expectEvent bool
	}{
		{
			name:        "Row1: E1=LOCKED offset_in_range s2 → E3=LOCKED",
			e1State:     ptp.LOCKED,
			phc2sys:     "CLOCK_REALTIME phc offset 3 s2 freq -20217 delay 536",
			expectedE3:  ptp.LOCKED,
			expectEvent: true, // FREERUN→LOCKED transition
		},
		{
			name:        "Row2: E1=HOLDOVER offset_in_range s2 → E3=HOLDOVER",
			e1State:     ptp.HOLDOVER,
			phc2sys:     "CLOCK_REALTIME phc offset 5 s2 freq -20217 delay 536",
			expectedE3:  ptp.HOLDOVER,
			expectEvent: true, // LOCKED→HOLDOVER transition
		},
		{
			name:        "Row3: E1=FREERUN offset_in_range s2 → E3=FREERUN",
			e1State:     ptp.FREERUN,
			phc2sys:     "CLOCK_REALTIME phc offset 3 s2 freq -20217 delay 536",
			expectedE3:  ptp.FREERUN,
			expectEvent: true, // HOLDOVER→FREERUN transition
		},
		{
			name:        "Row4: E1=LOCKED offset_out_of_range s2 → E3=FREERUN",
			e1State:     ptp.LOCKED,
			phc2sys:     "CLOCK_REALTIME phc offset 999999 s2 freq -20217 delay 536",
			expectedE3:  ptp.FREERUN,
			expectEvent: false, // FREERUN→FREERUN, no event
		},
		{
			name:        "Row5: E1=LOCKED servo=s0 → E3=FREERUN",
			e1State:     ptp.LOCKED,
			phc2sys:     "CLOCK_REALTIME phc offset 3 s0 freq -20217 delay 536",
			expectedE3:  ptp.FREERUN,
			expectEvent: false, // FREERUN→FREERUN, no event
		},
	}

	cfgName := "ptp4l.0.config"
	tbcProfile := "tbc-e3-test"

	eventManager := metrics.NewPTPEventManager("", initPubSubTypes(), "testnode", &common.SCConfiguration{StorePath: "/tmp/store"})
	eventManager.MockTest(true)

	eventManager.PtpConfigMapUpdates.TBCProfiles = []string{tbcProfile}

	ptp4lCfg := &ptp4lconf.PTP4lConfig{
		Name:        cfgName,
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
	eventManager.AddPTPConfig(types.ConfigName(cfgName), ptp4lCfg)

	ptpStats := eventManager.GetStats(types.ConfigName(cfgName))
	ptpStats[metrics.MasterClockType] = &stats.Stats{}
	ptpStats[metrics.MasterClockType].SetAlias("ens2f0")
	ptpStats[metrics.MasterClockType].SetRole(types.SLAVE)

	// Initialize T-BC key and CLOCK_REALTIME via a T-BC LOCKED + phc2sys LOCKED baseline.
	// This ensures GetMainClockName() finds the T-BC key.
	replacer := strings.NewReplacer("[", " ", "]", " ", ":", " ")
	tbcInit := fmt.Sprintf("T-BC[1743005894]:[%s] ens2f0 offset 5 T-BC-STATUS s2", cfgName)
	eventManager.ParseTBCLogs("T-BC", cfgName, replacer.Replace(tbcInit), strings.Fields(replacer.Replace(tbcInit)), ptpStats)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set E1 state on the T-BC main clock key
			tbcKey := types.IFace(stats.TBCMainClockName)
			ptpStats[tbcKey].SetLastSyncState(tt.e1State)

			eventManager.ResetMockEvent()
			phc2sysLog := fmt.Sprintf("phc2sys[3263.065]: [%s] %s", cfgName, tt.phc2sys)
			eventManager.ExtractMetrics(phc2sysLog)

			cStat := ptpStats[metrics.ClockRealTime]
			assert.Equal(t, tt.expectedE3, cStat.LastSyncState(),
				"E3 (CLOCK_REALTIME) state mismatch")

			mockEvents := eventManager.GetMockEvent()
			if tt.expectEvent {
				assert.Contains(t, mockEvents, ptp.OsClockSyncStateChange,
					"expected OsClockSyncStateChange event")
			}
		})
	}
}

// TestE1CheckFiresRegardlessOfMasterOffsetSource verifies that the E1-aware E3
// derivation works even when masterOffsetSource is "ts2phc". In T-BC profiles,
// both ptp4l and ts2phc write to ptpStats["master"].ProcessName, causing
// masterOffsetSource to toggle. The E1 check must fire regardless of this value.
func TestE1CheckFiresRegardlessOfMasterOffsetSource(t *testing.T) {
	eventManager := metrics.NewPTPEventManager("", initPubSubTypes(), "testnode", &common.SCConfiguration{StorePath: "/tmp/store"})
	eventManager.MockTest(true)

	cfgName := "ptp4l.0.config"
	tbcProfile := "tbc-race-test"

	eventManager.PtpConfigMapUpdates.TBCProfiles = []string{tbcProfile}

	ptp4lCfg := &ptp4lconf.PTP4lConfig{
		Name:        cfgName,
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
	eventManager.AddPTPConfig(types.ConfigName(cfgName), ptp4lCfg)

	ptpStats := eventManager.GetStats(types.ConfigName(cfgName))
	ptpStats[metrics.MasterClockType] = &stats.Stats{}
	ptpStats[metrics.MasterClockType].SetAlias("ens2f0")
	ptpStats[metrics.MasterClockType].SetRole(types.SLAVE)

	replacer := strings.NewReplacer("[", " ", "]", " ", ":", " ")
	tbcInit := fmt.Sprintf("T-BC[1743005894]:[%s] ens2f0 offset 5 T-BC-STATUS s2", cfgName)
	eventManager.ParseTBCLogs("T-BC", cfgName, replacer.Replace(tbcInit), strings.Fields(replacer.Replace(tbcInit)), ptpStats)

	// Baseline: phc2sys LOCKED
	phc2sysBaseline := fmt.Sprintf("phc2sys[3263.065]: [%s] CLOCK_REALTIME phc offset 3 s2 freq -20217 delay 536", cfgName)
	eventManager.ExtractMetrics(phc2sysBaseline)

	// Set T-BC to HOLDOVER (upstream lost)
	tbcKey := types.IFace(stats.TBCMainClockName)
	ptpStats[tbcKey].SetLastSyncState(ptp.HOLDOVER)

	// Simulate the race: force masterOffsetSource to "ts2phc" (as ts2phc would)
	metrics.SetMasterOffsetSource("ts2phc")

	eventManager.ResetMockEvent()
	phc2sysAfter := fmt.Sprintf("phc2sys[3264.065]: [%s] CLOCK_REALTIME phc offset 5 s2 freq -20217 delay 536", cfgName)
	eventManager.ExtractMetrics(phc2sysAfter)

	cStat := ptpStats[metrics.ClockRealTime]
	assert.Equal(t, ptp.HOLDOVER, cStat.LastSyncState(),
		"E3 must be HOLDOVER even when masterOffsetSource is ts2phc — "+
			"the E1 check must not be gated by masterOffsetSource")
}

// TestTBCOffsetMetricUpdatedEveryLog tests that T-BC offset metric is updated on every log
func TestTBCOffsetMetricUpdatedEveryLog(t *testing.T) {
	eventManager := metrics.NewPTPEventManager("", initPubSubTypes(), "testnode", &common.SCConfiguration{StorePath: "/tmp/store"})
	eventManager.MockTest(true)

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
			logLine:        fmt.Sprintf("T-BC[1743005894]:[%s] ens2f0 offset 123 T-BC-STATUS s0", configName),
			expectedOffset: 123,
			expectedState:  ptp.FREERUN,
		},
		{
			name:           "T-BC s2 with offset 55",
			logLine:        fmt.Sprintf("T-BC[1743005894]:[%s] ens2f0 offset 55 T-BC-STATUS s2", configName),
			expectedOffset: 55,
			expectedState:  ptp.LOCKED,
		},
		{
			name:           "T-BC s2 with offset 0 (still locked)",
			logLine:        fmt.Sprintf("T-BC[1743005894]:[%s] ens2f0 offset 0 T-BC-STATUS s2", configName),
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

// TestTBCProcessDownEventFiresFreerun verifies that killing ptp4l on a T-BC
// profile sets the T-BC stats key to FREERUN and publishes a ptp-state-change
// event, and that subsequent T-BC-STATUS s2 publishes a LOCKED event.
// Regression test for OCPBUGS-85330.
func TestTBCProcessDownEventFiresFreerun(t *testing.T) {
	eventManager := metrics.NewPTPEventManager("", initPubSubTypes(), "testnode", &common.SCConfiguration{StorePath: "/tmp/store"})
	eventManager.MockTest(true)

	configName = "ptp4l.1.config"
	tbcProfile := "tbc-tr"

	eventManager.PtpConfigMapUpdates.TBCProfiles = []string{tbcProfile}
	eventManager.PtpConfigMapUpdates.PtpProcessOpts = make(map[string]*ptpConfig.PtpProcessOpts)

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
	ptpStats[metrics.MasterClockType].SetAlias("enox")
	tbcKey := types.IFace(stats.TBCMainClockName)
	ptpStats[tbcKey] = &stats.Stats{}
	ptpStats[tbcKey].SetAlias("enox")
	ptpStats[tbcKey].SetLastSyncState(ptp.LOCKED)

	// Step 1: Simulate ptp4l process down (PTP_PROCESS_STATUS:0)
	downLog := "ptp4l[1780430740]:[ptp4l.1.config] PTP_PROCESS_STATUS:0"
	eventManager.ResetMockEvent()
	eventManager.ExtractMetrics(downLog)

	// Verify T-BC stats key is now FREERUN
	assert.Equal(t, ptp.FREERUN, ptpStats[tbcKey].LastSyncState(),
		"T-BC stats should be FREERUN after ptp4l down")

	// Verify a PtpStateChange event was published
	mockEvents := eventManager.GetMockEvent()
	assert.Contains(t, mockEvents, ptp.PtpStateChange,
		"PtpStateChange event should fire on ptp4l down for T-BC")

	// Step 2: Simulate T-BC-STATUS s2 arriving (DPLL still locked)
	replacer := strings.NewReplacer("[", " ", "]", " ", ":", " ")
	tbcLog := "T-BC[1780430800]:[ts2phc.1.config] ens2f0 offset 0 T-BC-STATUS s2"
	output := replacer.Replace(tbcLog)
	fields := strings.Fields(output)

	eventManager.ResetMockEvent()
	eventManager.ParseTBCLogs("T-BC", configName, output, fields, ptpStats)

	// Verify T-BC stats key transitions back to LOCKED
	assert.Equal(t, ptp.LOCKED, ptpStats[tbcKey].LastSyncState(),
		"T-BC stats should be LOCKED after T-BC-STATUS s2")
}
