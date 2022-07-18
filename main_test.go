//go:build e2e
// +build e2e

// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package main

import (
	"fmt"
	"os"
	"testing"
)

// TestRunMain wraps the main() function in order to build a test binary and collection coverage for
// E2E/Integration tests. Controller CLI flags are also passed in here.
func TestRunMain(t *testing.T) {
	os.Args = append(
		os.Args, "--leader-elect=false", fmt.Sprintf("--target-kubeconfig-path=%s", os.Getenv("TARGET_KUBECONFIG_PATH")),
	)

	main()
}
