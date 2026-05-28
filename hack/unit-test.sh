#!/bin/bash
# Run unit tests and generate filtered coverage profile.
set -e

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "${REPO_ROOT}"

make gha
grep -vE "zz_generated|mock_|/test/|vendor/|/bin/" cover.out > coverage.out
