// Copyright (c) 2026 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	testclient "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
	sdktls "open-cluster-management.io/sdk-go/pkg/tls"
)

// allowSelfSubjectAccessReviews makes a fake clientset report every SelfSubjectAccessReview as
// allowed. Without this, the fake client's default "create" reaction just stores and echoes back
// the submitted object, leaving Status.Allowed at its false zero value - indistinguishable from a
// real permission denial.
func allowSelfSubjectAccessReviews(client *testclient.Clientset) {
	client.PrependReactor("create", "selfsubjectaccessreviews",
		func(action k8stesting.Action) (bool, runtime.Object, error) {
			//nolint:forcetypeassert
			review := action.(k8stesting.CreateAction).GetObject().(*authorizationv1.SelfSubjectAccessReview)
			review.Status.Allowed = true

			return true, review, nil
		},
	)
}

func TestResolveTLSConfigFlagsTakePrecedence(t *testing.T) {
	wantVersion, err := sdktls.ParseTLSVersion("VersionTLS13")
	require.NoError(t, err)

	// A nil kubeClient would panic if resolveTLSConfig tried to use it, proving the
	// ConfigMap path is skipped when the flags are set.
	cfg, err := resolveTLSConfig(t.Context(), "VersionTLS13", "", nil, "", func() {})

	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, wantVersion, cfg.MinVersion)
}

func TestResolveTLSConfigInvalidFlagsReturnsError(t *testing.T) {
	_, err := resolveTLSConfig(t.Context(), "not-a-version", "", nil, "", func() {})

	assert.Error(t, err)
}

func TestResolveTLSConfigNoFlagsNoClusterFallsBackToNil(t *testing.T) {
	cfg, err := resolveTLSConfig(t.Context(), "", "", nil, "", func() {})

	require.NoError(t, err)
	assert.Nil(t, cfg, "expected nil config so caller falls back to a TLS 1.2 floor with Go's default cipher suites")
}

func TestResolveTLSConfigReadsConfigMapWhenNoFlags(t *testing.T) {
	client := testclient.NewSimpleClientset(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      sdktls.ConfigMapName,
			Namespace: "addon-ns",
		},
		Data: map[string]string{
			sdktls.ConfigMapKeyMinVersion: "VersionTLS13",
		},
	})
	allowSelfSubjectAccessReviews(client)

	cfg, err := resolveTLSConfig(t.Context(), "", "", client, "addon-ns", func() {})

	require.NoError(t, err)
	require.NotNil(t, cfg)

	wantVersion, err := sdktls.ParseTLSVersion("VersionTLS13")
	require.NoError(t, err)
	assert.Equal(t, wantVersion, cfg.MinVersion)
}

func TestResolveTLSConfigDefaultsWhenConfigMapAbsent(t *testing.T) {
	client := testclient.NewSimpleClientset()
	allowSelfSubjectAccessReviews(client)

	cfg, err := resolveTLSConfig(t.Context(), "", "", client, "addon-ns", func() {})

	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, sdktls.GetDefaultTLSConfig().MinVersion, cfg.MinVersion)
}

func TestResolveTLSConfigMissingConfigMapPermissionReturnsError(t *testing.T) {
	// No allowSelfSubjectAccessReviews call: the fake client's default reaction leaves
	// Status.Allowed at its false zero value, simulating denied RBAC.
	client := testclient.NewSimpleClientset(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      sdktls.ConfigMapName,
			Namespace: "addon-ns",
		},
	})

	_, err := resolveTLSConfig(t.Context(), "", "", client, "addon-ns", func() {})

	assert.Error(t, err)
}
