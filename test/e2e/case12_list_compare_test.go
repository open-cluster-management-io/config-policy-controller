// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package e2e

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"open-cluster-management.io/config-policy-controller/test/utils"
)

var _ = Describe("Test list handling for musthave", func() {
	const (
		configPolicyNameInform  string = "policy-pod-mh-listinform"
		configPolicyNameEnforce string = "policy-pod-create-listinspec"
		podName                 string = "nginx-pod-e2e-12"
		informYaml              string = "../resources/case12_list_compare/case12_pod_inform.yaml"
		enforceYaml             string = "../resources/case12_list_compare/case12_pod_create.yaml"
	)

	Describe("Create a policy with a nested list on managed cluster in ns:"+testNamespace, Ordered, func() {
		It("should be created properly on the managed cluster", func() {
			By("Creating " + configPolicyNameEnforce + " and " + configPolicyNameInform + " on managed")
			utils.Kubectl("apply", "-f", enforceYaml, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				configPolicyNameEnforce, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func(g Gomega) {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					configPolicyNameEnforce, testNamespace, true, defaultTimeoutSeconds)

				utils.CheckComplianceStatus(g, managedPlc, "Compliant")
			}, defaultTimeoutSeconds, 1).Should(Succeed())
			utils.Kubectl("apply", "-f", informYaml, "-n", testNamespace)
			plc = utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				configPolicyNameInform, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func(g Gomega) {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					configPolicyNameInform, testNamespace, true, defaultTimeoutSeconds)

				utils.CheckComplianceStatus(g, managedPlc, "Compliant")
			}, defaultTimeoutSeconds, 1).Should(Succeed())
		})
		AfterAll(func() {
			policies := []string{
				configPolicyNameInform,
				configPolicyNameEnforce,
			}

			deleteConfigPolicies(policies)

			utils.KubectlDelete("pod", podName, "-n", "default")
		})
	})

	Describe("Create a policy with a list field on managed cluster in ns:"+testNamespace, Ordered, func() {
		const (
			configPolicyNameRoleInform  string = "policy-role-mh-listinform"
			configPolicyNameRoleEnforce string = "policy-role-create-listinspec"
			roleInformYaml              string = "../resources/case12_list_compare/case12_role_inform.yaml"
			roleEnforceYaml             string = "../resources/case12_list_compare/case12_role_create.yaml"
		)

		It("should be created properly on the managed cluster", func() {
			By("Creating " + configPolicyNameRoleEnforce + " and " +
				configPolicyNameRoleInform + " on managed")
			utils.Kubectl("apply", "-f", roleEnforceYaml, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				configPolicyNameRoleEnforce, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func(g Gomega) {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					configPolicyNameRoleEnforce, testNamespace, true, defaultTimeoutSeconds)

				utils.CheckComplianceStatus(g, managedPlc, "Compliant")
			}, defaultTimeoutSeconds, 1).Should(Succeed())
			utils.Kubectl("apply", "-f", roleInformYaml, "-n", testNamespace)
			plc = utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				configPolicyNameRoleInform, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func(g Gomega) {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					configPolicyNameRoleInform, testNamespace, true, defaultTimeoutSeconds)

				utils.CheckComplianceStatus(g, managedPlc, "NonCompliant")
			}, defaultTimeoutSeconds, 1).Should(Succeed())
		})
		AfterAll(func() {
			policies := []string{
				configPolicyNameRoleInform,
				configPolicyNameRoleEnforce,
			}

			deleteConfigPolicies(policies)
		})
	})
	Describe("Create and patch a role on managed cluster in ns:"+testNamespace, Ordered, func() {
		const (
			roleToPatch          string = "topatch-role-configpolicy"
			roleToPatchYaml      string = "../resources/case12_list_compare/case12_role_create_small.yaml"
			rolePatchEnforce     string = "patch-role-configpolicy"
			rolePatchEnforceYaml string = "../resources/case12_list_compare/case12_role_patch.yaml"
			rolePatchInform      string = "patch-role-configpolicy-inform"
			rolePatchInformYaml  string = "../resources/case12_list_compare/case12_role_patch_inform.yaml"
		)

		It("should be created properly on the managed cluster", func() {
			By("Creating " + roleToPatch + " and " + rolePatchEnforce + " on managed")
			utils.Kubectl("apply", "-f", roleToPatchYaml, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				roleToPatch, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func(g Gomega) {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					roleToPatch, testNamespace, true, defaultTimeoutSeconds)

				utils.CheckComplianceStatus(g, managedPlc, "Compliant")
			}, defaultTimeoutSeconds, 1).Should(Succeed())
			utils.Kubectl("apply", "-f", rolePatchEnforceYaml, "-n", testNamespace)
			plc = utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				rolePatchEnforce, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func(g Gomega) {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					rolePatchEnforce, testNamespace, true, defaultTimeoutSeconds)

				utils.CheckComplianceStatus(g, managedPlc, "Compliant")
			}, defaultTimeoutSeconds, 1).Should(Succeed())
			utils.Kubectl("apply", "-f", rolePatchInformYaml, "-n", testNamespace)
			plc = utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				rolePatchInform, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func(g Gomega) {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					rolePatchInform, testNamespace, true, defaultTimeoutSeconds)

				utils.CheckComplianceStatus(g, managedPlc, "Compliant")
			}, defaultTimeoutSeconds, 1).Should(Succeed())
		})
		AfterAll(func() {
			policies := []string{
				roleToPatch,
				rolePatchEnforce,
				rolePatchInform,
			}

			deleteConfigPolicies(policies)
		})
	})
	Describe("Create and patch an oauth object on managed cluster in ns:"+testNamespace, Ordered, func() {
		const (
			oauthCreate     string = "policy-idp-create"
			oauthPatch      string = "policy-idp-patch"
			oauthVerify     string = "policy-idp-verify"
			oauthCreateYaml string = "../resources/case12_list_compare/case12_oauth_create.yaml"
			oauthPatchYaml  string = "../resources/case12_list_compare/case12_oauth_patch.yaml"
			oauthVerifyYaml string = "../resources/case12_list_compare/case12_oauth_verify.yaml"
		)

		const (
			singleItemListCreate     string = "policy-htpasswd-single"
			singleItemListPatch      string = "policy-htpasswd-single"
			singleItemListInform     string = "policy-htpasswd-single-inform"
			singleItemListCreateYaml string = "../resources/case12_list_compare/case12_oauth_single_create.yaml"
			singleItemListPatchYaml  string = "../resources/case12_list_compare/case12_oauth_single_patch.yaml"
			singleItemListInformYaml string = "../resources/case12_list_compare/case12_oauth_single_inform.yaml"
		)

		const (
			smallerListExistingCreate     string = "policy-htpasswd-less"
			smallerListExistingPatch      string = "policy-htpasswd-less"
			smallerListExistingInform     string = "policy-htpasswd-less-inform"
			smallerListExistingCreateYaml string = "../resources/case12_list_compare/case12_oauth_less_create.yaml"
			smallerListExistingPatchYaml  string = "../resources/case12_list_compare/case12_oauth_less_patch.yaml"
			smallerListExistingInformYaml string = "../resources/case12_list_compare/case12_oauth_less_inform.yaml"
		)

		It("should be created properly on the managed cluster", func() {
			By("Creating " + oauthCreate + " and " + oauthPatch + " on managed")
			utils.Kubectl("apply", "-f", oauthCreateYaml, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				oauthCreate, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func(g Gomega) {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					oauthCreate, testNamespace, true, defaultTimeoutSeconds)

				utils.CheckComplianceStatus(g, managedPlc, "Compliant")
			}, defaultTimeoutSeconds, 1).Should(Succeed())
			utils.KubectlDelete("-f", oauthCreateYaml, "-n", testNamespace)

			utils.Kubectl("apply", "-f", oauthPatchYaml, "-n", testNamespace)
			plc = utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				oauthPatch, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func(g Gomega) {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					oauthPatch, testNamespace, true, defaultTimeoutSeconds)

				utils.CheckComplianceStatus(g, managedPlc, "Compliant")
			}, defaultTimeoutSeconds, 1).Should(Succeed())
			utils.Kubectl("apply", "-f", oauthVerifyYaml, "-n", testNamespace)
			plc = utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				oauthVerify, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func(g Gomega) {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					oauthVerify, testNamespace, true, defaultTimeoutSeconds)

				utils.CheckComplianceStatus(g, managedPlc, "Compliant")
			}, defaultTimeoutSeconds, 1).Should(Succeed())
		})

		It("should handle lists with just one object properly on the managed cluster", func() {
			By("Creating " + singleItemListCreate + " and " + singleItemListPatch + " on managed")
			utils.Kubectl("apply", "-f", singleItemListCreateYaml, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				singleItemListCreate, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func(g Gomega) {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					singleItemListCreate, testNamespace, true, defaultTimeoutSeconds)

				utils.CheckComplianceStatus(g, managedPlc, "Compliant")
			}, defaultTimeoutSeconds, 1).Should(Succeed())

			utils.Kubectl("apply", "-f", singleItemListPatchYaml, "-n", testNamespace)
			plc = utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				singleItemListPatch, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func(g Gomega) {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					singleItemListPatch, testNamespace, true, defaultTimeoutSeconds)

				utils.CheckComplianceStatus(g, managedPlc, "Compliant")
			}, defaultTimeoutSeconds, 1).Should(Succeed())
			utils.Kubectl("apply", "-f", singleItemListInformYaml, "-n", testNamespace)
			plc = utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				singleItemListInform, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func(g Gomega) {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					singleItemListInform, testNamespace, true, defaultTimeoutSeconds)

				utils.CheckComplianceStatus(g, managedPlc, "Compliant")
			}, defaultTimeoutSeconds, 1).Should(Succeed())
		})

		It("should handle lists with fewer items in existing than the template", func() {
			By("Creating " + smallerListExistingCreate + " and " + smallerListExistingPatch + " on managed")
			utils.Kubectl("apply", "-f", smallerListExistingCreateYaml, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				smallerListExistingCreate, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func(g Gomega) {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					smallerListExistingCreate, testNamespace, true, defaultTimeoutSeconds)

				utils.CheckComplianceStatus(g, managedPlc, "Compliant")
			}, defaultTimeoutSeconds, 1).Should(Succeed())

			utils.Kubectl("apply", "-f", smallerListExistingPatchYaml, "-n", testNamespace)
			plc = utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				smallerListExistingPatch, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func(g Gomega) {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					smallerListExistingPatch, testNamespace, true, defaultTimeoutSeconds)

				utils.CheckComplianceStatus(g, managedPlc, "Compliant")
			}, defaultTimeoutSeconds, 1).Should(Succeed())
			utils.Kubectl("apply", "-f", smallerListExistingInformYaml, "-n", testNamespace)
			plc = utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				smallerListExistingInform, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func(g Gomega) {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					smallerListExistingInform, testNamespace, true, defaultTimeoutSeconds)

				utils.CheckComplianceStatus(g, managedPlc, "Compliant")
			}, defaultTimeoutSeconds, 1).Should(Succeed())
		})

		AfterAll(func() {
			policies := []string{
				oauthCreate,
				oauthPatch,
				oauthVerify,
				singleItemListCreate,
				singleItemListPatch,
				singleItemListInform,
				smallerListExistingCreate,
				smallerListExistingPatch,
				smallerListExistingInform,
			}

			deleteConfigPolicies(policies)
		})
	})

	Describe("Create a deployment object with env vars on managed cluster in ns:"+testNamespace, Ordered, func() {
		const (
			whitespaceListCreate     string = "policy-pod-whitespace-env"
			whitespaceListInform     string = "policy-pod-whitespace-env-inform"
			whitespaceListCreateYaml string = "../resources/case12_list_compare/case12_whitespace_create.yaml"
			whitespaceDeployment     string = "envvar-whitespace"
		)

		It("should only add the list item with prefix and suffix whitespace once", func() {
			By("Creating " + whitespaceListCreate + " and " + whitespaceListInform + " on managed")
			utils.Kubectl("apply", "-f", whitespaceListCreateYaml, "-n", testNamespace)

			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				whitespaceListCreate, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func(g Gomega) {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					whitespaceListCreate, testNamespace, true, defaultTimeoutSeconds)

				utils.CheckComplianceStatus(g, managedPlc, "Compliant")
			}, defaultTimeoutSeconds, 1).Should(Succeed())
			// Ensure it remains compliant for a while - need to ensure there were multiple enforce checks/attempts.
			Consistently(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					whitespaceListCreate, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultConsistentlyDuration, 1).Should(Equal("Compliant"))

			// Verify that the container list and its environment variable list is correct (there are no duplicates)
			deploy := utils.GetWithTimeout(clientManagedDynamic, gvrDeployment,
				whitespaceDeployment, "default", true, defaultTimeoutSeconds)
			Expect(deploy).NotTo(BeNil())
			//nolint:forcetypeassert
			tmpl := deploy.Object["spec"].(map[string]interface{})["template"].(map[string]interface{})
			//nolint:forcetypeassert
			containers := tmpl["spec"].(map[string]interface{})["containers"].([]interface{})
			Expect(containers).To(HaveLen(1))
			//nolint:forcetypeassert
			envvars := containers[0].(map[string]interface{})["env"].([]interface{})
			Expect(envvars).To(HaveLen(1))
		})

		AfterAll(func(ctx SpecContext) {
			policies := []string{
				whitespaceListCreate,
				whitespaceListInform,
			}

			deleteConfigPolicies(policies)

			err := clientManaged.AppsV1().Deployments("default").Delete(
				ctx, whitespaceDeployment, metav1.DeleteOptions{},
			)
			if !k8serrors.IsNotFound(err) {
				Expect(err).ToNot(HaveOccurred())
			}
		})
	})
	Describe("Create a statefulset object with a byte quantity field "+
		"on managed cluster in ns:"+testNamespace, Ordered, func() {
		const (
			byteCreate     string = "policy-byte-create"
			byteCreateYaml string = "../resources/case12_list_compare/case12_byte_create.yaml"
			byteInform     string = "policy-byte-inform"
			byteInformYaml string = "../resources/case12_list_compare/case12_byte_inform.yaml"
		)

		cleanup := func() {
			// Delete the policies and ignore any errors (in case it was deleted previously)
			policies := []string{
				byteCreate,
				byteInform,
			}

			deleteConfigPolicies(policies)
		}
		It("should only add the list item with the rounded byte value once", func() {
			By("Creating " + byteCreate + " and " + byteInform + " on managed")
			utils.Kubectl("apply", "-f", byteCreateYaml, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				byteCreate, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func(g Gomega) {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					byteCreate, testNamespace, true, defaultTimeoutSeconds)

				utils.CheckComplianceStatus(g, managedPlc, "Compliant")
			}, defaultTimeoutSeconds, 1).Should(Succeed())
			// Ensure it remains compliant for a while - need to ensure there were multiple enforce checks/attempts.
			Consistently(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					byteCreate, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultConsistentlyDuration, 1).Should(Equal("Compliant"))

			// Verify that the container list and its environment variable list is correct (there are no duplicates)
			utils.Kubectl("apply", "-f", byteInformYaml, "-n", testNamespace)
			plc = utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				byteInform, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func(g Gomega) {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					byteInform, testNamespace, true, defaultTimeoutSeconds)

				utils.CheckComplianceStatus(g, managedPlc, "Compliant")
			}, defaultTimeoutSeconds, 1).Should(Succeed())
		})
		AfterAll(cleanup)
	})
})
