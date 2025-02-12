// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package dryruntest

import (
	"embed"
	"testing"

	"open-cluster-management.io/config-policy-controller/test/dryrun"
)

//go:embed missing_kind_name/*
var missingKindName embed.FS

func TestMissingKindName(t *testing.T) {
	t.Run("Test Missing kind and name", dryrun.Run(missingKindName, "missing_kind_name"))
}

//go:embed missing_name/*
var missingName embed.FS

func TestMissingName(t *testing.T) {
	t.Run("Test Missing name", dryrun.Run(missingName, "missing_name"))
}

//go:embed missing_kind/*
var missingKind embed.FS

func TestMissingKind(t *testing.T) {
	t.Run("Test Missing kind", dryrun.Run(missingKind, "missing_kind"))
}
