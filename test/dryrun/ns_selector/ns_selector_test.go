// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package dryruntest

import (
	"embed"
	"testing"

	"open-cluster-management.io/config-policy-controller/test/dryrun"
)

//go:embed ns_default/*
var nsDefault embed.FS

func TestNsDefault(t *testing.T) {
	t.Run("Test compliant with namespaceSelector",
		dryrun.Run(nsDefault))
}
