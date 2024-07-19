// Copyright Contributors to the Open Cluster Management project

package e2e

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"open-cluster-management.io/config-policy-controller/test/utils"
)

var _ = Describe("Test inform policies on ClusterRoles (RHBZ#2130985)", Ordered, func() {
	const (
		policyName = "case24-role-handling"
		policyYAML = "../resources/case24_role_handling/policy.yaml"
		rolesYAML  = "../resources/case24_role_handling/roles.yaml"
	)

	BeforeEach(func() {
		By("Creating the roles")
		utils.Kubectl("apply", "-f", rolesYAML)
	})

	AfterEach(func() {
		By("Deleting the " + policyName + " policy")
		deleteConfigPolicies([]string{policyName})

		By("Deleting the roles")
		utils.KubectlDelete("-f", rolesYAML)
	})

	It("verifies an inform policy with the created ClusterRoles ", func() {
		By("Creating the " + policyName + " policy")
		utils.Kubectl("apply", "-f", policyYAML, "-n", testNamespace)

		By("Verifying that the " + policyName + " policy is compliant")
		Eventually(func() interface{} {
			managedPlc := utils.GetWithTimeout(
				clientManagedDynamic, gvrConfigPolicy, policyName, testNamespace, true, defaultTimeoutSeconds,
			)

			return utils.GetComplianceState(managedPlc)
		}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
	})
})
