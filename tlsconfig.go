// Copyright (c) 2026 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	authorizationv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	sdktls "open-cluster-management.io/sdk-go/pkg/tls"

	"open-cluster-management.io/config-policy-controller/pkg/common"
)

// resolveEffectiveTLSConfig wires resolveTLSConfig into the running process: it discovers the
// controller's own namespace (to watch the ocm-tls-profile ConfigMap), restarts the process on
// ConfigMap changes, and logs the config that will be applied. If no flags or ConfigMap settings
// apply, it falls back to Go's default cipher suites with an explicit TLS 1.2 floor.
func resolveEffectiveTLSConfig(ctx context.Context, restCfg *rest.Config, opts *ctrlOpts) *sdktls.TLSConfig {
	operatorNamespace, err := common.GetOperatorNamespace()
	if err != nil {
		if errors.Is(err, common.ErrNoNamespace) || errors.Is(err, common.ErrRunLocal) {
			log.Info("Not running in a cluster; skipping the ocm-tls-profile ConfigMap watch")
		} else {
			log.Error(err, "Failed to get operator namespace; skipping the ocm-tls-profile ConfigMap watch")
		}

		operatorNamespace = ""
	}

	var kubeClient kubernetes.Interface
	if operatorNamespace != "" {
		kubeClient = kubernetes.NewForConfigOrDie(restCfg)
	}

	tlsCfg, err := resolveTLSConfig(ctx, opts.tlsMinVersion, opts.tlsCipherSuites, kubeClient, operatorNamespace,
		func() {
			log.Info("TLS configuration changed; exiting so the pod restarts with the new settings")
			os.Exit(0)
		},
	)
	if err != nil {
		log.Error(err, "Failed to resolve TLS configuration; falling back to defaults")

		tlsCfg = nil
	}

	effective := tlsCfg
	if effective == nil {
		effective = sdktls.GetDefaultTLSConfig()
	}

	log.Info("Effective TLS configuration",
		"minVersion", sdktls.VersionToString(effective.MinVersion),
		"cipherSuites", sdktls.CipherSuitesToString(effective.CipherSuites),
	)

	return effective
}

// resolveTLSConfig determines the TLS configuration to apply to the metrics and webhook servers,
// in order of precedence:
//  1. Explicit --tls-min-version/--tls-cipher-suites flags.
//  2. The ocm-tls-profile ConfigMap in the controller's own namespace, if kubeClient and
//     namespace are available and RBAC permits list/watch on it. This also starts a background
//     watcher that invokes onConfigMapChange when the ConfigMap's data changes. If the ConfigMap
//     doesn't exist yet, sdktls.StartTLSConfigMapWatcher still returns a non-nil TLSConfig (Go's
//     TLS 1.2 defaults) rather than nil, since it needs something concrete to seed the watcher's
//     change detection. If RBAC denies list/watch, returns an error instead of starting the
//     watcher so the caller falls back to defaults - see canWatchTLSProfileConfigMap.
//  3. If kubeClient or namespace is unavailable (e.g. not running in a cluster), returns
//     (nil, nil) so the caller falls back to a TLS 1.2 floor with Go's default cipher suites.
func resolveTLSConfig(
	ctx context.Context, minVersion, cipherSuites string, kubeClient kubernetes.Interface, namespace string,
	onConfigMapChange func(),
) (*sdktls.TLSConfig, error) {
	flagCfg, err := sdktls.ConfigFromFlags(minVersion, cipherSuites)
	if err != nil {
		return nil, err
	}

	if flagCfg != nil {
		return flagCfg, nil
	}

	if kubeClient == nil || namespace == "" {
		return nil, nil
	}

	allowed, err := canWatchTLSProfileConfigMap(ctx, kubeClient, namespace)
	if err != nil {
		return nil, err
	}

	if !allowed {
		return nil, fmt.Errorf(
			"missing permission to list/watch the %s ConfigMap in namespace %s", sdktls.ConfigMapName, namespace,
		)
	}

	return sdktls.StartTLSConfigMapWatcher(ctx, kubeClient, namespace, onConfigMapChange)
}

// canWatchTLSProfileConfigMap checks list/watch access to the ocm-tls-profile ConfigMap via
// SelfSubjectAccessReview, which needs no RBAC of its own, instead of finding out the hard way
// from sdktls.StartTLSConfigMapWatcher (which hangs rather than erroring on missing RBAC).
func canWatchTLSProfileConfigMap(ctx context.Context, kubeClient kubernetes.Interface, namespace string) (bool, error) {
	for _, verb := range []string{"list", "watch"} {
		review := &authorizationv1.SelfSubjectAccessReview{
			Spec: authorizationv1.SelfSubjectAccessReviewSpec{
				ResourceAttributes: &authorizationv1.ResourceAttributes{
					Namespace: namespace,
					Verb:      verb,
					Resource:  "configmaps",
					Name:      sdktls.ConfigMapName,
				},
			},
		}

		reviewCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()

		result, err := kubeClient.AuthorizationV1().SelfSubjectAccessReviews().Create(
			reviewCtx, review, metav1.CreateOptions{})
		if err != nil {
			return false, fmt.Errorf("failed to check %s permission on the %s ConfigMap: %w",
				verb, sdktls.ConfigMapName, err)
		}

		if !result.Status.Allowed {
			return false, nil
		}
	}

	return true, nil
}
