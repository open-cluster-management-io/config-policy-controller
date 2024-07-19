// Copyright Contributors to the Open Cluster Management project

package e2e

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"open-cluster-management.io/config-policy-controller/test/utils"
)

var _ = Describe("Test resource creation when there are empty labels in configurationPolicy", Ordered, func() {
	const (
		case36AddEmptyMap     = "../resources/case36_empty_map/policy-should-add-empty-map.yaml"
		case36AddEmptyMapName = "case36-check-selinux-options"
		case36NoEmptyMap      = "../resources/case36_empty_map/policy-should-not-add-empty-map.yaml"
		case36NoEmptyMapName  = "case36-check-node-selector"
		case36Deployment      = "../resources/case36_empty_map/deployment.yaml"
		case36DeploymentName  = "case36-deployment"
	)

	BeforeEach(func() {
		By("creating the deployment " + case36DeploymentName)
		utils.Kubectl("apply", "-f", case36Deployment)
	})

	AfterEach(func() {
		By("deleting the deployment " + case36DeploymentName)
		utils.KubectlDelete("-f", case36Deployment)
		deleteConfigPolicies([]string{case36AddEmptyMapName, case36NoEmptyMapName})
	})

	It("verifies the policy "+case36AddEmptyMapName+"is NonCompliant", func() {
		By("creating the policy " + case36AddEmptyMapName)
		utils.Kubectl("apply", "-f", case36AddEmptyMap, "-n", testNamespace)

		By("checking if the policy " + case36AddEmptyMapName + " is NonCompliant")
		Eventually(func(g Gomega) {
			managedPlc := utils.GetWithTimeout(
				clientManagedDynamic,
				gvrConfigPolicy,
				case36AddEmptyMapName,
				testNamespace,
				true,
				defaultTimeoutSeconds,
			)

			utils.CheckComplianceStatus(g, managedPlc, "NonCompliant")
		}, defaultTimeoutSeconds, 1).Should(Succeed())
	})

	It("verifies the policy "+case36NoEmptyMapName+"is Compliant", func() {
		By("creating the policy " + case36NoEmptyMapName)
		utils.Kubectl("apply", "-f", case36NoEmptyMap, "-n", testNamespace)

		By("checking if the policy " + case36NoEmptyMapName + " is Compliant")
		Eventually(func(g Gomega) {
			managedPlc := utils.GetWithTimeout(
				clientManagedDynamic,
				gvrConfigPolicy,
				case36NoEmptyMapName,
				testNamespace,
				true,
				defaultTimeoutSeconds,
			)

			utils.CheckComplianceStatus(g, managedPlc, "Compliant")
		}, defaultTimeoutSeconds, 1).Should(Succeed())
	})
})
