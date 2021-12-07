//go:build !unittests
// +build !unittests

package cne

import (
	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"
	testclient "github.com/redhat-cne/cloud-event-proxy/test/utils/client"
	"github.com/redhat-cne/cloud-event-proxy/test/utils/execute"
)

var _ = ginkgo.Describe("[cloud-et-proxy]", func() {
	ginkgo.BeforeEach(func() {
		gomega.Expect(testclient.Client).NotTo(gomega.BeNil())
	})
	ginkgo.Context("cloud event components discovery", func() {
		ginkgo.It("Should deploy amq if not  available", func() {

		})

		ginkgo.It("Should deploy consumer if not  available", func() {

		})

		ginkgo.By("Configure cloud event proxy with amq endpoint", func() {

		})
	})
	ginkgo.Describe("Cloud event proxy  e2e tests", func() {
		execute.BeforeAll(func() {

		})

	})
})
