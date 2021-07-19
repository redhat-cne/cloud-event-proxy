#!/usr/bin/env bash

set -eu
make deps-update
make build-plugins
make build-only
