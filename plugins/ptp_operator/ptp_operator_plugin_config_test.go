//go:build unittests
// +build unittests

package main

import (
	"testing"

	"github.com/redhat-cne/cloud-event-proxy/pkg/common"
	ptpConfig "github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/config"
	ptpMetrics "github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/metrics"
	"github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/ptp4lconf"
	ptpTypes "github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/types"
	"github.com/stretchr/testify/assert"
	"k8s.io/utils/pointer"
)

func setupProcessConfigTest(t *testing.T) {
	t.Helper()
	publishers = InitPubSubTypes()
	sc := &common.SCConfiguration{
		StorePath: t.TempDir(),
		CloseCh:   make(chan struct{}),
	}
	config = sc
	eventManager = ptpMetrics.NewPTPEventManager(resourcePrefix, publishers, "test-node", sc)
	eventManager.MockTest(true)
	eventManager.PtpConfigMapUpdates = &ptpConfig.LinuxPTPConfigMapUpdate{
		EventThreshold: map[string]*ptpConfig.PtpClockThreshold{},
	}
	ptpMetrics.RegisterMetrics("test-node")
}

func TestProcessConfigCreate_InterfaceRoles(t *testing.T) {
	tests := []struct {
		name           string
		ptp4lConf      string
		profile        string
		expectedIfaces []struct {
			name string
			role ptpTypes.PtpPortRole
		}
	}{
		{
			name:    "serverOnly=1 assigns MASTER",
			profile: "bc-profile",
			ptp4lConf: "[global]\nboundary_clock_jbod 1\n" +
				"[ens1f0]\nserverOnly 1\n" +
				"[ens2f0]\nserverOnly 0\n",
			expectedIfaces: []struct {
				name string
				role ptpTypes.PtpPortRole
			}{
				{"ens1f0", ptpTypes.MASTER},
				{"ens2f0", ptpTypes.SLAVE},
			},
		},
		{
			name:    "masterOnly fallback when serverOnly absent",
			profile: "bc-legacy",
			ptp4lConf: "[global]\nboundary_clock_jbod 1\n" +
				"[ens1f0]\nmasterOnly 1\n" +
				"[ens2f0]\nmasterOnly 0\n",
			expectedIfaces: []struct {
				name string
				role ptpTypes.PtpPortRole
			}{
				{"ens1f0", ptpTypes.MASTER},
				{"ens2f0", ptpTypes.SLAVE},
			},
		},
		{
			name:    "serverOnly takes precedence over masterOnly",
			profile: "bc-mixed",
			ptp4lConf: "[global]\nboundary_clock_jbod 1\n" +
				"[ens1f0]\nserverOnly 0\nmasterOnly 1\n",
			expectedIfaces: []struct {
				name string
				role ptpTypes.PtpPortRole
			}{
				{"ens1f0", ptpTypes.SLAVE},
			},
		},
		{
			name:    "global clientOnly=1 fallback",
			profile: "oc-client",
			ptp4lConf: "[global]\nclientOnly 1\n" +
				"[ens1f0]\n",
			expectedIfaces: []struct {
				name string
				role ptpTypes.PtpPortRole
			}{
				{"ens1f0", ptpTypes.SLAVE},
			},
		},
		{
			name:    "global slaveOnly=1 fallback",
			profile: "oc-slave",
			ptp4lConf: "[global]\nslaveOnly 1\n" +
				"[ens1f0]\n",
			expectedIfaces: []struct {
				name string
				role ptpTypes.PtpPortRole
			}{
				{"ens1f0", ptpTypes.SLAVE},
			},
		},
		{
			name:    "no role parameters defaults to SLAVE",
			profile: "oc-default",
			ptp4lConf: "[global]\n" +
				"[ens1f0]\n",
			expectedIfaces: []struct {
				name string
				role ptpTypes.PtpPortRole
			}{
				{"ens1f0", ptpTypes.SLAVE},
			},
		},
		{
			name:    "no global section defaults to SLAVE",
			profile: "oc-minimal",
			ptp4lConf: "[ens1f0]\n",
			expectedIfaces: []struct {
				name string
				role ptpTypes.PtpPortRole
			}{
				{"ens1f0", ptpTypes.SLAVE},
			},
		},
		{
			name:    "interface not in sections gets UNKNOWN",
			profile: "partial",
			ptp4lConf: "[global]\nboundary_clock_jbod 1\n" +
				"[ens1f0]\nserverOnly 1\n",
			expectedIfaces: []struct {
				name string
				role ptpTypes.PtpPortRole
			}{
				{"ens1f0", ptpTypes.MASTER},
			},
		},
		{
			name:    "multiple interfaces with mixed roles",
			profile: "tbc-profile",
			ptp4lConf: "[global]\nboundary_clock_jbod 1\nslaveOnly 0\n" +
				"[ens1f0]\nserverOnly 0\n" +
				"[ens2f0]\nserverOnly 1\n" +
				"[ens3f0]\nserverOnly 1\n",
			expectedIfaces: []struct {
				name string
				role ptpTypes.PtpPortRole
			}{
				{"ens1f0", ptpTypes.SLAVE},
				{"ens2f0", ptpTypes.MASTER},
				{"ens3f0", ptpTypes.MASTER},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupProcessConfigTest(t)

			cfgName := "ptp4l.0.config"
			processConfigCreate(&ptp4lconf.PtpConfigUpdate{
				Name:      pointer.String(cfgName),
				Profile:   pointer.String(tt.profile),
				Ptp4lConf: pointer.String(tt.ptp4lConf),
			})

			storedCfg := eventManager.GetPTPConfig(ptpTypes.ConfigName(cfgName))
			if !assert.NotNil(t, storedCfg) {
				return
			}
			if !assert.Equal(t, len(tt.expectedIfaces), len(storedCfg.Interfaces),
				"interface count mismatch") {
				return
			}

			for i, expected := range tt.expectedIfaces {
				assert.Equal(t, expected.name, storedCfg.Interfaces[i].Name,
					"interface name at index %d", i)
				assert.Equal(t, expected.role, storedCfg.Interfaces[i].Role,
					"interface role at index %d (%s)", i, expected.name)
				assert.Equal(t, i+1, storedCfg.Interfaces[i].PortID,
					"port ID at index %d", i)
			}
		})
	}
}

func TestProcessConfigCreate_NilInputs(t *testing.T) {
	setupProcessConfigTest(t)

	processConfigCreate(nil)
	processConfigCreate(&ptp4lconf.PtpConfigUpdate{Name: nil, Profile: pointer.String("p")})
	processConfigCreate(&ptp4lconf.PtpConfigUpdate{Name: pointer.String("n"), Profile: nil})
}
