#!/usr/bin/env bash
VERSION=l2disc
IMAGE_NAME=test
REPO=quay.io/deliedit
make test

if [[ $# -eq 0 ]]; then
	echo "Running locally"
	go install github.com/crazy-max/xgo@latest
	mkdir -p /tmp/xgo-cache
	xgo -go 1.22 -out l2discovery -dest . -targets linux/amd64,linux/arm64 -v -ldflags "-s -w" -buildmode default -trimpath .
fi
podman manifest create ${REPO}/${IMAGE_NAME}:${VERSION}
podman build --platform linux/amd64,linux/arm64 --manifest ${REPO}/${IMAGE_NAME}:${VERSION} --rm -f Dockerfile .
podman manifest push ${REPO}/${IMAGE_NAME}:${VERSION}
