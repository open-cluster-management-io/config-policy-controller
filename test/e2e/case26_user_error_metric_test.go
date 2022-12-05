package e2e

import (
	"os/exec"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"open-cluster-management.io/config-policy-controller/test/utils"
)

var _ = Describe("Test related object metrics", Ordered, func() {
	const (
		policy1Name = "case26-test-policy-1"
		policyYaml  = "../resources/case26_user_error_metric/case26-missing-crd.yaml"
	)
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
			)
		}, defaultTimeoutSeconds, 1).Should(Equal([]string{"policies", "counter", "1"}))
	})

	cleanup := func() {
		// Delete the policies and ignore any errors (in case it was deleted previously)
		cmd := exec.Command("kubectl", "delete",
			"-f", policyYaml,
			"-n", testNamespace)
		_, _ = cmd.CombinedOutput()
		opt := metav1.ListOptions{}
		utils.ListWithTimeout(
			clientManagedDynamic, gvrConfigPolicy, opt, 0, false, defaultTimeoutSeconds)
	}

	It("should clean up", cleanup)

	It("should have no common related object metrics after clean up", func() {
		By("Checking metric endpoint for related object gauges")
		Eventually(func() interface{} {
			return utils.GetMetrics("policy_user_errors")
		}, defaultTimeoutSeconds, 1).Should(Equal([]string{}))
	})

	AfterAll(cleanup)
})
