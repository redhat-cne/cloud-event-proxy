#!/usr/bin/env bash

set -eu

make build-rest-plugin
make build-amqp-plugin
make build-only