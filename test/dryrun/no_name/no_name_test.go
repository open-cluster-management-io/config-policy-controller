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
	t.Run("Test noncompliant with 2 unmatched resources",
		dryrun.Run(noncompliantRelatedObj))
}

//go:embed compliant_related_obj/*
var compliantRelatedObj embed.FS

func TestCompliantRelatedObj(t *testing.T) {
	t.Run("Test compliant with 1 matched resources", dryrun.Run(compliantRelatedObj))
}

//go:embed mustnothave_compliant_related_obj/*
var mustnothaveCompliantRelatedObj embed.FS

func TestMustnothaveCompliantRelatedObj(t *testing.T) {
	t.Run("Test mustnothave compliant with 0 resources",
		dryrun.Run(mustnothaveCompliantRelatedObj))
}

//go:embed mustnothave_compliant_related_obj_1/*
var mustnothaveCompliantRelatedObj1 embed.FS

func TestMustnothaveCompliantRelatedObj1(t *testing.T) {
	t.Run("Test mustnothave compliant with 1 unmatched resources",
		dryrun.Run(mustnothaveCompliantRelatedObj1))
}

//go:embed mustnothave_noncompliant_related_obj_1/*
var mustnothaveNonCompliantRelatedObj1 embed.FS

func TestMustnothaveNonCompliantRelatedObj1(t *testing.T) {
	t.Run("Test mustnothave noncompliant with 1 matched resources",
		dryrun.Run(mustnothaveNonCompliantRelatedObj1))
}
