// Copyright (c) 2021 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package controllers

import (
	"context"
	"crypto/rand"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	policyv1 "open-cluster-management.io/config-policy-controller/api/v1"
)

const (
	clusterName = "local-cluster"
	IV          = "SUlJSUlJSUlJSUlJSUlJSQ=="
	keySize     = 256
	secretName  = "policy-encryption-key"
)

func getReconciler(includeSecret bool) ConfigurationPolicyReconciler {
	var client client.WithWatch

	if includeSecret {
		// Generate AES-256 keys and store them as a secret.
		key := make([]byte, keySize/8)
		_, err := rand.Read(key)
		Expect(err).ToNot(HaveOccurred())

		previousKey := make([]byte, keySize/8)
		_, err = rand.Read(previousKey)
		Expect(err).ToNot(HaveOccurred())

		encryptionSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      secretName,
				Namespace: clusterName,
			},
			Data: map[string][]byte{
				"key":         key,
				"previousKey": previousKey,
			},
		}

		client = fake.NewClientBuilder().WithObjects(encryptionSecret).Build()
	} else {
		client = fake.NewClientBuilder().Build()
	}

	return ConfigurationPolicyReconciler{Client: client, DecryptionConcurrency: 5}
}

func getEmptyPolicy() policyv1.ConfigurationPolicy {
	return policyv1.ConfigurationPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "my-policy",
			Namespace:   "local-cluster",
			Annotations: map[string]string{IVAnnotation: IV},
		},
	}
}

func TestGetEncryptionConfig(t *testing.T) {
	t.Parallel()
	RegisterFailHandler(Fail)

	r := getReconciler(true)

	policy := getEmptyPolicy()

	config, err := r.getEncryptionConfig(context.TODO(), &policy)
	Expect(err).ToNot(HaveOccurred())
	Expect(config).ToNot(BeNil())
	Expect(config.AESKey).ToNot(BeNil())
	Expect(config.AESKeyFallback).ToNot(BeNil())
	Expect(config.DecryptionEnabled).To(BeTrue())
	Expect(config.DecryptionConcurrency).To(Equal(uint8(5)))
	Expect(config.EncryptionEnabled).To(BeFalse())
	Expect(config.InitializationVector).ToNot(BeNil())
}

func TestGetEncryptionConfigInvalidIV(t *testing.T) {
	t.Parallel()
	RegisterFailHandler(Fail)

	r := getReconciler(true)

	policy := policyv1.ConfigurationPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "my-policy",
			Namespace:   "local-cluster",
			Annotations: map[string]string{IVAnnotation: "😱😱😱😱"},
		},
	}

	_, err := r.getEncryptionConfig(context.TODO(), &policy)
	Expect(err.Error()).To(
		Equal(
			"the policy annotation of \"policy.open-cluster-management.io/encryption-iv\" is not Base64: illegal " +
				"base64 data at input byte 0",
		),
	)
}

func TestGetEncryptionConfigNoSecret(t *testing.T) {
	t.Parallel()
	RegisterFailHandler(Fail)

	r := getReconciler(false)

	policy := getEmptyPolicy()

	_, err := r.getEncryptionConfig(context.TODO(), &policy)
	Expect(err.Error()).To(
		Equal(
			`failed to get the encryption key from Secret local-cluster/policy-encryption-key: secrets ` +
				`"policy-encryption-key" not found`,
		),
	)
}

func TestUsesEncryption(t *testing.T) {
	t.Parallel()
	RegisterFailHandler(Fail)

	policy := getEmptyPolicy()
	rv := usesEncryption(&policy)
	Expect(rv).To(BeTrue())
}

func TestUsesEncryptionNoIV(t *testing.T) {
	t.Parallel()
	RegisterFailHandler(Fail)

	policy := policyv1.ConfigurationPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-policy",
			Namespace: "local-cluster",
		},
	}
	rv := usesEncryption(&policy)
	Expect(rv).To(BeFalse())
}
