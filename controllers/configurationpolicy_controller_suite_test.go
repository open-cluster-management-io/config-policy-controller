// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package controllers

import (
	stdlog "log"
	"os"
	"path/filepath"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	policyv1 "open-cluster-management.io/config-policy-controller/api/v1"
)

var samplePolicy = policyv1.ConfigurationPolicy{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "foo",
		Namespace: "default",
	},
	Spec: policyv1.ConfigurationPolicySpec{
		Severity: "low",
		NamespaceSelector: policyv1.Target{
			Include: []policyv1.NonEmptyString{"default", "kube-*"},
			Exclude: []policyv1.NonEmptyString{"kube-system"},
		},
		RemediationAction: "inform",
		ObjectTemplates: []*policyv1.ObjectTemplate{
			{
				ComplianceType:   "musthave",
				ObjectDefinition: runtime.RawExtension{},
			},
		},
	},
}

func TestMain(m *testing.M) {
	t := &envtest.Environment{
		CRDDirectoryPaths: []string{filepath.Join("..", "..", "..", "deploy", "crds")},
	}

	err := policyv1.AddToScheme(scheme.Scheme)
	if err != nil {
		stdlog.Fatal(err)
	}

	if cfg, err = t.Start(); err != nil {
		stdlog.Fatal(err)
	}

	code := m.Run()

	if err = t.Stop(); err != nil {
		stdlog.Fatal(err)
	}

	os.Exit(code)
}
