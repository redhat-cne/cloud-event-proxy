package prom

import (
	"bytes"
	"fmt"
	"regexp"
	"strconv"
)

// InterfaceRole ... parses metrics and returns interface role
func InterfaceRole(iface *string, metrics bytes.Buffer) (int64, error) {
	regExStr := fmt.Sprintf(`openshift_ptp_interface_role\{iface="%s",node=\S*?,process="ptp4l"} ([0-4])`, *iface)
	roleRegEx, err := regexp.Compile(regExStr)
	if err != nil {
		return int64(0), err
	}
	match := roleRegEx.FindStringSubmatch(metrics.String())
	if match == nil || len(match) < 2 {
		return int64(0), fmt.Errorf("Unable to parse interface role")
	}
	return strconv.ParseInt(match[1], 10, 64)

}

// HasInvalidInterfaceRole ... checks if there extra interfaces that were not configured
func HasInvalidInterfaceRole(iface []string, metrics bytes.Buffer) error {
	roleRegEx := regexp.MustCompile(`openshift_ptp_interface_role\{iface="(\S*?)",node=\S*?,process="ptp4l"}`)

	match := roleRegEx.FindAllStringSubmatch(metrics.String(), -1)
	if match != nil || len(match) < 1 {
		return fmt.Errorf("interface role not found")
	}

	// O(n)
	sourceMap := map[string]bool{}
	for _, v := range iface {
		sourceMap[v] = true
	}
	// O(n)
	for _, v := range match {
		if _, ok := sourceMap[v[1]]; !ok {
			return fmt.Errorf("role for non configured port are found in metrics")

		}
	}
	return nil
}
