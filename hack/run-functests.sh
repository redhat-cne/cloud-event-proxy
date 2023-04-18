#!/bin/bash
set -x
which ginkgo
if [ $? -ne 0 ]; then
  echo "Downloading ginkgo tool"
  go install -mod=mod github.com/onsi/ginkgo/ginkgo@v1.16.5
fi

GOPATH="${GOPATH:-~/go}"
JUNIT_OUTPUT="${JUNIT_OUTPUT:-/tmp/artifacts/unit_report.xml}"
export PATH=$PATH:$GOPATH/bin

# silence deprecations
export ACK_GINKGO_DEPRECATIONS=1.16.5
# silence Ginkgo 2.0 notice
export ACK_GINKGO_RC=true
GOFLAGS=-mod=vendor ginkgo "$SUITE" -- -junit $JUNIT_OUTPUT
