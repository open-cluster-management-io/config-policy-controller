#!/bin/bash

# Licensed Materials - Property of IBM
# (c) Copyright IBM Corporation 2018, 2019. All Rights Reserved.
# Note to U.S. Government Users Restricted Rights:
# Use, duplication or disclosure restricted by GSA ADP Schedule
# Contract with IBM Corp.

set -e

### etcd binary for zLinux requires this "ETCD_UNSUPPORTED_ARCH=s390x" exported
if [[ `uname -p` == "s390x" ]]; then
    export ETCD_UNSUPPORTED_ARCH=s390x
fi

_script_dir=$(dirname "$0")
echo 'mode: atomic' > cover.out
echo '' > cover.tmp

# only test those packages containing test files
test_dirs=($(go list ./... ))
for dir in "${test_dirs[@]}"; do 
    if [ -n "$(ls -A ${GOPATH}/src/${dir}/*_test.go 2>/dev/null)" ]
    then
        echo "${dir} has test files"
        $_script_dir/test-package.sh ${dir} 
    else
        echo "${dir} has no test files"
    fi
done

