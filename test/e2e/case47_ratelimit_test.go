// Copyright (c) 2025 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package e2e

import (
	"fmt"
	"strconv"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"open-cluster-management.io/config-policy-controller/test/utils"
)

var _ = Describe("Test config policy ratelimiting", Ordered, func() {
	const (
		policyName    = "case47-cfgpolicy"
		policyYaml    = "../resources/case47_ratelimit/" + policyName + ".yaml"
		configMapName = "case47-configmap-to-watch"
		configMapYaml = "../resources/case47_ratelimit/" + configMapName + ".yaml"
		managedCMName = "case47-configmap-from-policy"
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

	// evaluationsSince returns a poll function reporting how many evaluations have happened since
	// the baseline was taken. This function always logs details, to help troubleshoot whether a
	// failure was caused by failing to get the metric, or based on the value of the metric.
	evaluationsSince := func(baseline float64) func() (float64, error) {
		return func() (float64, error) {
			total, err := metricCheck("config_policy_evaluation_total", "name", policyName)
			if err != nil {
				GinkgoWriter.Printf("evaluationsSince: failed to fetch config_policy_evaluation_total: %v\n", err)

				return 0, err
			}

			delta := total - baseline
			GinkgoWriter.Printf("evaluationsSince: total=%.0f baseline=%.0f delta=%.0f\n", total, baseline, delta)

			return delta, nil
		}
	}

	BeforeAll(func() {
		By("Creating " + policyYaml)
		utils.Kubectl("apply", "-f", policyYaml, "-n", testNamespace)
		By("Creating " + configMapYaml)
		utils.Kubectl("apply", "-f", configMapYaml) // The YAML specifies namespace "default"
	})

	It("should initially have a small number of evaluations", FlakeAttempts(3), func() {
		// The metric may not have appeared yet immediately after BeforeAll creates the policy, so
		// wait for it rather than requiring it on the first try.
		var baseline float64

		Eventually(func() error {
			total, err := metricCheck("config_policy_evaluation_total", "name", policyName)
			if err != nil {
				return err
			}

			baseline = total

			return nil
		}, 10, 2).Should(Succeed())

		Eventually(evaluationsSince(baseline), 10, 2).Should(BeNumerically("<=", 5))
		Consistently(evaluationsSince(baseline), 10, 2).Should(BeNumerically("<=", 5))
	})

	value := 0

	It("should limit the number of evaluations when a watched object changes frequently", FlakeAttempts(3), func() {
		baseline, err := metricCheck("config_policy_evaluation_total", "name", policyName)
		Expect(err).ToNot(HaveOccurred())

		start := time.Now()

		By("Updating the watched configmap frequently for 10 seconds")

		for start.Add(10 * time.Second).After(time.Now()) {
			value++
			utils.Kubectl("patch", "configmap", configMapName, "--type=json", "-p",
				`[{"op": "replace", "path": "/data/foo", "value": "`+strconv.Itoa(value)+`"}]`)
			time.Sleep(150 * time.Millisecond)
		}

		Consistently(evaluationsSince(baseline), 10, 2).Should(BeNumerically("<", 12))
	})

	It("should have updated the object to the final value", FlakeAttempts(3), func() {
		By("Verifying the configmap has bar=" + strconv.Itoa(value))
		Eventually(func(g Gomega) {
			cm := utils.GetWithTimeout(clientManagedDynamic, gvrConfigMap,
				managedCMName, "default", true, defaultTimeoutSeconds)
			g.Expect(cm).NotTo(BeNil())

			val, found, err := unstructured.NestedString(cm.Object, "data", "bar")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(found).To(BeTrue())
			g.Expect(val).Should(Equal(strconv.Itoa(value)))
		}, defaultTimeoutSeconds, 1).Should(Succeed())
	})

	AfterAll(func() {
		utils.KubectlDelete("-n", testNamespace, "-f", policyYaml)
		utils.KubectlDelete("-f", configMapYaml)
		utils.KubectlDelete("configmap", "-n", "default", "case47-configmap-from-policy")
	})
})
