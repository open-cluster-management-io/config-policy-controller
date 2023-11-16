// Copyright (c) 2021 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package controllers

import (
	"context"
	"encoding/base64"
	"fmt"

	"github.com/stolostron/go-template-utils/v5/pkg/templates"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	policyv1 "open-cluster-management.io/config-policy-controller/api/v1"
)

const IVAnnotation = "policy.open-cluster-management.io/encryption-iv"

// getEncryptionKey will get the encryption key in the managed cluster namespace used for policy template encryption.
func (r *ConfigurationPolicyReconciler) getEncryptionKey(namespace string) (*cachedEncryptionKey, error) {
	// #nosec G101
	const secretName = "policy-encryption-key"

	ctx := context.TODO()
	objectKey := types.NamespacedName{
		Name:      secretName,
		Namespace: namespace,
	}
	encryptionSecret := &corev1.Secret{}

	err := r.Get(ctx, objectKey, encryptionSecret)
	if err != nil {
		return nil, fmt.Errorf("failed to get the encryption key from Secret %s/%s: %w", namespace, secretName, err)
	}

	var key []byte
	if len(encryptionSecret.Data["key"]) > 0 {
		key = encryptionSecret.Data["key"]
	}

	var previousKey []byte
	if len(encryptionSecret.Data["previousKey"]) > 0 {
		previousKey = encryptionSecret.Data["previousKey"]
	}

	cachedKey := &cachedEncryptionKey{
		key:         key,
		previousKey: previousKey,
	}

	return cachedKey, nil
}

// getEncryptionConfig returns the encryption config for decrypting values that were encrypted using Hub templates.
// The returned bool determines if the cache was used. This value can be helpful to know if the cache should be
// refreshed if the decryption failed. The forceRefresh argument will skip the cache and update it with what the
// API returns.
func (r *ConfigurationPolicyReconciler) getEncryptionConfig(policy policyv1.ConfigurationPolicy, forceRefresh bool) (
	templates.EncryptionConfig, bool, error,
) {
	log := log.WithValues("policy", policy.GetName())
	usedCache := false

	annotations := policy.GetAnnotations()
	ivBase64 := annotations[IVAnnotation]

	iv, err := base64.StdEncoding.DecodeString(ivBase64)
	if err != nil {
		err = fmt.Errorf(`the policy annotation of "%s" is not Base64: %w`, IVAnnotation, err)

		return templates.EncryptionConfig{}, usedCache, err
	}

	if r.cachedEncryptionKey == nil || forceRefresh {
		r.cachedEncryptionKey = &cachedEncryptionKey{}
	}

	if r.cachedEncryptionKey.key == nil {
		log.V(2).Info(
			"The encryption key is not cached, getting the encryption key from the server for the EncryptionConfig " +
				"object",
		)

		var err error

		// The managed cluster namespace will be the same namespace as the ConfigurationPolicy
		r.cachedEncryptionKey, err = r.getEncryptionKey(policy.GetNamespace())
		if err != nil {
			return templates.EncryptionConfig{}, usedCache, err
		}
	} else {
		log.V(2).Info("Using the cached encryption key for the EncryptionConfig object")
		usedCache = true
	}

	encryptionConfig := templates.EncryptionConfig{
		AESKey:                r.cachedEncryptionKey.key,
		AESKeyFallback:        r.cachedEncryptionKey.previousKey,
		DecryptionConcurrency: r.DecryptionConcurrency,
		DecryptionEnabled:     true,
		InitializationVector:  iv,
	}

	return encryptionConfig, usedCache, nil
}

// usesEncryption detects if the initialization vector is set on the policy. If it is, then there
// are encrypted strings that need to be decrypted.
func usesEncryption(policy policyv1.ConfigurationPolicy) bool {
	return policy.GetAnnotations()[IVAnnotation] != ""
}
