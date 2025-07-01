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
	//go:embed object_pod
	objPod embed.FS
	//go:embed object_pod_nsselector
	objPodNsSelector embed.FS
	//go:embed object_pod_default_func
	objPodDefaultFunc embed.FS
	//go:embed object_cluster_scoped
	objClusterScoped embed.FS
	//go:embed object_templated_ns
	objTmplNs embed.FS
	//go:embed object_unnamed_objdef
	objUnnamedDef embed.FS
	//go:embed objectns_cluster_scoped
	objNsClusterScoped embed.FS
	//go:embed objectns_templated_empty
	objNsTemplatedEmpty embed.FS
	//go:embed objectns_templated_no_nsselector
	objNsTemplatedNoNsSelector embed.FS

	testCases = map[string]embed.FS{
		"Test Object: available for namespaced objects":                    objNamespaced,
		"Test Object: available for complex objects":                       objPod,
		"Test Object: nil for objects that don't exist":                    objPodNsSelector,
		"Test Object: nil but succeeds with default function":              objPodDefaultFunc,
		"Test Object: available for cluster-scoped objects":                objClusterScoped,
		"Test Object: nil when namespace is templated":                     objTmplNs,
		"Test Object: unavailable for unnamed objects":                     objUnnamedDef,
		"Test ObjectNamespace: unavailable for cluster-scoped objects":     objNsClusterScoped,
		"Test ObjectNamespace: noncompliant for empty templated namespace": objNsTemplatedEmpty,
		"Test ObjectNamespace: available for templated namespace":          objNsTemplatedNoNsSelector,
	}
)

func TestContextVariables(t *testing.T) {
	for name, testFiles := range testCases {
		t.Run(name, dryrun.Run(testFiles))
	}
}
