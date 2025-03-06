#!/bin/bash
set -x

GOPATH="${GOPATH:-~/go}"
JUNIT_OUTPUT="${JUNIT_OUTPUT:-/tmp/artifacts/unit_report.xml}"
export PATH=$GOPATH/bin:$PATH  # Prioritize $GOPATH/bin in PATH

# Function to check if Ginkgo v1.x is installed
check_ginkgo_v1() {
    if command -v ginkgo &>/dev/null; then
        version=$(ginkgo version 2>&1 | awk '/Ginkgo Version/ {print $3}')
        major_version=$(echo "$version" | cut -d. -f1)
        [ "$major_version" -eq 1 ] && return 0
    fi
    return 1
}

# Install Ginkgo v1.16.5 if needed
if ! check_ginkgo_v1; then
    echo "Installing Ginkgo v1.16.5"
    go install -mod=mod github.com/onsi/ginkgo/ginkgo@v1.16.5
fi

# Verify installation
if ! command -v ginkgo &>/dev/null; then
    echo "Ginkgo installation failed"
    exit 1
fi

# Configure environment for Ginkgo v1
export ACK_GINKGO_DEPRECATIONS=1.16.5
export ACK_GINKGO_RC=true

# Run tests with Ginkgo v1
GOFLAGS=-mod=vendor ginkgo -v "$SUITE" -- -junit "$JUNIT_OUTPUT"
