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

	configName = "ptp4l.0.config"
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
