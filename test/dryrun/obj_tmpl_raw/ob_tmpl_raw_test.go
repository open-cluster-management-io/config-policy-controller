// Copyright (c) 2025 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package dryruntest

import (
	"embed"
	"testing"

	"open-cluster-management.io/config-policy-controller/test/dryrun"
)

//go:embed invalid_yaml/*
var invalidObjectTemplatesRaw embed.FS

//go:embed range/*
var rangeObjectTemplatesRaw embed.FS

func TestInvalidObjectTemplatesRaw(t *testing.T) {
	t.Run("Test noncompliant with invalid YAML in object-templates-raw",
		dryrun.Run(invalidObjectTemplatesRaw))
}

func TestRangeObjectTemplatesRaw(t *testing.T) {
	t.Run("Test compliant with range in object-templates-raw",
		dryrun.Run(rangeObjectTemplatesRaw))
}
