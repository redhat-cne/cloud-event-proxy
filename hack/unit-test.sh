#!/bin/bash
# Run unit tests and generate filtered coverage profile.
# This wraps `make gha` which handles GOPATH setup and plugin builds
# required for GO111MODULE=off test execution.
set -e

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "${REPO_ROOT}"

make gha
grep -vE "/test/|/examples/|/plugins/mock/|vendor/" cover.out > coverage.out
