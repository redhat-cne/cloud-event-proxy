package config_test

import (
	"os"
	"testing"

	ptpConfig "github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/config"
	"github.com/stretchr/testify/assert"
)

var (
	profile0 string = "profile0"
	profile1 string = "profile1"
	inface0  string = "ens5f0"
	inface1  string = "ens5f1"
)

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
		"none": {
			wantProfile: []*ptpConfig.PtpProfile{},
			profilePath: "../_testprofile",
			nodeName:    "none",
			len:         0,
		},
	}

	closeCh := make(chan struct{})
	os.Setenv("PTP_PROFILE_PATH", "../_testprofile")
	os.Setenv("CONFIG_UPDATE_INTERVAL", "1")
	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			ptpUpdate := ptpConfig.NewLinuxPTPConfUpdate()
			go ptpUpdate.WatchConfigMapUpdate(tc.nodeName, closeCh)
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
			} else {
				for i, p := range ptpUpdate.NodeProfiles {
					tc.wantProfile[i].PtpClockThreshold.Close = p.PtpClockThreshold.Close
					assert.Equal(t, tc.wantProfile[i].PtpClockThreshold, p.PtpClockThreshold)
					tc.wantProfile[i].PtpClockThreshold.Close = ptpUpdate.EventThreshold[*p.Name].Close
					assert.Equal(t, tc.wantProfile[i].PtpClockThreshold, ptpUpdate.EventThreshold[*p.Name])
					assert.Equal(t, *tc.wantProfile[i].Name, *p.Name)
				}
			}
		})
	}
	closeCh <- struct{}{}

}
