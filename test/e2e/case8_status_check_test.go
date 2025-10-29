// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package e2e

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"open-cluster-management.io/config-policy-controller/test/utils"
)

var _ = Describe("Test pod obj template handling", func() {
	const (
		case8ConfigPolicyNamePod         string = "policy-pod-to-check"
		case8ConfigPolicyNameCheck       string = "policy-status-checker"
		case8ConfigPolicyNameCheckFail   string = "policy-status-checker-fail"
		case8ConfigPolicyNameEnforceFail string = "policy-status-enforce-fail"
		case8PolicyYamlPod               string = "../resources/case8_status_check/case8_pod.yaml"
		case8PolicyYamlCheck             string = "../resources/case8_status_check/case8_status_check.yaml"
		case8PolicyYamlCheckFail         string = "../resources/case8_status_check/case8_status_check_fail.yaml"
		case8PolicyYamlEnforceFail       string = "../resources/case8_status_check/case8_status_enforce_fail.yaml"
		case8ConfigPolicyStatusPod       string = "policy-pod-invalid"
		case8PolicyYamlBadPod            string = "../resources/case8_status_check/case8_pod_fail.yaml"
		case8PolicyYamlSpecChange        string = "../resources/case8_status_check/case8_pod_change.yaml"
	)

	Describe("Create a policy on managed cluster in ns:"+testNamespace, Ordered, func() {
		It("should create a policy properly on the managed cluster", func() {
			By("Creating " + case8ConfigPolicyNamePod + " on managed")
			utils.Kubectl("apply", "-f", case8PolicyYamlPod, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case8ConfigPolicyNamePod, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func(g Gomega) {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case8ConfigPolicyNamePod, testNamespace, true, defaultTimeoutSeconds)

				utils.CheckComplianceStatus(g, managedPlc, "Compliant")
			}, defaultTimeoutSeconds, 1).Should(Succeed())
		})
		It("should check status of the created policy", func() {
			By("Creating " + case8ConfigPolicyNameCheck + " on managed")
			utils.Kubectl("apply", "-f", case8PolicyYamlCheck, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case8ConfigPolicyNameCheck, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func(g Gomega) {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case8ConfigPolicyNameCheck, testNamespace, true, defaultTimeoutSeconds)

				utils.CheckComplianceStatus(g, managedPlc, "Compliant")
			}, defaultTimeoutSeconds, 1).Should(Succeed())
		})
		It("should return nonCompliant if status does not match", func() {
			By("Creating " + case8ConfigPolicyNameCheckFail + " on managed")
			utils.Kubectl("apply", "-f", case8PolicyYamlCheckFail, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case8ConfigPolicyNameCheckFail, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())

			var managedPlc *unstructured.Unstructured

			Eventually(func(g Gomega) {
				managedPlc = utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case8ConfigPolicyNameCheckFail, testNamespace, true, defaultTimeoutSeconds)

				utils.CheckComplianceStatus(g, managedPlc, "NonCompliant")
			}, defaultTimeoutSeconds, 1).Should(Succeed())

			relatedObjects, _, err := unstructured.NestedSlice(managedPlc.Object, "status", "relatedObjects")
			Expect(err).ToNot(HaveOccurred())
			Expect(relatedObjects).To(HaveLen(1))

			diff, _, _ := unstructured.NestedString(relatedObjects[0].(map[string]interface{}), "properties", "diff")
			Expect(diff).To(ContainSubstring("-  compliant: Compliant\n+  compliant: NonCompliant"))
		})
		It("should return nonCompliant if status does not match (enforce)", func() {
			By("Creating " + case8ConfigPolicyNameEnforceFail + " on managed")
			utils.Kubectl("apply", "-f", case8PolicyYamlEnforceFail, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case8ConfigPolicyNameEnforceFail, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())

			var managedPlc *unstructured.Unstructured

			Eventually(func(g Gomega) {
				managedPlc = utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case8ConfigPolicyNameEnforceFail, testNamespace, true, defaultTimeoutSeconds)

				utils.CheckComplianceStatus(g, managedPlc, "NonCompliant")
			}, defaultTimeoutSeconds, 1).Should(Succeed())

			relatedObjects, _, err := unstructured.NestedSlice(managedPlc.Object, "status", "relatedObjects")
			Expect(err).ToNot(HaveOccurred())
			Expect(relatedObjects).To(HaveLen(1))

			diff, _, _ := unstructured.NestedString(relatedObjects[0].(map[string]interface{}), "properties", "diff")
			Expect(diff).To(ContainSubstring("-  compliant: Compliant\n+  compliant: NonCompliant"))
		})
		AfterAll(func() {
			policies := []string{
				case8ConfigPolicyNameCheck,
				case8ConfigPolicyNameCheckFail,
				case8ConfigPolicyNameEnforceFail,
				case8ConfigPolicyNamePod,
			}

			deleteConfigPolicies(policies)
		})
	})
	Describe("Create a policy with status on managed cluster in ns:"+testNamespace, Ordered, func() {
		It("should create a policy properly on the managed cluster", func() {
			By("Creating " + case8ConfigPolicyStatusPod + " on managed")
			utils.Kubectl("apply", "-f", case8PolicyYamlBadPod, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case8ConfigPolicyStatusPod, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func(g Gomega) {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case8ConfigPolicyStatusPod, testNamespace, true, defaultTimeoutSeconds)

				utils.CheckComplianceStatus(g, managedPlc, "NonCompliant")
			}, defaultTimeoutSeconds, 1).Should(Succeed())
			Eventually(func() interface{} {
				pod := utils.GetWithTimeout(clientManagedDynamic, gvrPod,
					"nginx-badpod-e2e-8", "default", true, defaultTimeoutSeconds)

				return pod.Object["status"].(map[string]interface{})["phase"]
			}, defaultTimeoutSeconds, 1).Should(Equal("Pending"))
		})
		It("should be able to apply spec change and status does not interfere", func() {
			By("Merging change to " + case8ConfigPolicyStatusPod + " on managed")
			utils.Kubectl("apply", "-f", case8PolicyYamlSpecChange, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case8ConfigPolicyStatusPod, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func(g Gomega) {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case8ConfigPolicyStatusPod, testNamespace, true, defaultTimeoutSeconds)

				utils.CheckComplianceStatus(g, managedPlc, "NonCompliant")
			}, defaultTimeoutSeconds, 1).Should(Succeed())
			Eventually(func() interface{} {
				pod := utils.GetWithTimeout(clientManagedDynamic, gvrPod,
					"nginx-badpod-e2e-8", "default", true, defaultTimeoutSeconds)

				return pod.Object["spec"].(map[string]interface{})["activeDeadlineSeconds"]
			}, defaultTimeoutSeconds, 1).Should(Equal(int64(10)))
			Eventually(func() interface{} {
				pod := utils.GetWithTimeout(clientManagedDynamic, gvrPod,
					"nginx-badpod-e2e-8", "default", true, defaultTimeoutSeconds)

				return pod.Object["status"].(map[string]interface{})["phase"]
			}, defaultTimeoutSeconds, 1).Should(Equal("Failed"))
			Eventually(func(g Gomega) {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case8ConfigPolicyStatusPod, testNamespace, true, defaultTimeoutSeconds)

				utils.CheckComplianceStatus(g, managedPlc, "Compliant")
			}, defaultTimeoutSeconds, 1).Should(Succeed())
		})
		AfterAll(func() {
			policies := []string{
				case8ConfigPolicyStatusPod,
			}
			deleteConfigPolicies(policies)

			utils.KubectlDelete("pod", "nginx-badpod-e2e-8", "-n", "default")
		})
	})
})

var _ = Describe("Test related object property status", Ordered, func() {
	Describe("Create a policy missing a field added by kubernetes", Ordered, func() {
		const (
			policyName  = "policy-service"
			serviceName = "grc-policy-propagator-metrics"
			policyYAML  = "../resources/case8_status_check/case8_service_inform.yaml"
			serviceYAML = "../resources/case8_status_check/case8_service.yaml"
		)

		It("Should be compliant when the inform policy omits a field added by the apiserver", func() {
			By("Creating a Service with explicit type: ClusterIP")
			utils.Kubectl("apply", "-f", serviceYAML)

			By("Creating the " + policyName + " policy that omits the type field)")
			utils.Kubectl("apply", "-f", policyYAML, "-n", testNamespace)

			By("Verifying that the " + policyName + " policy is compliant")
			Eventually(func(g Gomega) {
				managedPlc := utils.GetWithTimeout(
					clientManagedDynamic, gvrConfigPolicy, policyName, testNamespace, true, defaultTimeoutSeconds,
				)

				utils.CheckComplianceStatus(g, managedPlc, "Compliant")

				relatedObjects, _, err := unstructured.NestedSlice(managedPlc.Object, "status", "relatedObjects")
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(relatedObjects).To(HaveLen(1))

				relatedObj := relatedObjects[0].(map[string]interface{})
				matchesAfterDryRun, _, _ := unstructured.NestedBool(relatedObj, "properties", "matchesAfterDryRun")

				g.Expect(matchesAfterDryRun).To(BeTrue())

				history, _, err := unstructured.NestedSlice(managedPlc.Object, "status", "history")
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(history).To(HaveLen(1))
			}, defaultTimeoutSeconds, 1).Should(Succeed())
		})

		It("Should be compliant when the enforce policy omits a field added by the apiserver", func() {
			By("Changing the policy to enforce mode")
			utils.EnforceConfigurationPolicy(policyName, testNamespace)

			By("Verifying that the " + policyName + " policy is compliant")
			Eventually(func(g Gomega) {
				managedPlc := utils.GetWithTimeout(
					clientManagedDynamic, gvrConfigPolicy, policyName, testNamespace, true, defaultTimeoutSeconds,
				)

				utils.CheckComplianceStatus(g, managedPlc, "Compliant")

				relatedObjects, _, err := unstructured.NestedSlice(managedPlc.Object, "status", "relatedObjects")
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(relatedObjects).To(HaveLen(1))

				relatedObj := relatedObjects[0].(map[string]interface{})
				matchesAfterDryRun, _, _ := unstructured.NestedBool(relatedObj, "properties", "matchesAfterDryRun")

				g.Expect(matchesAfterDryRun).To(BeTrue())

				history, _, err := unstructured.NestedSlice(managedPlc.Object, "status", "history")
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(history).To(HaveLen(2))
			}, defaultTimeoutSeconds, 1).Should(Succeed())
		})

		It("Should be compliant when the enforce policy includes all fields", func() {
			By("Patching the " + policyName + " policy with the Service type field)")
			utils.Kubectl("patch", "configurationpolicy", policyName, "-n", testNamespace, "--type=json", "-p",
				`[{"op": "add", "path": "/spec/object-templates/0/objectDefinition/spec/type", "value": "ClusterIP"}]`)

			By("Verifying that the " + policyName + " policy is compliant")
			Eventually(func(g Gomega) {
				managedPlc := utils.GetWithTimeout(
					clientManagedDynamic, gvrConfigPolicy, policyName, testNamespace, true, defaultTimeoutSeconds,
				)

				utils.CheckComplianceStatus(g, managedPlc, "Compliant")

				relatedObjects, _, err := unstructured.NestedSlice(managedPlc.Object, "status", "relatedObjects")
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(relatedObjects).To(HaveLen(1))

				relatedObj := relatedObjects[0].(map[string]interface{})
				matchesAfterDryRun, _, _ := unstructured.NestedBool(relatedObj, "properties", "matchesAfterDryRun")

				g.Expect(matchesAfterDryRun).To(BeFalse())

				history, _, err := unstructured.NestedSlice(managedPlc.Object, "status", "history")
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(history).To(HaveLen(3))
			}, defaultTimeoutSeconds, 1).Should(Succeed())
		})

		It("Should be compliant when the inform policy includes all fields", func() {
			By("Changing the policy to inform mode")
			utils.Kubectl("patch", "configurationpolicy", policyName, `--type=json`,
				`-p=[{"op":"replace","path":"/spec/remediationAction","value":"inform"}]`, "-n", testNamespace)

			By("Verifying that the " + policyName + " policy is compliant")
			Eventually(func(g Gomega) {
				managedPlc := utils.GetWithTimeout(
					clientManagedDynamic, gvrConfigPolicy, policyName, testNamespace, true, defaultTimeoutSeconds,
				)

				utils.CheckComplianceStatus(g, managedPlc, "Compliant")

				relatedObjects, _, err := unstructured.NestedSlice(managedPlc.Object, "status", "relatedObjects")
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(relatedObjects).To(HaveLen(1))

				relatedObj := relatedObjects[0].(map[string]interface{})
				matchesAfterDryRun, _, _ := unstructured.NestedBool(relatedObj, "properties", "matchesAfterDryRun")

				g.Expect(matchesAfterDryRun).To(BeFalse())

				history, _, err := unstructured.NestedSlice(managedPlc.Object, "status", "history")
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(history).To(HaveLen(4))
			}, defaultTimeoutSeconds, 1).Should(Succeed())
		})

		AfterAll(func() {
			deleteConfigPolicies([]string{policyName, policyName})

			utils.KubectlDelete("service", serviceName, "-n", "managed")
		})
	})
})
