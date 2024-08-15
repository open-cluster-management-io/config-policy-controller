// Copyright (c) 2021 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package controllers

import (
	"context"
	"encoding/base64"
	"fmt"

	"github.com/stolostron/go-template-utils/v6/pkg/templates"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	policyv1 "open-cluster-management.io/config-policy-controller/api/v1"
)

const IVAnnotation = "policy.open-cluster-management.io/encryption-iv"

// getEncryptionConfig returns the encryption config for decrypting values that were encrypted using Hub templates.
// The returned bool determines if the cache was used. This value can be helpful to know if the cache should be
// refreshed if the decryption failed. The forceRefresh argument will skip the cache and update it with what the
// API returns.
func (r *ConfigurationPolicyReconciler) getEncryptionConfig(ctx context.Context, policy *policyv1.ConfigurationPolicy) (
	templates.EncryptionConfig, error,
) {
	annotations := policy.GetAnnotations()
	ivBase64 := annotations[IVAnnotation]

	iv, err := base64.StdEncoding.DecodeString(ivBase64)
	if err != nil {
		err = fmt.Errorf(`the policy annotation of "%s" is not Base64: %w`, IVAnnotation, err)

		return templates.EncryptionConfig{}, err
	}

	objectKey := types.NamespacedName{
		Name:      "policy-encryption-key",
		Namespace: policy.GetNamespace(),
	}

	encryptionSecret := &corev1.Secret{}

	err = r.Get(ctx, objectKey, encryptionSecret)
	if err != nil {
		return templates.EncryptionConfig{}, fmt.Errorf(
			"failed to get the encryption key from Secret %s/policy-encryption-key: %w", policy.GetNamespace(), err,
		)
	}

	encryptionConfig := templates.EncryptionConfig{
		AESKey:                encryptionSecret.Data["key"],
		AESKeyFallback:        encryptionSecret.Data["previousKey"],
		DecryptionConcurrency: r.DecryptionConcurrency,
		DecryptionEnabled:     true,
		InitializationVector:  iv,
	}

	return encryptionConfig, nil
}

// usesEncryption detects if the initialization vector is set on the policy. If it is, then there
// are encrypted strings that need to be decrypted.
func usesEncryption(policy *policyv1.ConfigurationPolicy) bool {
	return policy.GetAnnotations()[IVAnnotation] != ""
}
