// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package e2e

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"open-cluster-management.io/config-policy-controller/test/utils"
)

const (
	case6ConfigPolicyNameNS    string = "case6-policy-ns"
	case6ConfigPolicyNameRole  string = "case6-role-policy-no-ns"
	case6ConfigPolicyNameCombo string = "case6-policy-combo-no-ns"
	case6ComboRole             string = "case6-role-policy-e2e2"
	case6NSName1               string = "case6-e2etest"
	case6NSName2               string = "case6-e2etest2"
	case6NSYaml                string = "../resources/case6_no_ns/case6_create_ns.yaml"
	case6RoleYaml              string = "../resources/case6_no_ns/case6_create_role.yaml"
	case6ComboYaml             string = "../resources/case6_no_ns/case6_combo.yaml"
)

var _ = Describe("Test multiple obj template handling", func() {
	Describe("Create a policy on managed cluster in ns:"+testNamespace, func() {
		It("should create a violation if the object should be namespaced", func() {
			By("Creating policies on managed")
			utils.Kubectl("apply", "-f", case6RoleYaml, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case6ConfigPolicyNameRole, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case6ConfigPolicyNameRole, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("NonCompliant"))

			By("Clean up")
			policies := []string{
				case6ConfigPolicyNameRole,
			}

			deleteConfigPolicies(policies)
		})
		It("should create pods on managed cluster", func() {
			By("creating cluster level objects")
			utils.Kubectl("apply", "-f", case6NSYaml, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case6ConfigPolicyNameNS, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case6ConfigPolicyNameNS, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
			utils.Kubectl("apply", "-f", case6ComboYaml, "-n", testNamespace)
			plc = utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case6ConfigPolicyNameCombo, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				comboPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case6ConfigPolicyNameCombo, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(comboPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("NonCompliant"))
			ns1 := utils.GetClusterLevelWithTimeout(clientManagedDynamic, gvrNS,
				case6NSName1, true, defaultTimeoutSeconds)
			Expect(ns1).NotTo(BeNil())
			ns2 := utils.GetClusterLevelWithTimeout(clientManagedDynamic, gvrNS,
				case6NSName2, true, defaultTimeoutSeconds)
			Expect(ns2).NotTo(BeNil())

			By("Clean up")
			policies := []string{
				case6ConfigPolicyNameNS,
				case6ConfigPolicyNameCombo,
			}

			deleteConfigPolicies(policies)

			utils.KubectlDelete("ns", case6NSName1)
			utils.KubectlDelete("ns", case6NSName2)
			utils.KubectlDelete("role", case6ComboRole, "-n", "default")
		})
	})
})
