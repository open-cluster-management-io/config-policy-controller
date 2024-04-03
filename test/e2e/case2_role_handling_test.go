// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package e2e

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

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

		AfterAll(func() {
			By("clean up case2")
			policies := []string{
				configPolicyNameInform,
				configPolicyNameEnforce,
				"policy-role-check-comp",
				"policy-role-check-mnh",
				"policy-role-check-moh",
			}
			deleteConfigPolicies(policies)
			utils.Kubectl("delete", "role", roleName, "-n", "default", "--ignore-not-found")
		})

		It("should be created properly on the managed cluster", func() {
			By("Creating " + policyYamlInform + " on managed")
			utils.Kubectl("apply", "-f", policyYamlInform, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				configPolicyNameInform, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case2ConfigPolicyNameInform, testNamespace, true, defaultTimeoutSeconds)

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

		})
	})
})
