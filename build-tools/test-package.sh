#!/bin/bash

# Licensed Materials - Property of IBM
# (c) Copyright IBM Corporation 2018, 2019. All Rights Reserved.
# Note to U.S. Government Users Restricted Rights:
# Use, duplication or disclosure restricted by GSA ADP Schedule
# Contract with IBM Corp.

# NOTE: This script should not be called directly. Please run `make test`.

set -e

_package=$1
echo "Testing package $_package"

# Make sure temporary files do not exist
rm -f test.out
rm -f cover.tmp

# Run tests
# -coverpkg=./... produces warnings to stderr that we filter out
go test -v -cover -coverpkg=./... -covermode=atomic -coverprofile=cover.tmp $_package  | tee test.out
# save the exit code from the go test command so we fail if tests fail
exit_code=${PIPESTATUS[0]}

# Support for TAP output
# note that it must return go files so using ./cmd
_package_base=$(go list ./test/integration)/..
_package_base=${_package_base%/*}
_tap_name="${_package/$_package_base/}"
_tap_name=${_tap_name//\//_}
if [ ! -d $GOPATH/src/$_package_base/test-output ]
    then mkdir -p $GOPATH/src/$_package_base/test-output
fi
cat test.out | $GOPATH/bin/patter > $GOPATH/src/$_package_base/test-output/$_tap_name.tap

# Merge coverage files
if [ -a cover.tmp ]; then
    $GOPATH/bin/gocovmerge cover.tmp cover.out > cover.all
    mv cover.all cover.out
fi

# Clean up temporary files
rm -f test.out
rm -f cover.tmp

# return the exit code from the test
exit $exit_code