package e2e

import (
	"fmt"
	"strconv"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"open-cluster-management.io/config-policy-controller/test/utils"
)

var _ = Describe("Test config policy metrics", Ordered, func() {
	const (
		policyName         = "case28-policy-cfgmap-watch"
		configMap          = "case28-configmap"
		policyYaml         = "../resources/case28_metric/case28_policy.yaml"
		operatorPolicyName = "case28-operatorpolicy"
		operatorPolicyYaml = "../resources/case28_metric/case28_operatorpolicy.yaml"
	)

	metricCheck := func(metricName string, label string, value string) (float64, error) {
		metric := utils.GetMetrics(
			metricName, fmt.Sprintf(`%s=\"%s\"`, label, value))
		if len(metric) == 0 {
			return 0, fmt.Errorf("failed to retrieve any %s metric", metricName)
		}
		metricVal, err := strconv.ParseFloat(metric[0], 64)
		if err != nil {
			return 0, fmt.Errorf("error converting metric: %w", err)
		}

		return metricVal, nil
	}

	BeforeAll(func() {
		By("Creating " + policyYaml)
		utils.Kubectl("apply", "-f", policyYaml, "-n", testNamespace)
		By("Creating " + operatorPolicyYaml)
		utils.Kubectl("apply", "-f", operatorPolicyYaml, "-n", testNamespace)
	})

	It("should correctly report total evaluations for the configurationpolicy", func() {
		By("Checking metric endpoint for total evaluations")
		Eventually(
			metricCheck, defaultTimeoutSeconds, 1,
		).WithArguments("config_policy_evaluation_total", "name", policyName).Should(BeNumerically(">", 0))
	})

	It("should report total evaluation duration for the configurationpolicy", func() {
		By("Checking metric endpoint for total evaluation duration")
		Eventually(
			metricCheck, defaultTimeoutSeconds, 1,
		).WithArguments("config_policy_evaluation_seconds_total", "name", policyName).Should(BeNumerically(">", 0))
	})

	It("should report status for the configurationpolicy", func() {
		By("Checking metric endpoint for configuration policy status")
		Eventually(
			metricCheck, defaultTimeoutSeconds, 1,
		).WithArguments("cluster_policy_governance_info", "policy", policyName).Should(BeNumerically("==", 1))

		By("Enforcing the policy")
		utils.EnforceConfigurationPolicy(policyName, testNamespace)
		Eventually(
			metricCheck, defaultTimeoutSeconds, 1,
		).WithArguments("cluster_policy_governance_info", "policy", policyName).Should(BeNumerically("==", 0))
	})

	It("should report status for the operatorpolicy", func() {
		By("Checking metric endpoint for operator policy status")
		Eventually(
			metricCheck, defaultTimeoutSeconds, 1,
		).WithArguments("cluster_policy_governance_info", "policy", operatorPolicyName).Should(BeNumerically("==", 1))
	})

	AfterAll(func() {
		utils.KubectlDelete("-n", testNamespace, "-f", policyYaml)
		utils.KubectlDelete("-n", testNamespace, "-f", operatorPolicyYaml)
		utils.KubectlDelete("configmap", "-n", "default", configMap)

		for metricName, label := range map[string]string{
			"config_policy_evaluation_total":         "name",
			"config_policy_evaluation_seconds_total": "name",
			"cluster_policy_governance_info":         "policy",
		} {
			Eventually(
				utils.GetMetrics, defaultTimeoutSeconds, 1,
			).WithArguments(metricName, fmt.Sprintf(`%s=\"%s\"`, label, policyName)).Should(HaveLen(0))
		}
	})
})
