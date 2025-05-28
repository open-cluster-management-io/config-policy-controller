// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package dryruntest

import (
	"embed"
	"testing"

	"open-cluster-management.io/config-policy-controller/test/dryrun"
)

var (
	//go:embed truncated
	truncated embed.FS
	//go:embed secret_obj_temp
	secretObjTemp embed.FS
	//go:embed from_secret_obj_temp_raw
	fromSecretObjTempRaw embed.FS
	//go:embed from_secret_obj_temp
	fromSecretObjTemp embed.FS

	testCases = map[string]embed.FS{
		"Diff is truncated":                                    truncated,
		"No diff when configuring a Secret":                    secretObjTemp,
		"No diff when using fromSecret (object-templates-raw)": fromSecretObjTempRaw,
		"No diff when using fromSecret (object-templates)":     fromSecretObjTemp,
	}
)

func TestContextVariables(t *testing.T) {
	for name, testFiles := range testCases {
		t.Run("Test Diff: "+name, dryrun.Run(testFiles))
	}
}
