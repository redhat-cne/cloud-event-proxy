package ptp4lconf_test

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/ptp4lconf"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

const (
	dirToWatch = "../_testFiles/"
	ptp4l0Conf = "ptp4l.0.config"
)

var (
	initialText = fmt.Sprintf("%d", time.Now().UnixNano())
)

func SetUp() error {
	cleanUp()
	f, err := os.OpenFile(filepath.Join(dirToWatch, ptp4l0Conf), os.O_CREATE|os.O_WRONLY, os.FileMode(0o644))
	if err != nil {
		log.Errorf("create file error %s", err)
		return err
	}
	defer func() {
		if fErr := f.Close(); fErr != nil {
			log.Printf("Error closing file: %s\n", err)
		}
	}()
	_, err = f.Write([]byte(initialText))
	if err != nil {
		return err
	}
	return nil
}
func cleanUp() {
	err := os.Remove(filepath.Join(dirToWatch, ptp4l0Conf))
	if err != nil {
		log.Println(err)
		return
	}
}
func Test_Config(t *testing.T) {
	var err error
	defer cleanUp()
	err = SetUp()
	assert.Nil(t, err)

	notifyConfigUpdates := make(chan *ptp4lconf.PtpConfigUpdate)
	w, err := ptp4lconf.NewPtp4lConfigWatcher(dirToWatch, notifyConfigUpdates)
	assert.Nil(t, err)
	select {
	case ptpConfigEvent := <-notifyConfigUpdates:
		// assert
		assert.Equal(t, ptp4l0Conf, *ptpConfigEvent.Name)
		assert.Equal(t, initialText, *ptpConfigEvent.Ptp4lConf)
	case <-time.After(1 * time.Second):
		log.Infof("timeout...")
	}

	// Update config
	newText := fmt.Sprintf("%d", time.Now().UnixNano())
	log.Infof("writing to %s", filepath.Join(dirToWatch, ptp4l0Conf))
	err = os.WriteFile(filepath.Join(dirToWatch, ptp4l0Conf), []byte(newText), os.FileMode(0o600))
	assert.Nil(t, err)
	log.Info("waiting...")
	// WriteFile creates two events it might be an issue with test only
	select {
	case <-notifyConfigUpdates:
		// assert
	case <-time.After(1 * time.Second):
		log.Infof("timeout...")
	}

	select {
	case ptpConfigEvent := <-notifyConfigUpdates:
		assert.Equal(t, ptp4l0Conf, *ptpConfigEvent.Name)
		assert.Equal(t, newText, *ptpConfigEvent.Ptp4lConf)
	case <-time.After(1 * time.Second):
		log.Infof("timeout...")
	}

	cleanUp()
	select {
	case ptpConfigEvent := <-notifyConfigUpdates:
		// assert
		log.Println(ptpConfigEvent.String())
		assert.Nil(t, err)
		assert.Equal(t, ptp4l0Conf, *ptpConfigEvent.Name)
		assert.True(t, ptpConfigEvent.Removed)
	case <-time.After(1 * time.Second):
		log.Infof("timeout...")
	}
	w.Close()
}

func Test_ProfileName(t *testing.T) {
	var testResult string

	testCases := []struct {
		configString   string
		expectedString string
	}{
		{
			configString:   "  recommend:\n  - profile: ordinary\n    priority: 0\n",
			expectedString: "ordinary",
		},
		{
			configString:   "  recommend:\n  - profile: ordinary-clock\n    priority: 0\n",
			expectedString: "ordinary-clock",
		},
		{
			configString:   "  recommend:\n  - profile: ordinary_clock\n    priority: 0\n",
			expectedString: "ordinary_clock",
		},
		{
			configString:   "  recommend:\n    priority: 0\n",
			expectedString: "",
		},
	}

	for _, tc := range testCases {
		testResult = ptp4lconf.GetPTPProfileName(tc.configString)
		assert.Equal(t, tc.expectedString, testResult)
	}
}

func TestPtpConfigUpdate_GetAllSections(t *testing.T) {
	testConfig := `
[ens3f0]
masterOnly 0
[ens3f1]
masterOnly 0
[global]
#
# Default Data Set
#
slaveOnly 1
twoStepFlag 1
priority1 128
priority2 128
domainNumber 24
#utc_offset 37
clockClass 248
clockAccuracy 0xFE
offsetScaledLogVariance 0xFFFF
free_running 0
freq_est_interval 1
dscp_event 0
dscp_general 0
dataset_comparison ieee1588
G.8275.defaultDS.localPriority 128
#
# Port Data Set
#
logAnnounceInterval -3
logSyncInterval -4
logMinDelayReqInterval -4
logMinPdelayReqInterval -4
announceReceiptTimeout 3
syncReceiptTimeout 0
delayAsymmetry 0
fault_reset_interval -4
neighborPropDelayThresh 20000000
masterOnly 0
G.8275.portDS.localPriority 128
#
# Run time options
#
assume_two_step 0
logging_level 6
path_trace_enabled 0
follow_up_info 0
hybrid_e2e 0
inhibit_multicast_service 0
net_sync_monitor 0
tc_spanning_tree 0
tx_timestamp_timeout 50
unicast_listen 0
unicast_master_table 0
unicast_req_duration 3600
use_syslog 1
verbose 0
summary_interval -4
kernel_leap 1
check_fup_sync 0
#
# Servo Options
#
pi_proportional_const 0.0
pi_integral_const 0.0
pi_proportional_scale 0.0
pi_proportional_exponent -0.3
pi_proportional_norm_max 0.7
pi_integral_scale 0.0
pi_integral_exponent 0.4
pi_integral_norm_max 0.3
step_threshold 0.0
first_step_threshold 0.00002
max_frequency 900000000
clock_servo pi
sanity_freq_limit 200000000
ntpshm_segment 0
#
# Transport options
#
transportSpecific 0x0
ptp_dst_mac 01:1B:19:00:00:00
p2p_dst_mac 01:80:C2:00:00:0E
udp_ttl 1
udp6_scope 0x0E
uds_address /var/run/ptp4l
#
# Default interface options
#
clock_type OC
network_transport L2
delay_mechanism E2E
time_stamping hardware
tsproc_mode filter
delay_filter moving_median
delay_filter_length 10
egressLatency 0
ingressLatency 0
boundary_clock_jbod 1
#
# Clock description
#
productDescription ;;
revisionData ;;
manufacturerIdentity 00:00:00
userDescription ;
timeSource 0xA0
ptp4lOpts: -2  --summary_interval -4
ptpClockThreshold:
holdOverTimeout: 65
maxOffsetThreshold: 3000
minOffsetThreshold: -3000
ptpSchedulingPolicy: SCHED_FIFO
ptpSchedulingPriority: 10
`

	type fields struct {
		Name      *string
		Ptp4lConf *string
		Profile   *string
		Removed   bool
	}
	tests := []struct {
		name   string
		fields fields
		want   map[string]map[string]string
	}{
		{
			name: "ok",
			fields: fields{
				Ptp4lConf: &testConfig,
			},
			want: map[string]map[string]string{
				"ens3f0": {
					"masterOnly": "0",
				},
				"ens3f1": {"masterOnly": "0"},
				"global": {
					"G.8275.defaultDS.localPriority": "128",
					"G.8275.portDS.localPriority":    "128",
					"announceReceiptTimeout":         "3",
					"assume_two_step":                "0",
					"boundary_clock_jbod":            "1",
					"check_fup_sync":                 "0",
					"clockAccuracy":                  "0xFE",
					"clockClass":                     "248",
					"clock_servo":                    "pi",
					"clock_type":                     "OC",
					"dataset_comparison":             "ieee1588",
					"delayAsymmetry":                 "0",
					"delay_filter":                   "moving_median",
					"delay_filter_length":            "10",
					"delay_mechanism":                "E2E",
					"domainNumber":                   "24",
					"dscp_event":                     "0",
					"dscp_general":                   "0",
					"egressLatency":                  "0",
					"fault_reset_interval":           "-4",
					"first_step_threshold":           "0.00002",
					"follow_up_info":                 "0",
					"free_running":                   "0",
					"freq_est_interval":              "1",
					"holdOverTimeout:":               "65",
					"hybrid_e2e":                     "0",
					"ingressLatency":                 "0",
					"inhibit_multicast_service":      "0",
					"kernel_leap":                    "1",
					"logAnnounceInterval":            "-3",
					"logMinDelayReqInterval":         "-4",
					"logMinPdelayReqInterval":        "-4",
					"logSyncInterval":                "-4",
					"logging_level":                  "6",
					"manufacturerIdentity":           "00:00:00",
					"masterOnly":                     "0",
					"maxOffsetThreshold:":            "3000",
					"max_frequency":                  "900000000",
					"minOffsetThreshold:":            "-3000",
					"neighborPropDelayThresh":        "20000000",
					"net_sync_monitor":               "0",
					"network_transport":              "L2",
					"ntpshm_segment":                 "0",
					"offsetScaledLogVariance":        "0xFFFF",
					"p2p_dst_mac":                    "01:80:C2:00:00:0E",
					"path_trace_enabled":             "0",
					"pi_integral_const":              "0.0",
					"pi_integral_exponent":           "0.4",
					"pi_integral_norm_max":           "0.3",
					"pi_integral_scale":              "0.0",
					"pi_proportional_const":          "0.0",
					"pi_proportional_exponent":       "-0.3",
					"pi_proportional_norm_max":       "0.7",
					"pi_proportional_scale":          "0.0",
					"priority1":                      "128",
					"priority2":                      "128",
					"productDescription":             ";;",
					"ptp4lOpts:":                     "-2  --summary_interval -4",
					"ptpSchedulingPolicy:":           "SCHED_FIFO",
					"ptpSchedulingPriority:":         "10",
					"ptp_dst_mac":                    "01:1B:19:00:00:00",
					"revisionData":                   ";;",
					"sanity_freq_limit":              "200000000",
					"slaveOnly":                      "1",
					"step_threshold":                 "0.0",
					"summary_interval":               "-4",
					"syncReceiptTimeout":             "0",
					"tc_spanning_tree":               "0",
					"timeSource":                     "0xA0",
					"time_stamping":                  "hardware",
					"transportSpecific":              "0x0",
					"tsproc_mode":                    "filter",
					"twoStepFlag":                    "1",
					"tx_timestamp_timeout":           "50",
					"udp6_scope":                     "0x0E",
					"udp_ttl":                        "1",
					"uds_address":                    "/var/run/ptp4l",
					"unicast_listen":                 "0",
					"unicast_master_table":           "0",
					"unicast_req_duration":           "3600",
					"use_syslog":                     "1",
					"userDescription":                ";",
					"verbose":                        "0",
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &ptp4lconf.PtpConfigUpdate{
				Name:      tt.fields.Name,
				Ptp4lConf: tt.fields.Ptp4lConf,
				Profile:   tt.fields.Profile,
				Removed:   tt.fields.Removed,
			}
			if got := p.GetAllSections(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("PtpConfigUpdate.GetAllSections() = %v, want %v", got, tt.want)
			}
		})
	}
}
