#!/bin/bash

# Licensed Materials - Property of IBM
# (c) Copyright IBM Corporation 2018, 2019. All Rights Reserved.
# Note to U.S. Government Users Restricted Rights:
# Use, duplication or disclosure restricted by GSA ADP Schedule
# Contract with IBM Corp.

set -e

ARCH=`uname -m`


# Optional first parameter, minor build version
BUILD=$TRAVIS_BUILD_NUMBER
REVISION=$(git rev-parse HEAD)
if [ -z "$BUILD" ]; then
    BUILD="0"
fi

ROOT_DIR=$(cd $(dirname $(dirname $0)) && pwd)
echo $ROOT_DIR

# Generate i18n
# $ROOT_DIR/build/generate-i18n-resources.sh

# Build release binaries
rm -rf $ROOT_DIR/bin

for CMD in `ls $ROOT_DIR/cmd`; do
    echo "Building ./cmd/$CMD"
    case $ARCH in
         ppc64le)
                  CGO_ENABLED=0 GOARCH=ppc64le GOOS=linux go build -ldflags "-s" -a -installsuffix cgo -o $ROOT_DIR/bin/${CMD}-linux-ppc64le ./cmd/$CMD
                  ;;
         x86_64)
                  CGO_ENABLED=0 GOARCH=amd64 GOOS=linux go build -ldflags "-s" -a -installsuffix cgo -o $ROOT_DIR/bin/${CMD}-linux-amd64 ./cmd/$CMD
                  ;;

         s390x)
                  CGO_ENABLED=0 GOARCH=s390x GOOS=linux go build -ldflags "-s" -a -installsuffix cgo -o $ROOT_DIR/bin/${CMD}-linux-s390x ./cmd/$CMD
    esac

    #CGO_ENABLED=0 GOARCH=amd64 GOOS=windows go build -ldflags "-s" -a -installsuffix cgo -o $ROOT_DIR/bin/hcmctl-win-amd64.exe ./cmd/$CMD
    #CGO_ENABLED=0 GOARCH=386 GOOS=windows go build -ldflags "-s" -a -installsuffix cgo -o $ROOT_DIR/bin/hcmctl-win-386.exe ./cmd/$CMD   
    #CGO_ENABLED=0 GOARCH=386 GOOS=linux go build -ldflags "-s" -a -installsuffix cgo -o $ROOT_DIR/bin/hcmctl-linux-386 ./cmd/$CMD
    #CGO_ENABLED=0 GOARCH=amd64 GOOS=darwin go build -ldflags "-s" -a -installsuffix cgo -o $ROOT_DIR/bin/hcmctl-darwin-amd64 ./cmd/$CMD
    chmod 0775 "$ROOT_DIR/bin/${CMD}-"*

    # Write the version out to a text file 
    echo "2.1.$BUILD" > $ROOT_DIR/bin/${CMD}-build.txt
    echo "$REVISION" > $ROOT_DIR/bin/${CMD}-revision.txt

    # Print hashes
    sha1sum $ROOT_DIR/bin/${CMD}-*
done

