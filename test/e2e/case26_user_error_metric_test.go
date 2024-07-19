package e2e

import (
	"fmt"
	"strconv"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"open-cluster-management.io/config-policy-controller/test/utils"
)

var _ = Describe("Test related object metrics", Ordered, func() {
	const (
		policy1Name   = "case26-test-policy-1"
		configmapName = "case26-configmap"
		policyYaml    = "../resources/case26_user_error_metric/case26-missing-crd.yaml"
	)
	cleanup := func() {
		utils.KubectlDelete("-f", policyYaml, "-n", testNamespace)

		By("Check configmap removed")
		utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
			configmapName, "default", false, defaultTimeoutSeconds)

		By("Checking metric endpoint for related object gauges")
		Eventually(func() interface{} {
			return utils.GetMetrics("policy_user_errors", fmt.Sprintf(`template=\"%s\"`, policy1Name))
		}, defaultTimeoutSeconds, 1).Should(Equal([]string{}))
	}

	It("should create policy", func() {
		By("Creating " + policyYaml)
		utils.Kubectl("apply",
			"-f", policyYaml,
			"-n", testNamespace)
		By("Verifying the policies were created")
		plc1 := utils.GetWithTimeout(
			clientManagedDynamic, gvrConfigPolicy, policy1Name, testNamespace, true, defaultTimeoutSeconds,
		)
		Expect(plc1).NotTo(BeNil())
	})

	It("should correctly report no CRD user error", func() {
		By("Checking metric endpoint for user error gauge for policy " + policy1Name)
		Eventually(func(g Gomega) {
			rv := utils.GetMetrics(
				"policy_user_errors",
				fmt.Sprintf(`template=\"%s\"`, policy1Name),
			)
			g.Expect(rv).To(HaveLen(1))

			value, err := strconv.ParseInt(rv[0], 10, 64)
			g.Expect(err).ToNot(HaveOccurred())
			// It should retry every 10 seconds so the counter will increase. This verifies that
			// at least one retry occurred.
			g.Expect(value).To(BeNumerically(">", 1))
		}, defaultTimeoutSeconds, 1).Should(Succeed())
	})

	AfterAll(cleanup)
})
