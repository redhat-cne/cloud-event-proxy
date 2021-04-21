#!/usr/bin/env bash
repo_root=$(dirname $0)/..

BUILDCMD=${BUILDCMD:-podman build}

REPO=${REPO:-cloud-event-proxy}
if [ -z ${VERSION+a} ]; then
	VERSION=$(git describe --abbrev=8 --dirty --always)
fi
NAME=${REPO}:${VERSION}

${BUILDCMD} --squash  -f Dockerfile -t "${NAME}" $(dirname $0)/..
