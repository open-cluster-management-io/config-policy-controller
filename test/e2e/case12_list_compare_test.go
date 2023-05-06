// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package e2e

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"open-cluster-management.io/config-policy-controller/test/utils"
)

const (
	case12ConfigPolicyNameInform  string = "policy-pod-mh-listinform"
	case12ConfigPolicyNameEnforce string = "policy-pod-create-listinspec"
	case12InformYaml              string = "../resources/case12_list_compare/case12_pod_inform.yaml"
	case12EnforceYaml             string = "../resources/case12_list_compare/case12_pod_create.yaml"
)

const (
	case12ConfigPolicyNameRoleInform  string = "policy-role-mh-listinform"
	case12ConfigPolicyNameRoleEnforce string = "policy-role-create-listinspec"
	case12RoleInformYaml              string = "../resources/case12_list_compare/case12_role_inform.yaml"
	case12RoleEnforceYaml             string = "../resources/case12_list_compare/case12_role_create.yaml"
)

const (
	case12RoleToPatch          string = "topatch-role-configpolicy"
	case12RoleToPatchYaml      string = "../resources/case12_list_compare/case12_role_create_small.yaml"
	case12RolePatchEnforce     string = "patch-role-configpolicy"
	case12RolePatchEnforceYaml string = "../resources/case12_list_compare/case12_role_patch.yaml"
	case12RolePatchInform      string = "patch-role-configpolicy-inform"
	case12RolePatchInformYaml  string = "../resources/case12_list_compare/case12_role_patch_inform.yaml"
)

const (
	case12OauthCreate     string = "policy-idp-create"
	case12OauthPatch      string = "policy-idp-patch"
	case12OauthVerify     string = "policy-idp-verify"
	case12OauthCreateYaml string = "../resources/case12_list_compare/case12_oauth_create.yaml"
	case12OauthPatchYaml  string = "../resources/case12_list_compare/case12_oauth_patch.yaml"
	case12OauthVerifyYaml string = "../resources/case12_list_compare/case12_oauth_verify.yaml"
)

const (
	case12SingleItemListCreate     string = "policy-htpasswd-single"
	case12SingleItemListPatch      string = "policy-htpasswd-single"
	case12SingleItemListInform     string = "policy-htpasswd-single-inform"
	case12SingleItemListCreateYaml string = "../resources/case12_list_compare/case12_oauth_single_create.yaml"
	case12SingleItemListPatchYaml  string = "../resources/case12_list_compare/case12_oauth_single_patch.yaml"
	case12SingleItemListInformYaml string = "../resources/case12_list_compare/case12_oauth_single_inform.yaml"
)

const (
	case12SmallerListExistingCreate     string = "policy-htpasswd-less"
	case12SmallerListExistingPatch      string = "policy-htpasswd-less"
	case12SmallerListExistingInform     string = "policy-htpasswd-less-inform"
	case12SmallerListExistingCreateYaml string = "../resources/case12_list_compare/case12_oauth_less_create.yaml"
	case12SmallerListExistingPatchYaml  string = "../resources/case12_list_compare/case12_oauth_less_patch.yaml"
	case12SmallerListExistingInformYaml string = "../resources/case12_list_compare/case12_oauth_less_inform.yaml"
)

const (
	case12WhitespaceListCreate     string = "policy-pod-whitespace-env"
	case12WhitespaceListInform     string = "policy-pod-whitespace-env-inform"
	case12WhitespaceListCreateYaml string = "../resources/case12_list_compare/case12_whitespace_create.yaml"
	case12WhitespaceDeployment     string = "envvar-whitespace"
)

const (
	case12ByteCreate     string = "policy-byte-create"
	case12ByteCreateYaml string = "../resources/case12_list_compare/case12_byte_create.yaml"
	case12ByteInform     string = "policy-byte-inform"
	case12ByteInformYaml string = "../resources/case12_list_compare/case12_byte_inform.yaml"
)

var _ = Describe("Test list handling for musthave", func() {
	Describe("Create a policy with a nested list on managed cluster in ns:"+testNamespace, func() {
		It("should be created properly on the managed cluster", func() {
			By("Creating " + case12ConfigPolicyNameEnforce + " and " + case12ConfigPolicyNameInform + " on managed")
			utils.Kubectl("apply", "-f", case12EnforceYaml, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case12ConfigPolicyNameEnforce, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case12ConfigPolicyNameEnforce, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
			utils.Kubectl("apply", "-f", case12InformYaml, "-n", testNamespace)
			plc = utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case12ConfigPolicyNameInform, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case12ConfigPolicyNameInform, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
		})
		It("Cleans up", func() {
			policies := []string{
				case12ConfigPolicyNameInform,
				case12ConfigPolicyNameEnforce,
			}

			deleteConfigPolicies(policies)
		})
	})
	Describe("Create a policy with a list field on managed cluster in ns:"+testNamespace, func() {
		It("should be created properly on the managed cluster", func() {
			By("Creating " + case12ConfigPolicyNameRoleEnforce + " and " +
				case12ConfigPolicyNameRoleInform + " on managed")
			utils.Kubectl("apply", "-f", case12RoleEnforceYaml, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case12ConfigPolicyNameRoleEnforce, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case12ConfigPolicyNameRoleEnforce, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
			utils.Kubectl("apply", "-f", case12RoleInformYaml, "-n", testNamespace)
			plc = utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case12ConfigPolicyNameRoleInform, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case12ConfigPolicyNameRoleInform, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("NonCompliant"))
		})
		It("Cleans up", func() {
			policies := []string{
				case12ConfigPolicyNameRoleInform,
				case12ConfigPolicyNameRoleEnforce,
			}

			deleteConfigPolicies(policies)
		})
	})
	Describe("Create and patch a role on managed cluster in ns:"+testNamespace, func() {
		It("should be created properly on the managed cluster", func() {
			By("Creating " + case12RoleToPatch + " and " + case12RolePatchEnforce + " on managed")
			utils.Kubectl("apply", "-f", case12RoleToPatchYaml, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case12RoleToPatch, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case12RoleToPatch, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
			utils.Kubectl("apply", "-f", case12RolePatchEnforceYaml, "-n", testNamespace)
			plc = utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case12RolePatchEnforce, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case12RolePatchEnforce, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
			utils.Kubectl("apply", "-f", case12RolePatchInformYaml, "-n", testNamespace)
			plc = utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case12RolePatchInform, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case12RolePatchInform, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
		})
		It("Cleans up", func() {
			policies := []string{
				case12RoleToPatch,
				case12RolePatchEnforce,
				case12RolePatchInform,
			}

			deleteConfigPolicies(policies)
		})
	})
	Describe("Create and patch an oauth object on managed cluster in ns:"+testNamespace, func() {
		It("should be created properly on the managed cluster", func() {
			By("Creating " + case12OauthCreate + " and " + case12OauthPatch + " on managed")
			utils.Kubectl("apply", "-f", case12OauthCreateYaml, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case12OauthCreate, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case12OauthCreate, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
			utils.Kubectl("delete", "-f", case12OauthCreateYaml, "-n", testNamespace)

			utils.Kubectl("apply", "-f", case12OauthPatchYaml, "-n", testNamespace)
			plc = utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case12OauthPatch, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case12OauthPatch, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
			utils.Kubectl("apply", "-f", case12OauthVerifyYaml, "-n", testNamespace)
			plc = utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case12OauthVerify, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case12OauthVerify, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
		})

		It("should handle lists with just one object properly on the managed cluster", func() {
			By("Creating " + case12SingleItemListCreate + " and " + case12SingleItemListPatch + " on managed")
			utils.Kubectl("apply", "-f", case12SingleItemListCreateYaml, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case12SingleItemListCreate, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case12SingleItemListCreate, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))

			utils.Kubectl("apply", "-f", case12SingleItemListPatchYaml, "-n", testNamespace)
			plc = utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case12SingleItemListPatch, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case12SingleItemListPatch, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
			utils.Kubectl("apply", "-f", case12SingleItemListInformYaml, "-n", testNamespace)
			plc = utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case12SingleItemListInform, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case12SingleItemListInform, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
		})

		It("should handle lists with fewer items in existing than the template", func() {
			By("Creating " + case12SmallerListExistingCreate + " and " + case12SmallerListExistingPatch + " on managed")
			utils.Kubectl("apply", "-f", case12SmallerListExistingCreateYaml, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case12SmallerListExistingCreate, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case12SmallerListExistingCreate, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))

			utils.Kubectl("apply", "-f", case12SmallerListExistingPatchYaml, "-n", testNamespace)
			plc = utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case12SmallerListExistingPatch, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case12SmallerListExistingPatch, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
			utils.Kubectl("apply", "-f", case12SmallerListExistingInformYaml, "-n", testNamespace)
			plc = utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case12SmallerListExistingInform, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case12SmallerListExistingInform, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
		})

		It("Cleans up", func() {
			policies := []string{
				case12OauthCreate,
				case12OauthPatch,
				case12OauthVerify,
				case12SingleItemListCreate,
				case12SingleItemListPatch,
				case12SingleItemListInform,
				case12SmallerListExistingCreate,
				case12SmallerListExistingPatch,
				case12SmallerListExistingInform,
			}

			deleteConfigPolicies(policies)
		})
	})
	Describe("Create a deployment object with env vars on managed cluster in ns:"+testNamespace, func() {
		It("should only add the list item with prefix and suffix whitespace once", func() {
			By("Creating " + case12WhitespaceListCreate + " and " + case12WhitespaceListInform + " on managed")
			utils.Kubectl("apply", "-f", case12WhitespaceListCreateYaml, "-n", testNamespace)

			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case12WhitespaceListCreate, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case12WhitespaceListCreate, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
			// Ensure it remains compliant for a while - need to ensure there were multiple enforce checks/attempts.
			Consistently(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case12WhitespaceListCreate, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, time.Second*20, 1).Should(Equal("Compliant"))

			// Verify that the container list and its environment variable list is correct (there are no duplicates)
			deploy := utils.GetWithTimeout(clientManagedDynamic, gvrDeployment,
				case12WhitespaceDeployment, "default", true, defaultTimeoutSeconds)
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

		It("Cleans up", func() {
			policies := []string{
				case12WhitespaceListCreate,
				case12WhitespaceListInform,
			}

			deleteConfigPolicies(policies)
		})
	})
	Describe("Create a statefulset object with a byte quantity field "+
		"on managed cluster in ns:"+testNamespace, Ordered, func() {
		cleanup := func() {
			// Delete the policies and ignore any errors (in case it was deleted previously)
			policies := []string{
				case12ByteCreate,
				case12ByteInform,
			}

			deleteConfigPolicies(policies)
		}
		It("should only add the list item with the rounded byte value once", func() {
			By("Creating " + case12ByteCreate + " and " + case12ByteInform + " on managed")
			utils.Kubectl("apply", "-f", case12ByteCreateYaml, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case12ByteCreate, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case12ByteCreate, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
			// Ensure it remains compliant for a while - need to ensure there were multiple enforce checks/attempts.
			Consistently(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case12ByteCreate, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, time.Second*20, 1).Should(Equal("Compliant"))

			// Verify that the container list and its environment variable list is correct (there are no duplicates)
			utils.Kubectl("apply", "-f", case12ByteInformYaml, "-n", testNamespace)
			plc = utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case12ByteInform, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case12ByteInform, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
		})
		AfterAll(cleanup)
	})
})
