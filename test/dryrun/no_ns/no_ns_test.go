// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package dryruntest

import (
	"embed"
	"testing"

	"open-cluster-management.io/config-policy-controller/test/dryrun"
)

//go:embed ns_role/*
var nsRole embed.FS

func TestNsRole(t *testing.T) {
	t.Run("Test noncompliant with role",
		dryrun.Run(nsRole))
}

//go:embed enforce_ns/*
var enforceNs embed.FS

func TestEnforceNs(t *testing.T) {
	t.Run("Test compliant with creating a namespace",
		dryrun.Run(enforceNs))
}

//go:embed cluster_role/*
var clusterRole embed.FS

func TestClusterRole(t *testing.T) {
	t.Run("Test clusterRole",
		dryrun.Run(clusterRole))
}
