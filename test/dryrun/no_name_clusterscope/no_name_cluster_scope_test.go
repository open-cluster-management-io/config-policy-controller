// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package dryruntest

import (
	"embed"
	"testing"

	"open-cluster-management.io/config-policy-controller/test/dryrun"
)

//go:embed noncompliant_related_obj/*
var noncompliantRelatedObj embed.FS

func TestNoncompliantRelatedObj(t *testing.T) {
	t.Run("Test noncompliant with 1 unmatched resources",
		dryrun.Run(noncompliantRelatedObj))
}

//go:embed compliant_related_obj/*
var compliantRelatedObj embed.FS

func TestCompliantRelatedObj(t *testing.T) {
	t.Run("Test compliant with 0 resources",
		dryrun.Run(compliantRelatedObj))
}
