// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package controllers

import (
	stdlog "log"
	"os"
	"path/filepath"
	"testing"

	policiesv1alpha1 "github.com/open-cluster-management/config-policy-controller/api/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

var samplePolicy = policiesv1alpha1.ConfigurationPolicy{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "foo",
		Namespace: "default",
	},
	Spec: policiesv1alpha1.ConfigurationPolicySpec{
		Severity: "low",
		NamespaceSelector: policiesv1alpha1.Target{
			Include: []policiesv1alpha1.NonEmptyString{"default", "kube-*"},
			Exclude: []policiesv1alpha1.NonEmptyString{"kube-system"},
		},
		RemediationAction: "inform",
		ObjectTemplates: []*policiesv1alpha1.ObjectTemplate{
			&policiesv1alpha1.ObjectTemplate{
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
	policiesv1alpha1.AddToScheme(scheme.Scheme)

	var err error
	if cfg, err = t.Start(); err != nil {
		stdlog.Fatal(err)
	}

	code := m.Run()
	t.Stop()
	os.Exit(code)
}
