package ptp

import (
	"context"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	testutils "github.com/redhat-cne/cloud-event-proxy/test/utils"
	testclient "github.com/redhat-cne/cloud-event-proxy/test/utils/client"
	"github.com/redhat-cne/cloud-event-proxy/test/utils/operator"
	"github.com/redhat-cne/cloud-event-proxy/test/utils/pods"
	"github.com/redhat-cne/cloud-event-proxy/test/utils/prom"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// var isSingleNode bool
const (
	OrdinaryClock        = "OC"
	DualNicBoundaryClock = "DUAL_NIC_BC"
	BoundaryClock        = "BC"
	// openshift_ptp_interface_role 0 = PASSIVE, 1 = SLAVE, 2 = MASTER, 3 = FAULTY, 4 =  UNKNOWN
	RoleUnknown int64 = 4
	RoleFaulty  int64 = 3
	RoleMaster  int64 = 2
	RoleSlave   int64 = 1
	RolePassive int64 = 0
)

var _ = ginkgo.Describe("PTP validation", ginkgo.Ordered, func() {
	ptpRunningPods := []corev1.Pod{}
	ptpPorts := []operator.PortInterface{}

	ginkgo.BeforeEach(func() {
		gomega.Expect(testclient.Client).NotTo(gomega.BeNil())
	})
	ginkgo.Context("Initialize", ginkgo.Label(OrdinaryClock, BoundaryClock, DualNicBoundaryClock), func() {
		ginkgo.It("Should check for ptp deployment  ", func() {
			deploy, err := testclient.Client.Deployments(testutils.NamespaceForPTP).Get(context.Background(), testutils.DeploymentForPTP, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(deploy.Status.Replicas).To(gomega.Equal(deploy.Status.ReadyReplicas))
		})
		ginkgo.It("Should check for ptp pods  ", func() {
			ptpPods, err := testclient.Client.Pods(testutils.NamespaceForPTP).List(context.Background(), metav1.ListOptions{LabelSelector: "app=linuxptp-daemon"})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			ptpRunningPods = append(ptpRunningPods, ptpPods.Items...)
			gomega.Expect(len(ptpRunningPods)).To(gomega.Equal(1))
			gomega.Expect(ptpRunningPods[0].Status.Phase).To(gomega.Equal(corev1.PodRunning))
		})

	})
	ginkgo.Context("when PTP Operator is deployed", func() {
		ginkgo.It("Should check for ptp events container ", func() {
			ginkgo.By("Checking event side car is present")
			gomega.Expect(len(ptpRunningPods)).To(gomega.BeNumerically(">", 0), "linuxptp-daemon is not deployed on cluster")
			gomega.Expect(len(ptpRunningPods[0].Spec.Containers)).To(gomega.BeNumerically("==", 3),
				"linuxptp-daemon is not deployed on cluster with cloud event proxy")
			cloudProxyFound := false
			for _, pod := range ptpRunningPods {
				for _, c := range pod.Spec.Containers {
					if c.Name == testutils.EventProxyContainerName {
						cloudProxyFound = true
					}
				}
			}
			gomega.Expect(cloudProxyFound).ToNot(gomega.BeFalse(), "No event pods detected")
		})
	})
	ginkgo.Context("When PTP events are enabled", func() {
		ginkgo.It("Should check for API health ", func() {
			ginkgo.By("probing health endpoint", func() {
				gomega.Eventually(func() string {
					buf, _ := pods.ExecCommand(testclient.Client, ptpRunningPods[0], testutils.EventProxyContainerName,
						[]string{"curl", "127.0.0.1:9085/api/cloudNotifications/v1/health"})
					return buf.String()
				}, 5*time.Second, 5*time.Second).Should(gomega.ContainSubstring("OK"),
					"Event API is not in healthy state")
			})
			ginkgo.By("checking  publisher creation", func() {
				gomega.Eventually(func() string {
					buf, _ := pods.ExecCommand(testclient.Client, ptpRunningPods[0], testutils.EventProxyContainerName,
						[]string{"curl", "127.0.0.1:9085/api/cloudNotifications/v1/publishers"})
					return buf.String()
				}, 5*time.Second, 5*time.Second).Should(gomega.ContainSubstring("endpointUri"),
					"Event API  did not return publishers")
			})
		})

	})

	ginkgo.Context("when PTP is configured", func() {
		ptpConfigs, err := operator.GetPTPConfig(testutils.NamespaceForPTP, testclient.Client)
		gomega.Expect(err).To(gomega.BeNil())
		ginkgo.It("should have only one PTPConfig", ginkgo.Label(OrdinaryClock), func() {
			gomega.Expect(len(ptpConfigs)).To(gomega.Equal(1))
		})
		ginkgo.It("should have only one interface", ginkgo.Label(OrdinaryClock), func() {
			gomega.Expect(ptpConfigs[0].Spec.Profile[0].Interface).NotTo(gomega.BeNil())
			ptpPorts = append(ptpPorts, operator.PortInterface{
				Name:      ptpConfigs[0].Spec.Profile[0].Name,
				Interface: ptpConfigs[0].Spec.Profile[0].Interface,
			})
		})

		ginkgo.It("should have only one PTPConfig", ginkgo.Label(BoundaryClock), func() {
			gomega.Expect(len(ptpConfigs)).To(gomega.Equal(1))
			ptpPorts = append(ptpPorts, operator.GetMasterSlaveInterfaces(ptpConfigs[0].Spec.Profile)...)
			// should have one slave and multiple master
			gomega.Expect(len(ptpPorts)).To(gomega.Equal(1), "One or more port configuration not found ")
			gomega.Expect(len(ptpPorts[0].MasterInterface)).To(gomega.BeNumerically(">", 0), "One or more master port configuration not found ")
			gomega.Expect(len(ptpPorts[0].SlaveInterface)).To(gomega.Equal(1), "No slave port  is configured")

		})
		ginkgo.It("should have Two PTPConfig", ginkgo.Label(DualNicBoundaryClock), func() {
			gomega.Expect(len(ptpConfigs)).To(gomega.Equal(2))
			ptpPorts = append(ptpPorts, operator.GetMasterSlaveInterfaces(ptpConfigs[0].Spec.Profile)...)
			// should have one slave and multiple master
			gomega.Expect(len(ptpPorts)).To(gomega.Equal(2), "One or more port configuration not found ")
			gomega.Expect(len(ptpPorts[0].MasterInterface)).To(gomega.BeNumerically(">", 0), "One or more master port configuration not found ")
			gomega.Expect(len(ptpPorts[0].SlaveInterface)).To(gomega.Equal(1), "No slave port  is configured for  Nic 1")
			gomega.Expect(len(ptpPorts[1].SlaveInterface)).To(gomega.Equal(1), "No slave port  is configured for  Nic 2")
		})

	})

	// Check event amd metrics when SLAVE is at FAULT
	// Check event and metrics when master is at  FAULT

	// Run until  Slave comes back from FAULT to SlAVE

	// RUN  until  Master comes back from FAULT to MASTER

	// Check OC  events and metrics in normal conditions
	// offset should be below 50ns, one slave port + no of offset below 50ns should be less than n
	//
	ginkgo.Context("Check interface role", func() {
		ginkgo.It("should not have non configured interface role", func() {
			var ifaces []string
			for _, ports := range ptpPorts {
				if ports.Interface != nil {
					ifaces = append(ifaces, *ports.Interface)
				}
				for _, master := range ports.MasterInterface {
					if master != nil {
						ifaces = append(ifaces, *master)
					}
				}
				for _, slave := range ports.SlaveInterface {
					if slave != nil {
						ifaces = append(ifaces, *slave)
					}
				}
			}

			gomega.Eventually(func() error {
				buf, _ := pods.ExecCommand(testclient.Client, ptpRunningPods[0], testutils.EventProxyContainerName, []string{"curl", "127.0.0.1:9091/metrics"})
				return prom.HasInvalidInterfaceRole(ifaces, buf)
			}, 30*time.Second, 5*time.Second).Should(gomega.BeNil(),
				"slave interface role metrics are not detected")
		})
		ginkgo.It("should have interface in SLAVE role", ginkgo.Label(OrdinaryClock), func() {
			gomega.Eventually(func() (int64, error) {
				buf, _ := pods.ExecCommand(testclient.Client, ptpRunningPods[0], testutils.EventProxyContainerName, []string{"curl", "127.0.0.1:9091/metrics"})
				return prom.InterfaceRole(ptpPorts[0].Interface, buf)
			}, 30*time.Second, 5*time.Second).Should(gomega.Equal(RoleSlave),
				"slave interface role metrics are not detected")
		})
		ginkgo.It("should have interface in SLAVE role", ginkgo.Label("BC"), func() {
			gomega.Eventually(func() (int64, error) {
				buf, _ := pods.ExecCommand(testclient.Client, ptpRunningPods[0], testutils.EventProxyContainerName, []string{"curl", "127.0.0.1:9091/metrics"})
				return prom.InterfaceRole(ptpPorts[0].SlaveInterface[0], buf)
			}, 30*time.Second, 5*time.Second).Should(gomega.Equal(RoleSlave),
				"slave interface role metrics are not detected")
		})
		ginkgo.It("should have interface in master role", ginkgo.Label(BoundaryClock, DualNicBoundaryClock), func() {
			for _, master := range ptpPorts {
				for _, m := range master.MasterInterface {
					gomega.Eventually(func() (int64, error) {
						buf, _ := pods.ExecCommand(testclient.Client, ptpRunningPods[0], testutils.EventProxyContainerName, []string{"curl", "127.0.0.1:9091/metrics"})
						return prom.InterfaceRole(m, buf)
					}, 30*time.Second, 5*time.Second).Should(gomega.Equal(RoleMaster),
						"master interface role metrics are not detected")
				}
			}
		})
	})

	ginkgo.Context("PTP sync state", func() {
		// TODO no considering dual slave in this scenario

		ginkgo.It("master offset should have in LOCKED state", ginkgo.Pending, ginkgo.Label(OrdinaryClock), func() {
			// OcSlaveInetrface := ptpPorts[0].SlaveInterface

		})
		ginkgo.It("master offset should have in LOCKED state", ginkgo.Pending, ginkgo.Label(BoundaryClock), func() {
			// OcSlaveInetrface := ptpPorts[0].SlaveInterface[0]

		})

		ginkgo.It("master offset should have in LOCKED state", ginkgo.Pending, ginkgo.Label(DualNicBoundaryClock), func() {

		})

		ginkgo.It("CLOCK_REALTIME should be in LOCKED state", ginkgo.Label(OrdinaryClock, BoundaryClock), func() {

		})
		ginkgo.It("CLOCK_REALTIME should be in LOCKED state", ginkgo.Label(DualNicBoundaryClock), func() {

		})

	})

	ginkgo.Context("OS clock sync state", ginkgo.Pending, func() {

	})
	ginkgo.Context("CLOCK_REALTIME  locked state is > 1", ginkgo.Pending, func() {

	})
	ginkgo.Context("CLOCK_REALTIME  FREERUN  state is =0 ", ginkgo.Pending, func() {

	})

	ginkgo.Context("master port LOCKED state > 1 ", ginkgo.Pending, func() {

	})
	ginkgo.Context("master port HOLDOVER state > 1 ", ginkgo.Pending, func() {

	})
	ginkgo.Context("master port FREERUN state = 0 ", ginkgo.Pending, func() {

	})

})
