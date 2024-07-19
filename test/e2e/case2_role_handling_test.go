// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package e2e

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"open-cluster-management.io/config-policy-controller/test/utils"
)

var _ = Describe("Test role obj template handling", Ordered, func() {
	Describe("Create a policy on managed cluster in ns:"+testNamespace, Ordered, func() {
		const (
			resourcePrefix                 string = "../resources/case2_role_handling/"
			configPolicyNameInform         string = "policy-role-create-inform"
			configPolicyNameEnforce        string = "policy-role-create"
			roleName                       string = "pod-reader-e2e"
			policyYamlInform               string = resourcePrefix + "case2_role_create_inform.yaml"
			policyYamlEnforce              string = resourcePrefix + "case2_role_create_enforce.yaml"
			policyCheckMNHYaml             string = resourcePrefix + "case2_role_check-mnh.yaml"
			policyCheckMOHYaml             string = resourcePrefix + "case2_role_check-moh.yaml"
			policyCheckCompliant           string = resourcePrefix + "case2_role_check-c.yaml"
			configPolicyNameBindingEnforce string = "policy-rolebinding-create"
			bindingName                    string = "pod-reader-e2e-binding"
			policyYamlBindingEnforce       string = resourcePrefix + "case2_rolebinding_create_enforce.yaml"
			policyYamlBindingPatch         string = resourcePrefix + "case2_rolebinding_create_patch.yaml"
		)

		AfterAll(func() {
			By("clean up case2")
			policies := []string{
				configPolicyNameInform,
				configPolicyNameEnforce,
				"policy-role-check-comp",
				"policy-role-check-mnh",
				"policy-role-check-moh",
				configPolicyNameBindingEnforce,
			}
			deleteConfigPolicies(policies)
			utils.KubectlDelete("role", roleName, "-n", "default")
			utils.KubectlDelete("rolebinding", bindingName, "-n", "default")
		})

		It("should be created properly on the managed cluster", func() {
			By("Creating " + policyYamlInform + " on managed")
			utils.Kubectl("apply", "-f", policyYamlInform, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				configPolicyNameInform, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					configPolicyNameInform, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("NonCompliant"))
		})

		It("should create role on managed cluster", func() {
			By("creating " + policyYamlEnforce + " on hub with spec.remediationAction = enforce")
			utils.Kubectl("apply", "-f", policyYamlEnforce, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				configPolicyNameEnforce, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					configPolicyNameEnforce, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
			Eventually(func() interface{} {
				informPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					configPolicyNameInform, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(informPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
			role := utils.GetWithTimeout(clientManagedDynamic, gvrRole, roleName,
				"default", true, defaultTimeoutSeconds)
			Expect(role).NotTo(BeNil())
		})

		It("should create statuses properly", func() {
			utils.Kubectl("apply", "-f", policyCheckMNHYaml, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				"policy-role-check-mnh", testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					"policy-role-check-mnh", testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("NonCompliant"))
			utils.Kubectl("apply", "-f", policyCheckMOHYaml, "-n", testNamespace)
			plc = utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				"policy-role-check-moh", testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					"policy-role-check-moh", testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("NonCompliant"))
			utils.Kubectl("apply", "-f", policyCheckCompliant, "-n", testNamespace)
			plc = utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				"policy-role-check-comp", testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					"policy-role-check-comp", testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
		})

		It("should create rolebinding on managed cluster", func() {
			By("creating " + policyYamlBindingEnforce + " on hub with spec.remediationAction = enforce")
			utils.Kubectl("apply", "-f", policyYamlBindingEnforce, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				configPolicyNameBindingEnforce, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					configPolicyNameBindingEnforce, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))

			Eventually(func() interface{} {
				informPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					configPolicyNameBindingEnforce, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(informPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
			binding := utils.GetWithTimeout(clientManagedDynamic, gvrRoleBinding, bindingName,
				"default", true, defaultTimeoutSeconds)
			Expect(binding).NotTo(BeNil())
			subjects, _, err := unstructured.NestedSlice(binding.Object, "subjects")
			Expect(err).ToNot(HaveOccurred())
			Expect(subjects).To(HaveLen(1))
		})

		It("should patch the rolebinding on managed cluster to an empty subjects", func() {
			By("creating " + policyYamlBindingPatch + " on hub with spec.remediationAction = inform")
			utils.Kubectl("apply", "-f", policyYamlBindingPatch, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				configPolicyNameBindingEnforce, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())

			var managedPlc *unstructured.Unstructured

			Eventually(func() interface{} {
				managedPlc = utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					configPolicyNameBindingEnforce, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("NonCompliant"))

			By("Verifying the diff in the status")
			relatedObjects, _, err := unstructured.NestedSlice(managedPlc.Object, "status", "relatedObjects")
			Expect(err).ToNot(HaveOccurred())
			Expect(relatedObjects).To(HaveLen(1))

			uid, _, _ := unstructured.NestedString(relatedObjects[0].(map[string]interface{}), "properties", "uid")
			Expect(uid).ToNot(BeEmpty())

			diff, _, _ := unstructured.NestedString(relatedObjects[0].(map[string]interface{}), "properties", "diff")
			expectedDiff := fmt.Sprintf(`--- default/pod-reader-e2e-binding : existing
+++ default/pod-reader-e2e-binding : updated
@@ -8,10 +8,6 @@
   uid: %s
 roleRef:
   apiGroup: rbac.authorization.k8s.io
   kind: Role
   name: pod-reader-e2e
-subjects:
-- apiGroup: rbac.authorization.k8s.io
-  kind: Group
-  name: system:authenticated:oauth
 
`, uid)
			Expect(diff).To(Equal(expectedDiff))

			By("patching policy spec.remediationAction = enforce")
			utils.Kubectl("patch", "configurationpolicy", configPolicyNameBindingEnforce, `--type=json`,
				`-p=[{"op":"replace","path":"/spec/remediationAction","value":"enforce"}]`, "-n", testNamespace)
			Eventually(func() interface{} {
				informPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					configPolicyNameBindingEnforce, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(informPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
			binding := utils.GetWithTimeout(clientManagedDynamic, gvrRoleBinding, bindingName,
				"default", true, defaultTimeoutSeconds)
			Expect(binding).NotTo(BeNil())
			subjects, _, err := unstructured.NestedSlice(binding.Object, "subjects")
			Expect(err).ToNot(HaveOccurred())
			Expect(subjects).To(BeNil())
		})
	})
})
