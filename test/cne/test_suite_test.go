//go:build !unittests
// +build !unittests

package cne_test

import (
	"context"
	"flag"
	testutils "github.com/redhat-cne/cloud-event-proxy/test/utils"
	testclient "github.com/redhat-cne/cloud-event-proxy/test/utils/client"
	"github.com/redhat-cne/cloud-event-proxy/test/utils/namespaces"
	"testing"
	"time"

	. "github.com/onsi/ginkgo"
	"github.com/onsi/ginkgo/reporters"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TODO: we should refactor tests to use client from controller-runtime package
// see - https://github.com/openshift/cluster-api-actuator-pkg/blob/master/pkg/e2e/framework/framework.go

var junitPath *string

func init() {
	junitPath = flag.String("junit", "junit.xml", "the path for the junit format report")
}

func TestTest(t *testing.T) {
	RegisterFailHandler(Fail)

	rr := []Reporter{}
	if junitPath != nil {
		rr = append(rr, reporters.NewJUnitReporter(*junitPath))
	}
	RunSpecsWithDefaultAndCustomReporters(t, "cloud native event e2e integration tests", rr)
}

var _ = BeforeSuite(func() {
	// create consumer test  namespace
	Expect(testclient.Client).NotTo(BeNil())

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: testutils.NamespaceTesting,
		},
	}
	_, err := testclient.Client.Namespaces().Create(context.Background(), ns, metav1.CreateOptions{})
	Expect(err).ToNot(HaveOccurred())
})

var _ = AfterSuite(func() {
	err := testclient.Client.Namespaces().Delete(context.Background(), testutils.NamespaceTesting, metav1.DeleteOptions{})
	Expect(err).ToNot(HaveOccurred())
	_ = namespaces.WaitForDeletion(testclient.Client, testutils.NamespaceTesting, 5*time.Minute)
})
