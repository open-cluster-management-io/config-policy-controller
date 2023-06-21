// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package e2e

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"open-cluster-management.io/config-policy-controller/test/utils"
)

const (
	case2ConfigPolicyNameInform  string = "policy-role-create-inform"
	case2ConfigPolicyNameEnforce string = "policy-role-create"
	case2roleName                string = "pod-reader-e2e"
	case2PolicyYamlInform        string = "../resources/case2_role_handling/case2_role_create_inform.yaml"
	case2PolicyYamlEnforce       string = "../resources/case2_role_handling/case2_role_create_enforce.yaml"
	case2PolicyCheckMNHYaml      string = "../resources/case2_role_handling/case2_role_check-mnh.yaml"
	case2PolicyCheckMOHYaml      string = "../resources/case2_role_handling/case2_role_check-moh.yaml"
	case2PolicyCheckCompliant    string = "../resources/case2_role_handling/case2_role_check-c.yaml"
)

var _ = Describe("Test role obj template handling", Ordered, func() {
	Describe("Create a policy on managed cluster in ns:"+testNamespace, Ordered, func() {
		It("should be created properly on the managed cluster", func() {
			By("Creating " + case2PolicyYamlInform + " on managed")
			utils.Kubectl("apply", "-f", case2PolicyYamlInform, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case2ConfigPolicyNameInform, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case2ConfigPolicyNameInform, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("NonCompliant"))
		})
		It("should create role on managed cluster", func() {
			By("creating " + case2PolicyYamlEnforce + " on hub with spec.remediationAction = enforce")
			utils.Kubectl("apply", "-f", case2PolicyYamlEnforce, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case2ConfigPolicyNameEnforce, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case2ConfigPolicyNameEnforce, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
			Eventually(func() interface{} {
				informPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case2ConfigPolicyNameInform, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(informPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
			role := utils.GetWithTimeout(clientManagedDynamic, gvrRole, case2roleName,
				"default", true, defaultTimeoutSeconds)
			Expect(role).NotTo(BeNil())
		})
		It("should create statuses properly", func() {
			utils.Kubectl("apply", "-f", case2PolicyCheckMNHYaml, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				"policy-role-check-mnh", testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					"policy-role-check-mnh", testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("NonCompliant"))
			utils.Kubectl("apply", "-f", case2PolicyCheckMOHYaml, "-n", testNamespace)
			plc = utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				"policy-role-check-moh", testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					"policy-role-check-moh", testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("NonCompliant"))
			utils.Kubectl("apply", "-f", case2PolicyCheckCompliant, "-n", testNamespace)
			plc = utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				"policy-role-check-comp", testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					"policy-role-check-comp", testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
		})
		AfterAll(func() {
			By("clean up case2")
			policies := []string{
				case2ConfigPolicyNameInform,
				case2ConfigPolicyNameEnforce,
				"policy-role-check-comp",
				"policy-role-check-mnh",
				"policy-role-check-moh",
			}
			deleteConfigPolicies(policies)
			utils.Kubectl("delete", "role", "pod-reader-e2e", "-n", "default", "--ignore-not-found")
		})
	})
})
