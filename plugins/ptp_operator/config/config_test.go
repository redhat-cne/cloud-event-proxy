package config_test

import (
	"os"
	"testing"

	ptpConfig "github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/config"
	"github.com/stretchr/testify/assert"
	apiextensions "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
)

const ntpFailoverPlugin = "ntpfailover"

var (
	profile0  = "profile0"
	profile1  = "profile1"
	profileHa = "profileHA"
	inface0   = "ens5f0"
	inface1   = "ens5f1"
)

// StrPtr returns a pointer to a string value. This is useful within expressions where the value is a literal.
func StrPtr(s string) *string {
	return &s
}

func Test_Config(t *testing.T) {
	testCases := map[string]struct {
		wantProfile []*ptpConfig.PtpProfile
		profilePath string
		nodeName    string
		len         int
	}{
		"section": {
			wantProfile: []*ptpConfig.PtpProfile{{
				Name:      &profile0,
				Interface: &inface1,
				PtpClockThreshold: &ptpConfig.PtpClockThreshold{
					HoldOverTimeout:    5,
					MaxOffsetThreshold: 3000,
					MinOffsetThreshold: -3000,
					Close:              make(chan struct{}),
				},
			}},
			profilePath: "../_testprofile",
			nodeName:    "section",
			len:         1,
		},
		"single": {
			wantProfile: []*ptpConfig.PtpProfile{{
				Name:      &profile0,
				Interface: &inface1,
				PtpClockThreshold: &ptpConfig.PtpClockThreshold{
					HoldOverTimeout:    30,
					MaxOffsetThreshold: 100,
					MinOffsetThreshold: -100,
					Close:              make(chan struct{}),
				},
			}},
			profilePath: "../_testprofile",
			nodeName:    "single",
			len:         1,
		},
		"mixed": {
			wantProfile: []*ptpConfig.PtpProfile{{
				Name:      &profile0,
				Interface: &inface0,
				PtpClockThreshold: &ptpConfig.PtpClockThreshold{
					HoldOverTimeout:    10,
					MaxOffsetThreshold: 50,
					MinOffsetThreshold: -50,
					Close:              make(chan struct{}),
				},
			}, {
				Name:      &profile1,
				Interface: &inface1,
				PtpClockThreshold: &ptpConfig.PtpClockThreshold{
					HoldOverTimeout:    30,
					MaxOffsetThreshold: 100,
					MinOffsetThreshold: -100,
					Close:              make(chan struct{}),
				},
			}},
			profilePath: "../_testprofile",
			nodeName:    "mixed",
			len:         2,
		},
		"optionalPhcOpts": {
			wantProfile: []*ptpConfig.PtpProfile{{
				Name:        &profile0,
				Interface:   &inface0,
				Ptp4lOpts:   StrPtr("-2 -s --summary_interval -4"),
				Phc2sysOpts: nil,
				PtpClockThreshold: &ptpConfig.PtpClockThreshold{
					HoldOverTimeout:    30,
					MaxOffsetThreshold: 100,
					MinOffsetThreshold: -100,
					Close:              make(chan struct{}),
				},
			}},
			profilePath: "../_testprofile",
			nodeName:    "optionalPhcOpts",
			len:         1,
		},
		"none": {
			wantProfile: []*ptpConfig.PtpProfile{},
			profilePath: "../_testprofile",
			nodeName:    "none",
			len:         0,
		},
		"ptpha": {
			wantProfile: []*ptpConfig.PtpProfile{{
				Name:        &profileHa,
				Interface:   nil,
				Ptp4lOpts:   nil,
				Phc2sysOpts: StrPtr("-a -r -m -n 24 -N 8 -R 16"),
				PtpClockThreshold: &ptpConfig.PtpClockThreshold{
					HoldOverTimeout:    30,
					MaxOffsetThreshold: 100,
					MinOffsetThreshold: -100,
					Close:              make(chan struct{}),
				},
			}},
			profilePath: "../_testprofile",
			nodeName:    "ptpha",
			len:         1,
		},
		ntpFailoverPlugin: {
			wantProfile: []*ptpConfig.PtpProfile{{
				Name:      &profile0,
				Interface: &inface1,
				PtpClockThreshold: &ptpConfig.PtpClockThreshold{
					HoldOverTimeout:    5,
					MaxOffsetThreshold: 1000,
					MinOffsetThreshold: -1000,
					Close:              make(chan struct{}),
				},
			}},
			profilePath: "../_testprofile",
			nodeName:    "ntpfailover",
			len:         1,
		},
	}

	closeCh := make(chan struct{})
	_ = os.Setenv("PTP_PROFILE_PATH", "../_testprofile")
	_ = os.Setenv("CONFIG_UPDATE_INTERVAL", "1")
	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			ptpUpdate := ptpConfig.NewLinuxPTPConfUpdate()
			go ptpUpdate.WatchConfigMapUpdate(tc.nodeName, closeCh, true)
			<-ptpUpdate.UpdateCh
			ptpUpdate.UpdatePTPThreshold()
			assert.Equal(t, tc.len, len(ptpUpdate.NodeProfiles))
			if tc.nodeName == "section" {
				for i, p := range ptpUpdate.NodeProfiles {
					tc.wantProfile[i].PtpClockThreshold.Close = p.PtpClockThreshold.Close
					assert.Equal(t, tc.wantProfile[i].PtpClockThreshold, p.PtpClockThreshold)
					tc.wantProfile[i].PtpClockThreshold.Close = ptpUpdate.EventThreshold[*p.Name].Close
					assert.Equal(t, tc.wantProfile[i].PtpClockThreshold, ptpUpdate.EventThreshold[*p.Name])
					assert.Equal(t, *tc.wantProfile[i].Name, *p.Name)
				}
			} else if tc.nodeName == "none" {
				assert.Equal(t, []ptpConfig.PtpProfile{}, ptpUpdate.NodeProfiles)
				assert.Equal(t, []ptpConfig.PtpProfile{}, ptpUpdate.NodeProfiles)
			} else if tc.nodeName == "haPTP" {
				for i, p := range ptpUpdate.NodeProfiles {
					tc.wantProfile[i].PtpClockThreshold.Close = p.PtpClockThreshold.Close
					assert.Equal(t, tc.wantProfile[i].PtpClockThreshold, p.PtpClockThreshold)
					tc.wantProfile[i].PtpClockThreshold.Close = ptpUpdate.EventThreshold[*p.Name].Close
					assert.Equal(t, tc.wantProfile[i].PtpClockThreshold, ptpUpdate.EventThreshold[*p.Name])
					assert.Equal(t, *tc.wantProfile[i].Name, *p.Name)
					assert.Equal(t, tc.wantProfile[i].PtpSettings[ptpConfig.HaProfileIdentifier], p.PtpSettings[ptpConfig.HaProfileIdentifier])
				}
			} else {
				for i, p := range ptpUpdate.NodeProfiles {
					if p.PtpClockThreshold != nil {
						tc.wantProfile[i].PtpClockThreshold.Close = p.PtpClockThreshold.Close
						assert.Equal(t, tc.wantProfile[i].PtpClockThreshold, p.PtpClockThreshold)
					}
					tc.wantProfile[i].PtpClockThreshold.Close = ptpUpdate.EventThreshold[*p.Name].Close
					assert.Equal(t, tc.wantProfile[i].PtpClockThreshold, ptpUpdate.EventThreshold[*p.Name])
					assert.Equal(t, *tc.wantProfile[i].Name, *p.Name)
				}
			}
		})
	}
	closeCh <- struct{}{}
}

func TestUpdatePTPProcessOptions_PopulatesChronydOpts(t *testing.T) {
	chronydOpts := "-f /etc/chrony.conf"
	phc2sysOpts := "-a -r -r -n 24"
	ptp4lOpts := "-2 -s"
	ts2phcOpts := "-m"
	profileName := "ntp-failover"

	l := &ptpConfig.LinuxPTPConfigMapUpdate{
		NodeProfiles: []ptpConfig.PtpProfile{
			{
				Name:        &profileName,
				Ptp4lOpts:   &ptp4lOpts,
				Phc2sysOpts: &phc2sysOpts,
				TS2PhcOpts:  &ts2phcOpts,
				ChronydOpts: &chronydOpts,
			},
		},
		PtpProcessOpts: make(map[string]*ptpConfig.PtpProcessOpts),
		PtpSettings:    make(map[string]map[string]string),
	}

	l.UpdatePTPProcessOptions()

	opts, ok := l.PtpProcessOpts[profileName]
	assert.True(t, ok, "profile must be present in PtpProcessOpts")
	assert.True(t, opts.ChronydEnabled(), "ChronydEnabled() must return true when profile has chronydOpts")
	assert.Equal(t, chronydOpts, *opts.ChronydOpts)
	assert.True(t, opts.Ptp4lEnabled())
	assert.True(t, opts.Phc2SysEnabled())
	assert.True(t, opts.TS2PhcEnabled())
}

func TestUpdatePTPProcessOptions_NilChronydOpts(t *testing.T) {
	phc2sysOpts := "-a -r -r -n 24"
	ptp4lOpts := "-2 -s"
	profileName := "ptp-oc"

	l := &ptpConfig.LinuxPTPConfigMapUpdate{
		NodeProfiles: []ptpConfig.PtpProfile{
			{
				Name:        &profileName,
				Ptp4lOpts:   &ptp4lOpts,
				Phc2sysOpts: &phc2sysOpts,
				ChronydOpts: nil,
			},
		},
		PtpProcessOpts: make(map[string]*ptpConfig.PtpProcessOpts),
		PtpSettings:    make(map[string]map[string]string),
	}

	l.UpdatePTPProcessOptions()

	opts, ok := l.PtpProcessOpts[profileName]
	assert.True(t, ok, "profile must be present in PtpProcessOpts")
	assert.False(t, opts.ChronydEnabled(), "ChronydEnabled() must return false when profile has no chronydOpts")
	assert.True(t, opts.Ptp4lEnabled())
	assert.True(t, opts.Phc2SysEnabled())
}

func TestUpdatePTPThreshold_NtpFailover(t *testing.T) {
	profileName := "test-profile"

	tests := []struct {
		name        string
		profile     ptpConfig.PtpProfile
		expectedMax int64
		expectedMin int64
	}{
		{
			name: "default threshold without ntpfailover",
			profile: ptpConfig.PtpProfile{
				Name: &profileName,
			},
			expectedMax: 100,
			expectedMin: -100,
		},
		{
			name: "ntpfailover with gnssFailover enabled uses looser threshold",
			profile: ptpConfig.PtpProfile{
				Name: &profileName,
				Plugins: map[string]*apiextensions.JSON{
					ntpFailoverPlugin: {Raw: []byte(`{"gnssFailover": true}`)},
				},
			},
			expectedMax: 1000,
			expectedMin: -1000,
		},
		{
			name: "ntpfailover with gnssFailover disabled uses standard threshold",
			profile: ptpConfig.PtpProfile{
				Name: &profileName,
				Plugins: map[string]*apiextensions.JSON{
					ntpFailoverPlugin: {Raw: []byte(`{"gnssFailover": false}`)},
				},
			},
			expectedMax: 100,
			expectedMin: -100,
		},
		{
			name: "explicit PtpClockThreshold takes precedence over ntpfailover",
			profile: ptpConfig.PtpProfile{
				Name: &profileName,
				PtpClockThreshold: &ptpConfig.PtpClockThreshold{
					HoldOverTimeout:    10,
					MaxOffsetThreshold: 500,
					MinOffsetThreshold: -500,
				},
				Plugins: map[string]*apiextensions.JSON{
					ntpFailoverPlugin: {Raw: []byte(`{"gnssFailover": true}`)},
				},
			},
			expectedMax: 500,
			expectedMin: -500,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := &ptpConfig.LinuxPTPConfigMapUpdate{
				NodeProfiles:   []ptpConfig.PtpProfile{tt.profile},
				EventThreshold: make(map[string]*ptpConfig.PtpClockThreshold),
			}
			l.UpdatePTPThreshold()
			th := l.EventThreshold[profileName]
			assert.NotNil(t, th)
			assert.Equal(t, tt.expectedMax, th.MaxOffsetThreshold)
			assert.Equal(t, tt.expectedMin, th.MinOffsetThreshold)
		})
	}
}

func TestUpdatePTPThreshold_OnThresholdUpdateCallback(t *testing.T) {
	profileName := "callback-profile"
	l := &ptpConfig.LinuxPTPConfigMapUpdate{
		NodeProfiles: []ptpConfig.PtpProfile{
			{
				Name: &profileName,
				PtpClockThreshold: &ptpConfig.PtpClockThreshold{
					HoldOverTimeout:    30,
					MaxOffsetThreshold: 200,
					MinOffsetThreshold: -200,
				},
			},
		},
		EventThreshold: make(map[string]*ptpConfig.PtpClockThreshold),
	}

	callbackCount := 0
	l.OnThresholdUpdate = func(thresholds map[string]*ptpConfig.PtpClockThreshold) {
		callbackCount++
		th := thresholds[profileName]
		assert.NotNil(t, th, "threshold for profile must exist in callback")
		assert.Equal(t, int64(200), th.MaxOffsetThreshold)
		assert.Equal(t, int64(-200), th.MinOffsetThreshold)
		assert.Equal(t, int64(30), th.HoldOverTimeout)
	}

	l.UpdatePTPThreshold()

	assert.Equal(t, 1, callbackCount, "OnThresholdUpdate callback must be invoked exactly once")
}

func TestUpdatePTPThreshold_NilCallbackDoesNotPanic(t *testing.T) {
	profileName := "nil-callback"
	l := &ptpConfig.LinuxPTPConfigMapUpdate{
		NodeProfiles: []ptpConfig.PtpProfile{
			{Name: &profileName},
		},
		EventThreshold: make(map[string]*ptpConfig.PtpClockThreshold),
	}
	assert.NotPanics(t, func() { l.UpdatePTPThreshold() })
}

func TestUpdatePTPProcessOptions_TS2PhcConfSetsDefaultOpts(t *testing.T) {
	ptp4lOpts := "-2 -s"
	profileName := "ts2phc-profile"
	ts2phcConf := "[global]\n"

	l := &ptpConfig.LinuxPTPConfigMapUpdate{
		NodeProfiles: []ptpConfig.PtpProfile{
			{
				Name:       &profileName,
				Ptp4lOpts:  &ptp4lOpts,
				TS2PhcConf: &ts2phcConf,
			},
		},
		PtpProcessOpts: make(map[string]*ptpConfig.PtpProcessOpts),
		PtpSettings:    make(map[string]map[string]string),
	}

	l.UpdatePTPProcessOptions()

	opts := l.PtpProcessOpts[profileName]
	assert.True(t, opts.TS2PhcEnabled(), "TS2PhcOpts should default to '-m' when TS2PhcConf is set but TS2PhcOpts is nil")
	assert.Equal(t, "-m", *opts.TS2PhcOpts)
}
