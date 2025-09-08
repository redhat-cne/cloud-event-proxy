package utils_test

import (
	"fmt"
	"testing"

	"github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/utils"
	"github.com/stretchr/testify/assert"
)

type testCase struct {
	ifname        string
	expectedAlias string
}

func Test_GetAlias(t *testing.T) {
	testCases := []testCase{
		{"eth0", "ethx"},
		{"eth1.100", "ethx.100"},
		{"eth1.100.XYZ", "ethx.100.XYZ"},
		// Mellanox style naming
		{"enP2s2f0np0", "enP2s2fx"},
		{"enP1s1f1np1", "enP1s1fx"},
		{"enP10s5f3np2", "enP10s5fx"},
		// Mellanox style naming with VLAN
		{"enP2s2f0np0.100", "enP2s2fx.100"},
		{"enP1s1f1np1.200", "enP1s1fx.200"},
		{"enP10s5f3np2.300.XYZ", "enP10s5fx.300.XYZ"},
	}
	for _, tc := range testCases {
		t.Run(fmt.Sprintf("%s->%s", tc.ifname, tc.expectedAlias), func(t *testing.T) {
			assert.Equal(t, tc.expectedAlias, utils.GetAlias(tc.ifname))
		})
	}
}
