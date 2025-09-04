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
	}

	defer utils.Aliases.Clear()

	for _, tc := range testCases {
		utils.Aliases.SetAlias(tc.ifname, tc.expectedAlias)
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("%s->%s", tc.ifname, tc.expectedAlias), func(t *testing.T) {
			assert.Equal(t, tc.expectedAlias, utils.GetAlias(tc.ifname))
		})
	}
}
