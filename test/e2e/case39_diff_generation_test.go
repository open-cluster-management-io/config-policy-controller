// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package e2e

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"open-cluster-management.io/config-policy-controller/test/utils"
)

var _ = Describe("Generate the diff", Ordered, func() {
	const (
		logPath          string = "../../build/_output/controller.log"
		configPolicyName string = "case39-policy-cfgmap-create"
		createYaml       string = "../resources/case39_diff_generation/case39-create-cfgmap-policy.yaml"
		updateYaml       string = "../resources/case39_diff_generation/case39-update-cfgmap-policy.yaml"
	)

	BeforeAll(func() {
		_, err := os.Stat(logPath)
		if err != nil {
			Skip(fmt.Sprintf("Skipping. Failed to find log file %s: %s", logPath, err.Error()))
		}
	})

	It("configmap should be created properly on the managed cluster", func() {
		By("Creating " + configPolicyName + " on managed")
		utils.Kubectl("apply", "-f", createYaml, "-n", testNamespace)
		plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
			configPolicyName, testNamespace, true, defaultTimeoutSeconds)
		Expect(plc).NotTo(BeNil())
		Eventually(func() interface{} {
			managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				configPolicyName, testNamespace, true, defaultTimeoutSeconds)

			return utils.GetStatusMessage(managedPlc)
		}, 120, 1).Should(Equal("configmaps [case39-map] found as specified in namespace default"))
	})

	It("configmap and status should be updated properly on the managed cluster", func() {
		By("Updating " + configPolicyName + " on managed")
		utils.Kubectl("apply", "-f", updateYaml, "-n", testNamespace)
		Eventually(func() interface{} {
			managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				configPolicyName, testNamespace, true, defaultTimeoutSeconds)

			return utils.GetStatusMessage(managedPlc)
		}, 30, 0.5).Should(Equal("configmaps [case39-map] was updated successfully in namespace default"))
	})

	It("diff should be logged by the controller", func() {
		By("Checking the controller logs")
		logFile, err := os.Open(logPath)
		Expect(err).ToNot(HaveOccurred())
		defer logFile.Close()

		diff := ""
		foundDiff := false
		logScanner := bufio.NewScanner(logFile)
		logScanner.Split(bufio.ScanLines)
		for logScanner.Scan() {
			line := logScanner.Text()
			if foundDiff && strings.HasPrefix(line, "\t{") {
				foundDiff = false
			} else if foundDiff || strings.Contains(line, "Logging the diff:") {
				foundDiff = true
			} else {
				continue
			}

			diff += line + "\n"
		}

		Expect(diff).Should(ContainSubstring(`Logging the diff:
--- default/case39-map : existing
+++ default/case39-map : updated
@@ -1,8 +1,8 @@
 apiVersion: v1
 data:
-  fieldToUpdate: "1"
+  fieldToUpdate: "2"
 kind: ConfigMap`))

		Expect(diff).Should(ContainSubstring(
			`{"policy": "case39-policy-cfgmap-create", "name": "case39-map", "namespace": "default", ` +
				`"resource": "configmaps"}`,
		))
	})

	AfterAll(func() {
		deleteConfigPolicies([]string{configPolicyName})
		utils.Kubectl("delete", "configmap", "case39-map", "--ignore-not-found")
	})
})

var _ = Describe("Diff generation with sensitive input", Ordered, func() {
	const (
		noDiffObjTemplatesRaw     = "case39-no-diff-object-templates-raw"
		noDiffObjTemplatesRawYAML = "../resources/case39_diff_generation/case39-no-diff-object-templates-raw.yaml"
		noDiffObjTemplates        = "case39-no-diff-object-templates"
		noDiffObjTemplatesYAML    = "../resources/case39_diff_generation/case39-no-diff-object-templates.yaml"
		noDiffOnSecret            = "case39-no-diff-on-secret"
		noDiffOnSecretYAML        = "../resources/case39_diff_generation/case39-no-diff-on-secret.yaml"
		secretName                = "case39-secret"
	)

	BeforeAll(func() {
		By("Creating " + secretName + " in the default namespace")
		utils.Kubectl("create", "-f", "../resources/case39_diff_generation/case39-secret.yaml")
	})

	AfterAll(func() {
		deleteConfigPolicies([]string{noDiffObjTemplatesRaw, noDiffObjTemplates, noDiffOnSecret})
		utils.Kubectl("-n", "default", "delete", "secret", secretName, "--ignore-not-found")
	})

	It("Does not automatically generate a diff when using fromSecret (object-templates-raw)", func() {
		By("Creating " + noDiffObjTemplatesRaw + " on managed")
		utils.Kubectl("apply", "-f", noDiffObjTemplatesRawYAML, "-n", testNamespace)

		var managedPlc *unstructured.Unstructured

		Eventually(func() interface{} {
			managedPlc = utils.GetWithTimeout(
				clientManagedDynamic,
				gvrConfigPolicy,
				noDiffObjTemplatesRaw,
				testNamespace,
				true,
				defaultTimeoutSeconds,
			)

			return utils.GetComplianceState(managedPlc)
		}, defaultTimeoutSeconds, 1).Should(Equal("NonCompliant"))

		By("Verifying the diff in the status contains instructions to set recordDiff")
		relatedObjects, _, err := unstructured.NestedSlice(managedPlc.Object, "status", "relatedObjects")
		Expect(err).ToNot(HaveOccurred())
		Expect(relatedObjects).To(HaveLen(1))

		diff, _, _ := unstructured.NestedString(relatedObjects[0].(map[string]interface{}), "properties", "diff")
		Expect(diff).To(Equal(
			`# This diff may contain sensitive data. The "recordDiff" field must be set to "InStatus" ` +
				`to record a diff.`,
		))
	})

	It("Does not automatically generate a diff when using fromSecret (object-templates)", func() {
		By("Creating " + noDiffObjTemplates + " on managed")
		utils.Kubectl("apply", "-f", noDiffObjTemplatesYAML, "-n", testNamespace)

		var managedPlc *unstructured.Unstructured

		Eventually(func() interface{} {
			managedPlc = utils.GetWithTimeout(
				clientManagedDynamic,
				gvrConfigPolicy,
				noDiffObjTemplates,
				testNamespace,
				true,
				defaultTimeoutSeconds,
			)

			return utils.GetComplianceState(managedPlc)
		}, defaultTimeoutSeconds, 1).Should(Equal("NonCompliant"))

		By("Verifying the diff in the status contains instructions to set recordDiff")
		relatedObjects, _, err := unstructured.NestedSlice(managedPlc.Object, "status", "relatedObjects")
		Expect(err).ToNot(HaveOccurred())
		Expect(relatedObjects).To(HaveLen(1))

		diff, _, _ := unstructured.NestedString(relatedObjects[0].(map[string]interface{}), "properties", "diff")
		Expect(diff).To(Equal(
			`# This diff may contain sensitive data. The "recordDiff" field must be set to "InStatus" ` +
				`to record a diff.`,
		))
	})

	It("Does not automatically generate a diff when configuring a Secret", func() {
		By("Creating " + noDiffOnSecret + " on managed")
		utils.Kubectl("apply", "-f", noDiffOnSecretYAML, "-n", testNamespace)

		var managedPlc *unstructured.Unstructured

		Eventually(func() interface{} {
			managedPlc = utils.GetWithTimeout(
				clientManagedDynamic,
				gvrConfigPolicy,
				noDiffOnSecret,
				testNamespace,
				true,
				defaultTimeoutSeconds,
			)

			return utils.GetComplianceState(managedPlc)
		}, defaultTimeoutSeconds, 1).Should(Equal("NonCompliant"))

		By("Verifying the diff in the status contains instructions to set recordDiff")
		relatedObjects, _, err := unstructured.NestedSlice(managedPlc.Object, "status", "relatedObjects")
		Expect(err).ToNot(HaveOccurred())
		Expect(relatedObjects).To(HaveLen(1))

		diff, _, _ := unstructured.NestedString(relatedObjects[0].(map[string]interface{}), "properties", "diff")
		Expect(diff).To(Equal(
			`# This diff may contain sensitive data. The "recordDiff" field must be set to "InStatus" ` +
				`to record a diff.`,
		))
	})
})

var _ = Describe("Diff generation that is truncated", Ordered, func() {
	const (
		policyTruncatedDiff     = "case39-truncated"
		policyTruncatedDiffYAML = "../resources/case39_diff_generation/case39-truncated.yaml"
	)

	AfterAll(func() {
		deleteConfigPolicies([]string{policyTruncatedDiff})
	})

	It("Does not automatically generate a diff when configuring a Secret", func() {
		By("Creating " + policyTruncatedDiff + " on managed")
		utils.Kubectl("apply", "-f", policyTruncatedDiffYAML, "-n", testNamespace)

		var managedPlc *unstructured.Unstructured

		Eventually(func() interface{} {
			managedPlc = utils.GetWithTimeout(
				clientManagedDynamic,
				gvrConfigPolicy,
				policyTruncatedDiff,
				testNamespace,
				true,
				defaultTimeoutSeconds,
			)

			return utils.GetComplianceState(managedPlc)
		}, defaultTimeoutSeconds, 1).Should(Equal("NonCompliant"))

		By("Verifying the diff in the status is truncated")
		relatedObjects, _, err := unstructured.NestedSlice(managedPlc.Object, "status", "relatedObjects")
		Expect(err).ToNot(HaveOccurred())
		Expect(relatedObjects).To(HaveLen(1))

		diff, _, _ := unstructured.NestedString(relatedObjects[0].(map[string]interface{}), "properties", "diff")
		Expect(diff).To(HavePrefix(
			"# Truncated: showing 50/68 diff lines:\n--- default : existing\n+++ default : updated",
		))
		Expect(diff).To(HaveSuffix("+    message46: message"))
	})
})
