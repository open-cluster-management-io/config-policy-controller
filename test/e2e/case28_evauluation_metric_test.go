package e2e

import (
	"fmt"
	"os/exec"
	"strconv"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"open-cluster-management.io/config-policy-controller/test/utils"
)

var _ = Describe("Test config policy evaluation metrics", Ordered, func() {
	const (
		policyName = "policy-cfgmap-watch"
		policyYaml = "../resources/case28_evaluation_metric/case28_policy.yaml"
	)

	prePolicyEvalDuration := -1

	It("should record number of evaluations under 30 seconds for comparison", func() {
		evalMetric := utils.GetMetrics(
			"config_policies_evaluation_duration_seconds_bucket", fmt.Sprintf(`le=\"%d\"`, 30))
		Expect(len(evalMetric) != 0).To(BeTrue())
		numEvals, err := strconv.Atoi(evalMetric[0])
		Expect(err == nil).To(BeTrue())
		prePolicyEvalDuration = numEvals
		Expect(prePolicyEvalDuration > -1).To(BeTrue())
	})

	It("should create policy", func() {
		By("Creating " + policyYaml)
		utils.Kubectl("apply",
			"-f", policyYaml,
			"-n", testNamespace)
		By("Verifying the policies were created")
		plc := utils.GetWithTimeout(
			clientManagedDynamic, gvrConfigPolicy, policyName, testNamespace, true, defaultTimeoutSeconds,
		)
		Expect(plc).NotTo(BeNil())
	})

	It("should update evaluation duration after the configurationpolicy is created", func() {
		By("Checking metric bucket for evaluation duration (30 seconds or less)")
		Eventually(func() interface{} {
			metric := utils.GetMetrics(
				"config_policies_evaluation_duration_seconds_bucket", fmt.Sprintf(`le=\"%d\"`, 30))
			if len(metric) == 0 {
				return false
			}
			numEvals, err := strconv.Atoi(metric[0])
			if err != nil {
				return false
			}

			return numEvals > prePolicyEvalDuration
		}, defaultTimeoutSeconds, 1).Should(Equal(true))
	})

	It("should correctly report total evaluations for the configurationpolicy", func() {
		By("Checking metric endpoint for total evaluations")
		Eventually(func() interface{} {
			metric := utils.GetMetrics(
				"config_policy_evaluation_total", fmt.Sprintf(`name=\"%s\"`, policyName))
			if len(metric) == 0 {
				return false
			}
			numEvals, err := strconv.Atoi(metric[0])
			if err != nil {
				return false
			}

			return numEvals > 0
		}, defaultTimeoutSeconds, 1).Should(Equal(true))
	})

	It("should report total evaluation duration for the configurationpolicy", func() {
		By("Checking metric endpoint for total evaluation duration")
		Eventually(func() interface{} {
			metric := utils.GetMetrics(
				"config_policy_evaluation_seconds_total", fmt.Sprintf(`name=\"%s\"`, policyName))
			if len(metric) == 0 {
				return false
			}
			numEvals, err := strconv.ParseFloat(metric[0], 64)
			if err != nil {
				return false
			}

			return numEvals > 0
		}, defaultTimeoutSeconds, 1).Should(Equal(true))
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

		Eventually(func() interface{} {
			return utils.GetMetrics(
				"config_policy_evaluation_total", fmt.Sprintf(`name=\"%s\"`, policyName))
		}, defaultTimeoutSeconds, 1).Should(Equal([]string{}))
		Eventually(func() interface{} {
			return utils.GetMetrics(
				"config_policy_evaluation_seconds_total", fmt.Sprintf(`name=\"%s\"`, policyName))
		}, defaultTimeoutSeconds, 1).Should(Equal([]string{}))
	}

	AfterAll(cleanup)
})
