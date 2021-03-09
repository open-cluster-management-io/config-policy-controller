#!/bin/bash
# Copyright Contributors to the Open Cluster Management project

set -e

echo "BUILD GOES HERE!"

echo "<repo>/<component>:<tag> : $1"

# Run our build target and set IMAGE_NAME_AND_VERSION
export IMAGE_NAME_AND_VERSION=${1}
make build-images
