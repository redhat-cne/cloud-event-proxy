#!/usr/bin/env bash

set -eu

make build-ptp-operator-plugin
make build-amqp-plugin
make build-only
