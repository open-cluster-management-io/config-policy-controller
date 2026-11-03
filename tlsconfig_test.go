// Copyright (c) 2026 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package main

import (
	"context"
	"crypto/tls"
	"errors"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	configv1 "github.com/openshift/api/config/v1"
	openshifttls "github.com/openshift/controller-runtime-common/pkg/tls"
	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	testclient "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	k8stesting "k8s.io/client-go/testing"
	sdktls "open-cluster-management.io/sdk-go/pkg/tls"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

func init() {
	RegisterFailHandler(Fail)
}

// allowSelfSubjectAccessReviews makes a fake clientset report every SelfSubjectAccessReview as
// allowed. Without this, the fake client's default "create" reaction just stores and echoes back
// the submitted object, leaving Status.Allowed at its false zero value - indistinguishable from a
// real permission denial.
func allowSelfSubjectAccessReviews(client *testclient.Clientset) {
	client.PrependReactor("create", "selfsubjectaccessreviews",
		func(action k8stesting.Action) (bool, k8sruntime.Object, error) {
			//nolint:forcetypeassert
			review := action.(k8stesting.CreateAction).GetObject().(*authorizationv1.SelfSubjectAccessReview)
			review.Status.Allowed = true

			return true, review, nil
		},
	)
}

func failSelfSubjectAccessReviews(client *testclient.Clientset) {
	client.PrependReactor("create", "selfsubjectaccessreviews",
		func(k8stesting.Action) (bool, k8sruntime.Object, error) {
			return true, nil, errors.New("review failed")
		},
	)
}

func newAPIServerTestScheme(t *testing.T) *k8sruntime.Scheme {
	t.Helper()

	s := k8sruntime.NewScheme()
	utilruntime.Must(configv1.Install(s))

	return s
}

// applyToConfig runs a TLSOpts function against a fresh tls.Config and returns it, to make
// assertions on the result easier to read.
func applyToConfig(f func(*tls.Config)) *tls.Config {
	cfg := &tls.Config{} //nolint:gosec // test-only, never used to actually serve traffic
	f(cfg)

	return cfg
}

func TestFetchAPIServerTLSProfileSpecNotFound(t *testing.T) {
	c := fakeclient.NewClientBuilder().WithScheme(newAPIServerTestScheme(t)).Build()

	spec, found, watchable := fetchAPIServerTLSProfileSpec(t.Context(), c)

	Expect(found).To(BeFalse())
	Expect(watchable).To(BeFalse(), "no CRD/resource means there's nothing to watch")
	Expect(spec).To(Equal(configv1.TLSProfileSpec{}))
}

// TestFetchAPIServerTLSProfileSpecForbiddenStaysWatchable proves that an RBAC-denied Get is
// treated as possibly transient: found is false (fall back to defaults now), but watchable stays
// true so the drift watcher still gets registered. If the permission is later granted, the
// watcher's informer will sync, see the real profile differs from the zero-value spec it was
// seeded with, and trigger a restart to pick it up -- without any dedicated retry loop.
func TestFetchAPIServerTLSProfileSpecForbiddenStaysWatchable(t *testing.T) {
	c := fakeclient.NewClientBuilder().
		WithScheme(newAPIServerTestScheme(t)).
		WithInterceptorFuncs(interceptor.Funcs{
			Get: func(
				_ context.Context, _ client.WithWatch, _ client.ObjectKey, _ client.Object, _ ...client.GetOption,
			) error {
				return apierrors.NewForbidden(configv1.Resource("apiservers"), openshifttls.APIServerName, nil)
			},
		}).
		Build()

	spec, found, watchable := fetchAPIServerTLSProfileSpec(t.Context(), c)

	Expect(found).To(BeFalse())
	Expect(watchable).To(BeTrue(), "an RBAC/connectivity error might be transient, so it's still worth watching")
	Expect(spec).To(Equal(configv1.TLSProfileSpec{}))
}

func TestResolveEffectiveTLSConfigFlagsTakePrecedence(t *testing.T) {
	// Flags must be honored without ever consulting the (nil, in this test) rest.Config/APIServer.
	tlsOptsFunc, watchAPIServer, spec, err := resolveEffectiveTLSConfig(t.Context(), nil, "VersionTLS13", "", "")

	Expect(err).ToNot(HaveOccurred())
	Expect(watchAPIServer).To(BeFalse())
	Expect(spec).To(Equal(configv1.TLSProfileSpec{}))
	Expect(applyToConfig(tlsOptsFunc).MinVersion).To(Equal(uint16(tls.VersionTLS13)))
}

func TestResolveEffectiveTLSConfigInvalidFlagsReturnError(t *testing.T) {
	for name, tc := range map[string]struct {
		minVersion, cipherSuites, groups string
	}{
		"min version":  {minVersion: "not-a-real-version"},
		"cipher suite": {cipherSuites: "NOT_A_CIPHER"},
		"group":        {groups: "not-a-group"},
	} {
		t.Run(name, func(t *testing.T) {
			g := NewWithT(t)

			tlsOptsFunc, watchAPIServer, spec, err := resolveEffectiveTLSConfig(
				t.Context(), nil, tc.minVersion, tc.cipherSuites, tc.groups,
			)

			g.Expect(err).To(HaveOccurred())
			g.Expect(tlsOptsFunc).To(BeNil())
			g.Expect(watchAPIServer).To(BeFalse())
			g.Expect(spec).To(Equal(configv1.TLSProfileSpec{}))
		})
	}
}

// newAPIServerCRD returns a minimal APIServer CRD so envtest can serve the resource without
// checking in the full generated manifest. Only spec.tlsSecurityProfile is preserved;
// that's all resolveEffectiveTLSConfig reads.
func newAPIServerCRD() *apiextensionsv1.CustomResourceDefinition {
	preserveUnknown := true

	return &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: "apiservers.config.openshift.io"},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: configv1.GroupVersion.Group,
			Scope: apiextensionsv1.ClusterScoped,
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Plural: "apiservers",
				Kind:   "APIServer",
			},
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{{
				Name:    configv1.GroupVersion.Version,
				Served:  true,
				Storage: true,
				Schema: &apiextensionsv1.CustomResourceValidation{
					OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
						Type:                   "object",
						XPreserveUnknownFields: &preserveUnknown,
					},
				},
			}},
		},
	}
}

// TestResolveEffectiveTLSConfigAppliesAPIServerProfile is an integration test: it reads a TLS
// profile from a real API server and applies the resulting TLSOpts to a tls.Config.
func TestResolveEffectiveTLSConfigAppliesAPIServerProfile(t *testing.T) {
	testEnv := &envtest.Environment{CRDs: []*apiextensionsv1.CustomResourceDefinition{newAPIServerCRD()}}

	restCfg, err := testEnv.Start()
	if err != nil {
		t.Fatalf("start envtest: %v", err)
	}

	defer func() {
		Expect(testEnv.Stop()).To(Succeed())
	}()

	profile := configv1.TLSProfileSpec{
		Groups: []configv1.TLSGroup{configv1.TLSGroupSecP384r1, configv1.TLSGroupX25519},
	}

	apiServerClient, err := client.New(restCfg, client.Options{Scheme: scheme})
	Expect(err).ToNot(HaveOccurred())
	Expect(apiServerClient.Create(t.Context(), &configv1.APIServer{
		ObjectMeta: metav1.ObjectMeta{Name: openshifttls.APIServerName},
		Spec: configv1.APIServerSpec{
			TLSSecurityProfile: &configv1.TLSSecurityProfile{
				Type:   configv1.TLSProfileCustomType,
				Custom: &configv1.CustomTLSProfile{TLSProfileSpec: profile},
			},
		},
	})).To(Succeed())

	tlsOptsFunc, watchAPIServer, resolvedSpec, err := resolveEffectiveTLSConfig(t.Context(), restCfg, "", "", "")
	Expect(err).ToNot(HaveOccurred())
	Expect(watchAPIServer).To(BeTrue())
	Expect(resolvedSpec).To(Equal(profile))

	expectedCurves := []tls.CurveID{tls.CurveP384, tls.X25519}
	Expect(applyToConfig(tlsOptsFunc).CurvePreferences).To(Equal(expectedCurves))

	// ConfigMap flags must not erase the TLS groups
	tlsOptsFunc, watchAPIServer, _, err = resolveEffectiveTLSConfig(t.Context(), restCfg, "VersionTLS13", "", "")
	Expect(err).ToNot(HaveOccurred())

	config := applyToConfig(tlsOptsFunc)

	Expect(watchAPIServer).To(BeTrue(), "profile groups still come from the APIServer")
	Expect(config.MinVersion).To(Equal(uint16(tls.VersionTLS13)))
	Expect(config.CurvePreferences).To(Equal(expectedCurves))

	// --tls-groups overrides the profile groups
	tlsOptsFunc, watchAPIServer, _, err = resolveEffectiveTLSConfig(t.Context(), restCfg, "", "", "secp256r1")
	Expect(err).ToNot(HaveOccurred())

	config = applyToConfig(tlsOptsFunc)

	Expect(watchAPIServer).To(BeFalse(), "group flag overrides APIServer groups")
	Expect(config.MinVersion).To(Equal(sdktls.GetDefaultTLSConfig().MinVersion))
	Expect(config.CurvePreferences).To(Equal([]tls.CurveID{tls.CurveP256}))

	// Unsupported --tls-groups values are rejected instead of silently ignored
	tlsOptsFunc, watchAPIServer, spec, err := resolveEffectiveTLSConfig(t.Context(), restCfg, "", "", "not-a-group")
	Expect(err).To(HaveOccurred())
	Expect(tlsOptsFunc).To(BeNil())
	Expect(watchAPIServer).To(BeFalse())
	Expect(spec).To(Equal(configv1.TLSProfileSpec{}))
}

// TestReadAPIServerTLSProfileSpecWithoutCRD runs against a real API server (envtest) with
// no CRDs installed, proving the CRD-absent fallback works against real
// client-go/controller-runtime discovery behavior. A fake client can't exercise this: it resolves
// GVKs straight from its scheme instead of doing real API discovery, so it can only ever return
// NotFound, never the meta.IsNoMatchError that fetchAPIServerTLSProfileSpec also checks for.
func TestReadAPIServerTLSProfileSpecWithoutCRD(t *testing.T) {
	testEnv := &envtest.Environment{}

	cfg, err := testEnv.Start()
	Expect(err).ToNot(HaveOccurred())

	defer func() {
		Expect(testEnv.Stop()).To(Succeed())
	}()

	spec, found, watchable := readAPIServerTLSProfileSpec(t.Context(), cfg)

	Expect(found).To(BeFalse())
	Expect(watchable).To(BeFalse(), "no CRD installed means there's nothing to watch")
	Expect(spec).To(Equal(configv1.TLSProfileSpec{}))

	// With no profile and no --tls-groups flag, leave curve preferences as Go defaults.
	tlsOptsFunc, watchAPIServer, resolvedSpec, err := resolveEffectiveTLSConfig(t.Context(), cfg, "", "", "")
	Expect(err).ToNot(HaveOccurred())
	Expect(watchAPIServer).To(BeFalse())
	Expect(resolvedSpec).To(Equal(configv1.TLSProfileSpec{}))
	Expect(applyToConfig(tlsOptsFunc).CurvePreferences).To(BeNil())

	// Without an APIServer profile, --tls-groups is the only source of group preferences
	tlsOptsFunc, watchAPIServer, _, err = resolveEffectiveTLSConfig(t.Context(), cfg, "", "", "X25519")
	Expect(err).ToNot(HaveOccurred())
	Expect(watchAPIServer).To(BeFalse())
	Expect(applyToConfig(tlsOptsFunc).CurvePreferences).To(Equal([]tls.CurveID{tls.X25519}))
}

func TestResolveTLSConfigMapSkipsWhenFlagsFullySpecified(t *testing.T) {
	// Both settings come from flags, so the ConfigMap is irrelevant.
	Expect(resolveOCMTLSConfigMap(
		t.Context(), &rest.Config{}, "VersionTLS13", "TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
	)).To(BeNil())
}

func TestCanWatchTLSProfileConfigMap(t *testing.T) {
	t.Run("allowed", func(t *testing.T) {
		g := NewWithT(t)

		c := testclient.NewSimpleClientset()
		allowSelfSubjectAccessReviews(c)

		allowed, err := canWatchTLSProfileConfigMap(t.Context(), c, "addon-ns")

		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(allowed).To(BeTrue())
	})

	t.Run("denied", func(t *testing.T) {
		g := NewWithT(t)

		allowed, err := canWatchTLSProfileConfigMap(t.Context(), testclient.NewSimpleClientset(), "addon-ns")

		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(allowed).To(BeFalse())
	})

	t.Run("review error", func(t *testing.T) {
		g := NewWithT(t)

		c := testclient.NewSimpleClientset()
		failSelfSubjectAccessReviews(c)

		allowed, err := canWatchTLSProfileConfigMap(t.Context(), c, "addon-ns")

		g.Expect(err).To(HaveOccurred())
		g.Expect(err.Error()).To(ContainSubstring("review failed"))
		g.Expect(allowed).To(BeFalse())
	})
}

func newTLSConfigMap(data map[string]string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: sdktls.ConfigMapName, Namespace: "addon-ns"},
		Data:       data,
	}
}

func TestStartTLSConfigMapWatcherReadsConfigMapWhenNoFlags(t *testing.T) {
	c := testclient.NewSimpleClientset(newTLSConfigMap(map[string]string{
		sdktls.ConfigMapKeyMinVersion: "VersionTLS13",
	}))
	allowSelfSubjectAccessReviews(c)

	tlsCfg := startTLSConfigMapWatcher(t.Context(), c, "addon-ns")

	Expect(tlsCfg).ToNot(BeNil())
	Expect(tlsCfg.MinVersion).To(Equal(uint16(tls.VersionTLS13)))
}

func TestStartTLSConfigMapWatcherDefaultsWhenConfigMapAbsent(t *testing.T) {
	c := testclient.NewSimpleClientset()
	allowSelfSubjectAccessReviews(c)

	tlsCfg := startTLSConfigMapWatcher(t.Context(), c, "addon-ns")

	Expect(tlsCfg).ToNot(BeNil())
	Expect(tlsCfg.MinVersion).To(Equal(sdktls.GetDefaultTLSConfig().MinVersion))
}

func TestStartTLSConfigMapWatcherSkips(t *testing.T) {
	denied := testclient.NewSimpleClientset(newTLSConfigMap(map[string]string{
		sdktls.ConfigMapKeyMinVersion: "VersionTLS13",
	}))
	Expect(startTLSConfigMapWatcher(t.Context(), denied, "addon-ns")).To(BeNil())

	reviewErr := testclient.NewSimpleClientset()
	failSelfSubjectAccessReviews(reviewErr)
	Expect(startTLSConfigMapWatcher(t.Context(), reviewErr, "addon-ns")).To(BeNil())
}
