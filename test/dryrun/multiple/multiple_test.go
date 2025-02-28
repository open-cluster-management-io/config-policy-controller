// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package dryruntest

import (
	"embed"
	"testing"

	"open-cluster-management.io/config-policy-controller/test/dryrun"
)

//go:embed multiple_obj_combo/*
var multipleObjCombo embed.FS

func TestMultipleObjCombo(t *testing.T) {
	t.Run("Test multiple object combo", dryrun.Run(multipleObjCombo, "multiple_obj_combo"))
}

//go:embed multiple_namespace_inform/*
var multipleNsInfrom embed.FS

func TestMultipleNsInform(t *testing.T) {
	t.Run("Test multiple namespace inform", dryrun.Run(multipleNsInfrom, "multiple_namespace_inform"))
}

//go:embed multiple_namespace_enforce/*
var multipleNsEnforce embed.FS

func TestMultipleNsEnforce(t *testing.T) {
	t.Run("Test multiple namespace enforce", dryrun.Run(multipleNsEnforce, "multiple_namespace_enforce"))
}

//go:embed multiple_obj_template/*
var multipleObjTemp embed.FS

func TestMultipleObjTemp(t *testing.T) {
	t.Run("Test multiple object template", dryrun.Run(multipleObjTemp, "multiple_obj_template"))
}

//go:embed multiple_mustnothave/*
var multipleMustNotHave embed.FS

func TestMultipleMustNotHave(t *testing.T) {
	// This test verifies that the mustnothave policy flags both objects which match the
	// object selector, even though one of them has other fields that doesn't match what
	// is defined in the policy.
	t.Run("Test objectSelector with mustnothave and a templated name",
		dryrun.Run(multipleMustNotHave, "multiple_mustnothave"))
}
