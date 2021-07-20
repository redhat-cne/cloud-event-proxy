package config_test

import (
	"os"
	"testing"

	ptpConfig "github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/config"
	"github.com/stretchr/testify/assert"
)

func Test_Config(t *testing.T) {

	testCases := map[string]struct {
		wantProfile []*ptpConfig.PtpClockThreshold
		profilePath string
		nodeName    string
		len         int
	}{
		"single": {
			wantProfile: []*ptpConfig.PtpClockThreshold{{
				HoldOverTimeout:    30,
				MaxOffsetThreshold: 100,
				MinOffsetThreshold: -100,
			}},
			profilePath: "../_testprofile",
			nodeName:    "single",
			len:         1,
		},
		"mixed": {
			wantProfile: []*ptpConfig.PtpClockThreshold{
				{
					HoldOverTimeout:    10,
					MaxOffsetThreshold: 50,
					MinOffsetThreshold: -50,
				}, {

					HoldOverTimeout:    30,
					MaxOffsetThreshold: 100,
					MinOffsetThreshold: -100,
				},
			},
			profilePath: "../_testprofile",
			nodeName:    "mixed",
			len:         2,
		},
		"none": {
			wantProfile: []*ptpConfig.PtpClockThreshold{},
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
			go ptpUpdate.WatchConfigUpdate(tc.nodeName, closeCh)
			<-ptpUpdate.UpdateCh
			ptpUpdate.UpdatePTPThreshold()
			assert.Equal(t, tc.len, len(ptpUpdate.NodeProfiles))
			if tc.nodeName == "none" {
				assert.Equal(t, []ptpConfig.PtpProfile{}, ptpUpdate.NodeProfiles)
				assert.Equal(t, []ptpConfig.PtpProfile{}, ptpUpdate.NodeProfiles)
			} else {
				for i, p := range ptpUpdate.NodeProfiles {
					assert.Equal(t, tc.wantProfile[i], p.PtpClockThreshold)
					assert.Equal(t, p.PtpClockThreshold, ptpUpdate.EventThreshold[*p.Interface])
				}
			}
		})
	}
	closeCh <- struct{}{}

}
