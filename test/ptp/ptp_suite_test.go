//go:build !unittests
// +build !unittests

package ptp_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = func() bool {
	testing.Init()
	return true
}()

func TestTest(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "PTP")
}
