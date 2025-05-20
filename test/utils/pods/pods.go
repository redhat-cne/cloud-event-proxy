// Package pods ...
package pods

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"

	testclient "github.com/redhat-cne/cloud-event-proxy/test/utils/client"
)

// GetLog connects to a pod and fetches log
func GetLog(p *corev1.Pod, containerName string) (string, error) {
	req := testclient.Client.Pods(p.Namespace).GetLogs(p.Name, &corev1.PodLogOptions{Container: containerName})
	log, err := req.Stream(context.Background())
	if err != nil {
		return "", err
	}
	defer log.Close()

	buf := new(bytes.Buffer)
	_, err = io.Copy(buf, log)

	if err != nil {
		return "", err
	}

	return buf.String(), nil
}

// ExecCommand runs command in the pod and returns buffer output
func ExecCommand(cs *testclient.Set, pod corev1.Pod, containerName string, command []string) (bytes.Buffer, error) {
	var buf bytes.Buffer
	req := testclient.Client.CoreV1Interface.RESTClient().
		Post().
		Namespace(pod.Namespace).
		Resource("pods").
		Name(pod.Name).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: containerName,
			Command:   command,
			Stdin:     true,
			Stdout:    true,
			Stderr:    true,
			TTY:       true,
		}, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(cs.Config, "POST", req.URL())
	if err != nil {
		return buf, err
	}

	err = exec.Stream(remotecommand.StreamOptions{
		Stdin:  os.Stdin,
		Stdout: &buf,
		Stderr: os.Stderr,
		Tty:    true,
	})

	return buf, err
}

func CheckSinglePodRunning(namespace, deployment, app string) {
	label := fmt.Sprintf("app=%s", app)

	ginkgo.By(fmt.Sprintf("Checking deployment %s is created", deployment))
	gomega.Eventually(func() int {
		deploy, err := testclient.Client.Deployments(namespace).Get(context.Background(), deployment, metav1.GetOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		return int(deploy.Status.Replicas)
	}, 5*time.Minute, 2*time.Second).Should(gomega.Equal(1))

	ginkgo.By(fmt.Sprintf("Checking pod with label %s is created", label))
	gomega.Eventually(func() int {
		pods, err := testclient.Client.Pods(namespace).List(context.Background(), metav1.ListOptions{
			LabelSelector: label})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		return len(pods.Items)
	}, 5*time.Minute, 2*time.Second).Should(gomega.Equal(1))

	ginkgo.By(fmt.Sprintf("Checking pod with label %s is running", label))
	gomega.Eventually(func() corev1.PodPhase {
		pods, err := testclient.Client.Pods(namespace).List(context.Background(), metav1.ListOptions{
			LabelSelector: label})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		return pods.Items[0].Status.Phase
	}, 5*time.Minute, 2*time.Second).Should(gomega.Equal(corev1.PodRunning))
}
