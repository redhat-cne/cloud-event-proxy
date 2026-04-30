package main

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/redhat-cne/cloud-event-proxy/pkg/common"
	ptpConfig "github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/config"
	"github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/filesystem"
	ptpMetrics "github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/metrics"
	"github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/ptp4lconf"
	"github.com/redhat-cne/sdk-go/pkg/event/ptp"
	"github.com/stretchr/testify/assert"
	"k8s.io/utils/pointer"
)

func TestTBCFixedStartupNoSpuriousFreerun(t *testing.T) {
	mockFS, cleanup := filesystem.SetupMockFS()
	defer cleanup()

	profileName := "tsc-holdover"
	cfgFileName := "ptp4l.0.config"
	testNodeName := "test-node"
	profilePath := "/etc/linuxptp"

	ptp4lConfData := "# profile: " + profileName + "\n[global]\nslaveOnly 0\nboundary_clock_jbod 1\n[ens3f2]\nserverOnly 0\n"

	profiles := []ptpConfig.PtpProfile{
		{
			Name:        pointer.String(profileName),
			Interface:   pointer.String("ens3f2"),
			Ptp4lOpts:   pointer.String("-2 -s --summary_interval -4"),
			Phc2sysOpts: pointer.String("-r -n 24 -N 8 -R 16 -u 0 -m -s ens3f2"),
			TS2PhcOpts:  pointer.String("-s generic -a --ts2phc.rh_external_pps 1"),
			Ptp4lConf:   pointer.String(ptp4lConfData),
			PtpSettings: map[string]string{},
			PtpClockThreshold: &ptpConfig.PtpClockThreshold{
				HoldOverTimeout:    5,
				MaxOffsetThreshold: 100,
				MinOffsetThreshold: -100,
			},
		},
	}
	profileJSON, err := json.Marshal(profiles)
	assert.NoError(t, err)

	// Mock the configmap profile file read by WatchConfigMapUpdate -> updatePtpConfig
	configMapPath := profilePath + "/" + testNodeName
	mockFS.AllowStat(configMapPath, nil, nil)
	mockFS.AllowReadFile(configMapPath, profileJSON, nil)

	// Mock the ptp4l config file listing and read by loadInitialPtp4lConfigs -> ReadAllConfig -> readConfig
	mockFS.AllowReadDir(ptpConfigDir, []os.DirEntry{
		filesystem.MockDirEntry{EntryName: cfgFileName, Dir: false},
	}, nil)
	mockFS.AllowReadFile("/var/run/"+cfgFileName, []byte(ptp4lConfData), nil)

	// Set up environment for WatchConfigMapUpdate to use our profile path
	originalPath := os.Getenv("PTP_PROFILE_PATH")
	os.Setenv("PTP_PROFILE_PATH", profilePath)
	defer os.Setenv("PTP_PROFILE_PATH", originalPath)

	// Initialize eventManager the same way Start() does
	publishers = InitPubSubTypes()
	scConfig := &common.SCConfiguration{
		StorePath: t.TempDir(),
		CloseCh:   make(chan struct{}),
	}
	config = scConfig
	eventManager = ptpMetrics.NewPTPEventManager(resourcePrefix, publishers, testNodeName, scConfig)
	eventManager.MockTest(true)
	ptpMetrics.RegisterMetrics(testNodeName)

	// Phase 1: startPtpConfigSync — real code path: reads mocked configmap, populates TBCProfiles
	startPtpConfigSync(2*time.Second, testNodeName, scConfig)

	assert.Contains(t, eventManager.PtpConfigMapUpdates.TBCProfiles, profileName,
		"TBCProfiles should contain the profile after configmap sync")

	// Phase 2: loadInitialPtp4lConfigs — real code path: reads mocked config files,
	// calls processConfigCreate which calls GetProfileType, should return TBC
	loadInitialPtp4lConfigs()

	storedCfg := eventManager.GetPTPConfig("ptp4l.0.config")
	assert.Equal(t, ptp4lconf.TBC, storedCfg.ProfileType,
		"ProfileType should be TBC because configmap was loaded before config registration")

	// Phase 3: simulate buffered socket log lines — real code path via ExtractMetrics
	bufferedLogs := []string{
		"ptp4l[5196819.100]: [ptp4l.0.config] master offset -5 s2 freq +500 path delay 89",
		"phc2sys[3263.065]: [ptp4l.0.config] CLOCK_REALTIME phc offset 3 s2 freq -20217 delay 536",
	}
	for _, line := range bufferedLogs {
		eventManager.ExtractMetrics(line)
	}

	eventManager.ResetMockEvent()

	// The critical event: SLAVE to FAULTY
	ptp4lFaulty := "ptp4l[3263.061]: [ptp4l.0.config:5] port 1 (ens3f2): SLAVE to FAULTY on FAULT_DETECTED (FT_UNSPECIFIED)"
	eventManager.ExtractMetrics(ptp4lFaulty)

	mockEvents := eventManager.GetMockEvent()
	for _, evt := range mockEvents {
		assert.NotEqual(t, ptp.OsClockSyncStateChange, evt,
			"No spurious OsClockSyncStateChange should be emitted because ProfileType=TBC was set correctly before log processing")
	}
}
