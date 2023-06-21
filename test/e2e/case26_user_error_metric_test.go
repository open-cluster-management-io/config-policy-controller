package e2e

import (
	"fmt"
	"os/exec"

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
		// Delete the policies and ignore any errors (in case it was deleted previously)
		cmd := exec.Command("kubectl", "delete",
			"-f", policyYaml,
			"-n", testNamespace, "--ignore-not-found")
		_, _ = cmd.CombinedOutput()

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
		Eventually(func() interface{} {
			return utils.GetMetrics(
				"policy_user_errors",
				fmt.Sprintf(`template=\"%s\"`, policy1Name),
			)
		}, defaultTimeoutSeconds, 1).Should(Equal([]string{"1"}))
	})

	AfterAll(cleanup)
})
