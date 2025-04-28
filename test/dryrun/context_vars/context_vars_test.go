// Copyright (c) 2025 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package dryruntest

import (
	"embed"
	"testing"

	"open-cluster-management.io/config-policy-controller/test/dryrun"
)

var (
	//go:embed object_namespaced
	objNamespaced embed.FS
	//go:embed object_cluster_scoped
	objClusterScoped embed.FS
	//go:embed object_templated_ns
	objTmplNs embed.FS
	//go:embed object_unnamed_objdef
	objUnnamedDef embed.FS
	//go:embed objectns_cluster_scoped
	objNsClusterScoped embed.FS

	testCases = map[string]embed.FS{
		"Test Object: available for namespaced objects":                objNamespaced,
		"Test Object: available for cluster-scoped objects":            objClusterScoped,
		"Test Object: unavailable when namespace is templated":         objTmplNs,
		"Test Object: unavailable for unnamed objects":                 objUnnamedDef,
		"Test ObjectNamespace: unavailable for cluster-scoped objects": objNsClusterScoped,
	}
)

func TestContextVariables(t *testing.T) {
	for name, testFiles := range testCases {
		t.Run(name, dryrun.Run(testFiles))
	}
}
