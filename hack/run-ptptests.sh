#!/bin/bash
set -x
which ginkgo
if [ $? -ne 0 ]; then
# we are moving to a temp folder as in go.mod we have a dependency that is not
# resolved if we are not using google's GOPROXY. That is not the case when building as
# we are using vendored dependencies
	GINKGO_TMP_DIR=$(mktemp -d)
	cd $GINKGO_TMP_DIR
	go mod init tmp
	go install -mod=mod github.com/onsi/ginkgo/v2/ginkgo
	go get github.com/onsi/gomega/...
	rm -rf $GINKGO_TMP_DIR
	echo "Downloading ginkgo tool"
	cd -
fi

#TEST_TYPE="${TEST_TYPE:-BC}"
GOPATH="${GOPATH:-~/go}"
JUNIT_OUTPUT="${JUNIT_OUTPUT:-/tmp/artifacts/unit_report.xml}"
export PATH=$PATH:$GOPATH/bin

echo -n "PTP Config type ( OC, BC, DUAL_NIC)"
read -r TEST_TYPE
echo -n "Test Case ( DEFAULT, FAULT_SLAVE, FAULTY_MASTER, FOREVER)"
read -r TEST_CASE

GOFLAGS=-mod=vendor ginkgo --label-filter "$TEST_TYPE" "$SUITE"  -junit $JUNIT_OUTPUT
