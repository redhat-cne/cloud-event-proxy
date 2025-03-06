//go:build !unittests
// +build !unittests

// Package cne ...
package cne

import (
	"context"
	"fmt"
	"time"

	"github.com/onsi/gomega/format"

	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"
	testutils "github.com/redhat-cne/cloud-event-proxy/test/utils"
	testclient "github.com/redhat-cne/cloud-event-proxy/test/utils/client"
	"github.com/redhat-cne/cloud-event-proxy/test/utils/pods"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = ginkgo.Describe("validation", func() {
	ginkgo.BeforeEach(func() {
		gomega.Expect(testclient.Client).NotTo(gomega.BeNil())
	})

	ginkgo.Context("cne", func() {
		ginkgo.It("should have the all test  namespaces", func() {
			_, err := testclient.Client.Namespaces().Get(context.Background(), testutils.NamespaceProducerTesting, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			_, err = testclient.Client.Namespaces().Get(context.Background(), testutils.NamespaceConsumerTesting, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
		})

		ginkgo.It("should have the event producer deployment in running state", func() {
			deploy, err := testclient.Client.Deployments(testutils.NamespaceProducerTesting).Get(context.Background(), testutils.CloudEventProducerDeploymentName, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(deploy.Status.Replicas).To(gomega.Equal(deploy.Status.ReadyReplicas))

			pods, err := testclient.Client.Pods(testutils.NamespaceProducerTesting).List(context.Background(), metav1.ListOptions{
				LabelSelector: "app=producer"})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			gomega.Expect(len(pods.Items)).To(gomega.Equal(1))
			gomega.Expect(pods.Items[0].Status.Phase).To(gomega.Equal(corev1.PodRunning))

		})
		ginkgo.It("should have the event consumer deployment in running state", func() {
			deploy, err := testclient.Client.Deployments(testutils.NamespaceConsumerTesting).Get(context.Background(), testutils.CloudEventConsumerDeploymentName, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(deploy.Status.Replicas).To(gomega.Equal(deploy.Status.ReadyReplicas))

			pods, err := testclient.Client.Pods(testutils.NamespaceConsumerTesting).List(context.Background(), metav1.ListOptions{
				LabelSelector: "app=consumer"})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			gomega.Expect(len(pods.Items)).To(gomega.BeNumerically(">", 0), "consumer is not deployed in the cluster")
			gomega.Expect(pods.Items[0].Status.Phase).To(gomega.Equal(corev1.PodRunning))
		})
	})

	ginkgo.Describe("[e2e]", func() {
		producerPod := corev1.Pod{}
		consumerPod := corev1.Pod{}

		ginkgo.BeforeEach(func() {
			producerPods, err := testclient.Client.Pods(testutils.NamespaceProducerTesting).List(context.Background(), metav1.ListOptions{
				LabelSelector: "app=producer"})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(len(producerPods.Items)).To(gomega.BeNumerically(">", 0), "producer is not deployed on cluster")
			producerPod = producerPods.Items[0]

			consumerPods, err := testclient.Client.Pods(testutils.NamespaceConsumerTesting).List(context.Background(), metav1.ListOptions{
				LabelSelector: "app=consumer"})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(len(consumerPods.Items)).To(gomega.BeNumerically(">", 0), "consumer is not deployed on cluster")
			consumerPod = consumerPods.Items[0]
		})

		ginkgo.Context("cloud event producer validation", func() {
			ginkgo.It("Should check for producer", func() {
				ginkgo.By("Checking container is present")
				gomega.Expect(len(producerPod.Spec.Containers)).To(gomega.BeNumerically("==", 1), "producer container not present")
			})

			ginkgo.It("Should check for producer metrics", func() {
				gomega.Eventually(func() string {
					buf, _ := pods.ExecCommand(testclient.Client, producerPod, testutils.EventProxyContainerName, []string{"curl", "127.0.0.1:9091/metrics"})
					return buf.String()
				}, 5*time.Minute, 5*time.Second).Should(gomega.ContainSubstring("cne_api_events_published"),
					"api metrics not found")

			})
			ginkgo.It("Should check for event framework api", func() {
				ginkgo.By("Checking event api is healthy")
				gomega.Eventually(func() string {
					buf, _ := pods.ExecCommand(testclient.Client, producerPod, testutils.EventProxyContainerName, []string{"curl", "127.0.0.1:9043/api/ocloudNotifications/v2/health"})
					return buf.String()
				}, 5*time.Minute, 5*time.Second).Should(gomega.ContainSubstring("OK"),
					"Event API is not in healthy state")

				ginkgo.By("Checking mock publisher is created")
				gomega.Eventually(func() string {
					buf, _ := pods.ExecCommand(testclient.Client, producerPod, testutils.EventProxyContainerName, []string{"curl", "127.0.0.1:9043/api/ocloudNotifications/v2/publishers"})
					return buf.String()
				}, 5*time.Minute, 5*time.Second).Should(gomega.ContainSubstring("EndpointUri"),
					"Event API did not return publishers")
			})

			ginkgo.It("Should check for event generated", func() {
				ginkgo.By("Checking  logs")
				format.MaxLength = 20000
				podLogs, err := pods.GetLog(&producerPod, testutils.EventProxyContainerName)
				gomega.Expect(err).NotTo(gomega.HaveOccurred(), "Error to find needed log due to %s", err)
				gomega.Expect(podLogs).Should(gomega.ContainSubstring("Created publisher"),
					fmt.Sprintf("Event publisher was not created in pod %s", producerPod.Name))
				gomega.Expect(podLogs).Should(gomega.ContainSubstring("event sent"),
					fmt.Sprintf("Event was not generated in the pod %s", producerPod.Name))
				gomega.Expect(podLogs).Should(gomega.ContainSubstring("post events"),
					fmt.Sprintf("Event posting did not succeed  %s", producerPod.Name))
			})
		})

		ginkgo.Context("cloud event consumer validation", func() {
			ginkgo.It("Should check for consumer", func() {
				ginkgo.By("Checking event consumer container and event proxy container present")
				gomega.Expect(len(consumerPod.Spec.Containers)).To(gomega.BeNumerically("==", 1), "consumer doesn't have required no of  containers ")
			})

			ginkgo.It("Should check for event received ", func() {
				ginkgo.By("Checking  logs")
				podLogs, err := pods.GetLog(&consumerPod, testutils.ConsumerContainerName)
				gomega.Expect(err).NotTo(gomega.HaveOccurred(), "Error to find needed log due to %s", err)
				gomega.Expect(podLogs).Should(gomega.ContainSubstring("created subscription"),
					fmt.Sprintf("Event publisher was not created in pod %s", producerPod.Name))
				gomega.Expect(podLogs).Should(gomega.ContainSubstring("received event"),
					fmt.Sprintf("Event was not generated in the pod %s", producerPod.Name))
				gomega.Expect(podLogs).ShouldNot(gomega.ContainSubstring("context deadline exceeded"),
					fmt.Sprintf("AMQ failed to post due to context deadline exceeded %s", producerPod.Name))
				gomega.Expect(podLogs).Should(gomega.ContainSubstring("Got CurrentState"),
					fmt.Sprintf("Event was not return by Got CurrentState call %s", producerPod.Name))
			})

		})
	})

})
