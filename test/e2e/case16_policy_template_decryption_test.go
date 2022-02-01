// Copyright (c) 2021 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package e2e

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"open-cluster-management.io/config-policy-controller/test/utils"
)

const (
	case16CreatedNamespace    = "my-ns"
	case16Policy              = "../resources/case16_policy_template_decryption/policy.yaml"
	case16PolicyDiffKey       = "../resources/case16_policy_template_decryption/policy-diff-key.yaml"
	case16PolicyDiffKeyName   = "policy-namespace-create-diff-key"
	case16PolicyInvalidIV     = "../resources/case16_policy_template_decryption/policy-invalid-iv.yaml"
	case16PolicyInvalidIVName = "policy-namespace-create-invalid-iv"
	// Use a separate policy for this which contains an encrypted string that will force the cache to be refreshed
	case16PolicyInvalidKey       = "../resources/case16_policy_template_decryption/policy-invalid-key.yaml"
	case16PolicyInvalidKeyName   = "policy-namespace-create-invalid-key"
	case16PolicyName             = "policy-namespace-create"
	case16SecondCreatedNamespace = "my-second-ns"
	case16Secret                 = "../resources/case16_policy_template_decryption/secret.yaml"
	case16SecretInvalid          = "../resources/case16_policy_template_decryption/secret-invalid.yaml"
	case16SecretDiffKey          = "../resources/case16_policy_template_decryption/secret-diff-key.yaml"
)

var _ = Describe("Test policy template decryption", func() {
	It("deletes the namespace "+case16CreatedNamespace, func() {
		utils.Kubectl("delete", "namespace", case16CreatedNamespace, "--ignore-not-found")
	})

	It("creates the policy-encryption-key secret in "+testNamespace, func() {
		utils.Kubectl("apply", "-f", case16Secret, "-n", testNamespace)
	})

	It("creates the policy "+case16PolicyName+" in "+testNamespace, func() {
		utils.Kubectl("apply", "-f", case16Policy, "-n", testNamespace)
	})

	It("verifies the policy "+case16PolicyName+" in "+testNamespace, func() {
		Eventually(func() interface{} {
			managedPlc := utils.GetWithTimeout(
				clientManagedDynamic, gvrConfigPolicy, case16PolicyName, testNamespace, true, defaultTimeoutSeconds,
			)

			return utils.GetComplianceState(managedPlc)
		}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
	})

	It("simulates a key rotation", func() {
		utils.Kubectl("apply", "-f", case16SecretDiffKey, "-n", testNamespace)
	})

	It("Creates the policy "+case16PolicyDiffKeyName+" in "+testNamespace+" that uses the new key", func() {
		utils.Kubectl("apply", "-f", case16PolicyDiffKey, "-n", testNamespace)
	})

	It("verifies the policy "+case16PolicyDiffKeyName+" in "+testNamespace, func() {
		Eventually(func() interface{} {
			managedPlc := utils.GetWithTimeout(
				clientManagedDynamic,
				gvrConfigPolicy,
				case16PolicyDiffKeyName,
				testNamespace,
				true,
				defaultTimeoutSeconds,
			)

			return utils.GetComplianceState(managedPlc)
		}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
	})

	It("verifies that the policy "+case16PolicyName+" in "+testNamespace+" is still compliant", func() {
		Eventually(func() interface{} {
			managedPlc := utils.GetWithTimeout(
				clientManagedDynamic,
				gvrConfigPolicy,
				case16PolicyName,
				testNamespace,
				true,
				defaultTimeoutSeconds,
			)

			return utils.GetComplianceState(managedPlc)
		}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
	})

	It("cleans up", func() {
		utils.Kubectl("delete", "-f", case16Policy, "-n", testNamespace)
		utils.Kubectl("delete", "-f", case16PolicyDiffKey, "-n", testNamespace)
		utils.Kubectl("delete", "-f", case16Secret, "-n", testNamespace)
		utils.Kubectl("delete", "namespace", case16CreatedNamespace, "--ignore-not-found")
		utils.Kubectl("delete", "namespace", case16SecondCreatedNamespace, "--ignore-not-found")
	})
})

var _ = Describe("Test policy template decryption with an invalid AES key", func() {
	It("creates the invalid policy-encryption-key secret in "+testNamespace, func() {
		utils.Kubectl("apply", "-f", case16SecretInvalid, "-n", testNamespace)
	})

	It("creates the policy "+case16PolicyInvalidKeyName+" in "+testNamespace, func() {
		utils.Kubectl("apply", "-f", case16PolicyInvalidKey, "-n", testNamespace)
	})

	It("verifies the policy "+case16PolicyInvalidKeyName+" in "+testNamespace+" is noncompliant", func() {
		Eventually(func() interface{} {
			managedPlc := utils.GetWithTimeout(
				clientManagedDynamic,
				gvrConfigPolicy,
				case16PolicyInvalidKeyName,
				testNamespace,
				true,
				defaultTimeoutSeconds,
			)

			return utils.GetStatusMessage(managedPlc)
		}, defaultTimeoutSeconds, 1).Should(Equal(`The "policy-encryption-key" Secret contains an invalid AES key`))
	})

	It("cleans up", func() {
		utils.Kubectl("delete", "-f", case16PolicyInvalidKey, "-n", testNamespace)
		utils.Kubectl("delete", "-f", case16Secret, "-n", testNamespace)
	})
})

var _ = Describe("Test policy template decryption with an invalid initialization vector", func() {
	It("creates the policy-encryption-key secret in "+testNamespace, func() {
		utils.Kubectl("apply", "-f", case16Secret, "-n", testNamespace)
	})

	It("creates the policy "+case16PolicyInvalidIVName+" in "+testNamespace, func() {
		utils.Kubectl("apply", "-f", case16PolicyInvalidIV, "-n", testNamespace)
	})

	It("verifies the policy "+case16PolicyInvalidIVName+" in "+testNamespace+" is noncompliant", func() {
		expected := `The "policy.open-cluster-management.io/encryption-iv" annotation value is not a valid ` +
			"initialization vector"
		Eventually(func() interface{} {
			managedPlc := utils.GetWithTimeout(
				clientManagedDynamic,
				gvrConfigPolicy,
				case16PolicyInvalidIVName,
				testNamespace,
				true,
				defaultTimeoutSeconds,
			)

			return utils.GetStatusMessage(managedPlc)
		}, defaultTimeoutSeconds, 1).Should(Equal(expected))
	})

	It("cleans up", func() {
		utils.Kubectl("delete", "-f", case16PolicyInvalidIV, "-n", testNamespace)
		utils.Kubectl("delete", "-f", case16Secret, "-n", testNamespace)
	})
})
