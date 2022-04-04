package operator

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	v1 "github.com/openshift/ptp-operator/api/v1"
	"github.com/redhat-cne/cloud-event-proxy/test/utils/client"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Strptr return prt of string
func Strptr(s string) *string {
	// copy variable
	c := s
	return &c
}

// PortInterface ... ptp interface object
type PortInterface struct {
	Name            *string
	Interface       *string
	MasterInterface []*string
	SlaveInterface  []*string
}

// GetPTPConfig ... getPTPConfig
func GetPTPConfig(namespace string, testClient *client.Set) ([]v1.PtpConfig, error) {
	var ptpConfig []v1.PtpConfig
	configList, err := testClient.PtpV1Interface.PtpConfigs(namespace).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return ptpConfig, err
	}
	ptpConfig = append(ptpConfig, configList.Items...)
	return ptpConfig, err
}

// GetMasterSlaveInterfaces Todo: this works only if masterOnly or SlaveOnly is right under the section
func GetMasterSlaveInterfaces(profile []v1.PtpProfile) []PortInterface {
	var ptpPorts []PortInterface

	for _, s := range profile {
		sectionRegEx := regexp.MustCompile(`\[(.*?)\]`)
		sections := sectionRegEx.FindAllStringSubmatch(*s.Ptp4lConf, -1)
		p := PortInterface{}
		p.Name = Strptr(*s.Name)
		for _, section := range sections {
			if section[1] == "global" {
				continue
			}
			sectionValue := fmt.Sprintf(`\[%s\][\r\n]+([^\r\n]+)`, section[1])
			sectionValueRegEx := regexp.MustCompile(sectionValue)
			portType := sectionValueRegEx.FindStringSubmatch(*s.Ptp4lConf)
			if strings.Contains(portType[1], "masterOnly 1") {
				p.MasterInterface = append(p.MasterInterface, Strptr(section[1]))
			} else {
				p.SlaveInterface = append(p.SlaveInterface, Strptr(section[1]))
			}
		}
		ptpPorts = append(ptpPorts, p)
	}

	return ptpPorts
}

// ParsePTPInterfaceRole return value from metrics
func ParsePTPInterfaceRole(s, port string) string {
	// # HELP openshift_ptp_interface_role 0 = PASSIVE, 1 = SLAVE, 2 = MASTER, 3 = FAULTY, 4 =  UNKNOWN
	// # TYPE openshift_ptp_interface_role gauge
	// openshift_ptp_interface_role{iface="ens5f0",node="cnfde7.ptp.lab.eng.bos.redhat.com",process="ptp4l"} 2

	return "SLAVE"
}
