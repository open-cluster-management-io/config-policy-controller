// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package e2e

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"open-cluster-management.io/config-policy-controller/test/utils"
)

const (
	case3ConfigPolicyNameCSV     string = "policy-imagemanifestvulnpolicy-example-csv"
	case3ConfigPolicyNameSub     string = "policy-imagemanifestvulnpolicy-example-sub"
	case3ConfigPolicyNameVuln    string = "policy-imagemanifestvulnpolicy-example-imv"
	case3ConfigPolicyNameVulnObj string = "policy-imagemanifestvulnpolicy-example-imv-obj"
	case3PolicyYamlCSV           string = "../resources/case3_imgvuln/case3_csv.yaml"
	case3PolicyYamlSub           string = "../resources/case3_imgvuln/case3_subscription.yaml"
	case3PolicyYamlVuln          string = "../resources/case3_imgvuln/case3_vuln.yaml"
	case3PolicyYamlVulnObj       string = "../resources/case3_imgvuln/case3_vuln_object.yaml"
)

var _ = Describe("Test img vulnerability obj template handling", func() {
	Describe("Create a clusterserviceversion on managed cluster in ns:"+testNamespace, Ordered, func() {
		It("should be created properly on the managed cluster", func() {
			By("Creating " + case3ConfigPolicyNameCSV + " on managed")
			utils.Kubectl("apply", "-f", case3PolicyYamlCSV, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case3ConfigPolicyNameCSV, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case3ConfigPolicyNameCSV, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("NonCompliant"))

			deleteConfigPolicies([]string{case3ConfigPolicyNameCSV})
		})
		It("should check for a subscription on managed cluster", func() {
			By("Creating " + case3ConfigPolicyNameSub + " on managed")
			utils.Kubectl("apply", "-f", case3PolicyYamlSub, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case3ConfigPolicyNameSub, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case3ConfigPolicyNameSub, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("NonCompliant"))

			deleteConfigPolicies([]string{case3ConfigPolicyNameSub})
		})
		It("should be noncompliant for no CRD found (kind)", func() {
			By("Creating " + case3ConfigPolicyNameVuln + " on managed")
			utils.Kubectl("apply", "-f", case3PolicyYamlVuln, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case3ConfigPolicyNameVuln, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case3ConfigPolicyNameVuln, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("NonCompliant"))
			Consistently(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case3ConfigPolicyNameVuln, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, 20, 1).Should(Equal("NonCompliant"))

			deleteConfigPolicies([]string{case3ConfigPolicyNameVuln})
		})
		It("should be noncompliant for no CRD found (object)", func() {
			By("Creating " + case3ConfigPolicyNameVulnObj + " on managed")
			utils.Kubectl("apply", "-f", case3PolicyYamlVulnObj, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case3ConfigPolicyNameVulnObj, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case3ConfigPolicyNameVulnObj, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("NonCompliant"))
			Consistently(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case3ConfigPolicyNameVulnObj, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, 20, 1).Should(Equal("NonCompliant"))

			deleteConfigPolicies([]string{case3ConfigPolicyNameVulnObj})
		})
	})
})
