// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package dryruntest

import (
	"embed"
	"testing"

	"open-cluster-management.io/config-policy-controller/test/dryrun"
)

//go:embed truncated/*
var truncated embed.FS

// Status comparing should match
func TestTruncated(t *testing.T) {
	t.Run("Test Diff generation that is truncated",
		dryrun.Run(truncated, "truncated"))
}

//go:embed secret_obj_temp/*
var secretObjTemp embed.FS

func TestSecretObjTemp(t *testing.T) {
	t.Run("Test does not automatically generate a diff when configuring a Secret",
		dryrun.Run(secretObjTemp, "secret_obj_temp"))
}

//go:embed from_secret_obj_temp_raw/*
var fromSecretObjTempRaw embed.FS

func TestFromSecretObjTempRaw(t *testing.T) {
	t.Run("Test does not automatically generate a diff when using fromSecret (object-templates-raw)",
		dryrun.Run(fromSecretObjTempRaw, "from_secret_obj_temp_raw"))
}

//go:embed from_secret_obj_temp/*
var fromSecretObjTemp embed.FS

func TestFromSecretObjTemp(t *testing.T) {
	t.Run("Test does not automatically generate a diff when using fromSecret (object-templates)",
		dryrun.Run(fromSecretObjTemp, "from_secret_obj_temp"))
}
