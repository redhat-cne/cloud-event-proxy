//go:build !unittests
// +build !unittests

package cne_test

import (
	"context"
	"testing"
	"time"

	testutils "github.com/redhat-cne/cloud-event-proxy/test/utils"
	testclient "github.com/redhat-cne/cloud-event-proxy/test/utils/client"
	"github.com/redhat-cne/cloud-event-proxy/test/utils/namespaces"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestTest(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "cloud native event e2e integration tests")

}

var _ = BeforeSuite(func() {
	// create consumer test  namespace
	Expect(testclient.Client).NotTo(BeNil())

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: testutils.NamespaceForTesting,
		},
	}
	_, err := testclient.Client.Namespaces().Create(context.Background(), ns, metav1.CreateOptions{})
	Expect(err).ToNot(HaveOccurred())
})

var _ = AfterSuite(func() {
	err := testclient.Client.Namespaces().Delete(context.Background(), testutils.NamespaceForTesting, metav1.DeleteOptions{})
	Expect(err).ToNot(HaveOccurred())
	_ = namespaces.WaitForDeletion(testclient.Client, testutils.NamespaceForTesting, 5*time.Minute)
})
