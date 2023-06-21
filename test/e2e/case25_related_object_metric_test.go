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
		policy1Name   = "case25-test-policy-1"
		policy2Name   = "case25-test-policy-2"
		relatedObject = "case25-configmap"
		policyYaml    = "../resources/case25_related_object_metric/case25-test-policy.yaml"
	)
	It("should create policies and related objects", func() {
		By("Creating " + policyYaml)
		utils.Kubectl("apply",
			"-f", policyYaml,
			"-n", testNamespace)
		By("Verifying the policies were created")
		plc1 := utils.GetWithTimeout(
			clientManagedDynamic, gvrConfigPolicy, policy1Name, testNamespace, true, defaultTimeoutSeconds,
		)
		Expect(plc1).NotTo(BeNil())
		plc2 := utils.GetWithTimeout(
			clientManagedDynamic, gvrConfigPolicy, policy2Name, testNamespace, true, defaultTimeoutSeconds,
		)
		By("Verifying the related object was created")
		Expect(plc2).NotTo(BeNil())
		obj := utils.GetWithTimeout(
			clientManagedDynamic, gvrConfigMap, relatedObject, "default", true, defaultTimeoutSeconds,
		)
		Expect(obj).NotTo(BeNil())
	})

	It("should correctly report common related objects", func() {
		By("Checking metric endpoint for relate object gauge for policy " + policy1Name)
		Eventually(func() interface{} {
			return utils.GetMetrics(
				"common_related_objects", fmt.Sprintf(`policy=\"%s/%s\"`, testNamespace, policy1Name))
		}, defaultTimeoutSeconds, 1).Should(Equal([]string{"2"}))
		By("Checking metric endpoint for relate object gauge for policy " + policy2Name)
		Eventually(func() interface{} {
			return utils.GetMetrics(
				"common_related_objects", fmt.Sprintf(`policy=\"%s/%s\"`, testNamespace, policy2Name))
		}, defaultTimeoutSeconds, 1).Should(Equal([]string{"2"}))
	})

	cleanup := func() {
		// Delete the policies and ignore any errors (in case it was deleted previously)
		cmd := exec.Command("kubectl", "delete",
			"-f", policyYaml,
			"-n", testNamespace, "--ignore-not-found")
		_, _ = cmd.CombinedOutput()
		utils.GetWithTimeout(
			clientManagedDynamic, gvrConfigPolicy, policy1Name, testNamespace, false, defaultTimeoutSeconds,
		)
		utils.GetWithTimeout(
			clientManagedDynamic, gvrConfigPolicy, policy2Name, testNamespace, false, defaultTimeoutSeconds,
		)
		utils.GetWithTimeout(
			clientManagedDynamic, gvrConfigMap, relatedObject, "default", false, defaultTimeoutSeconds)
	}

	It("should clean up", cleanup)

	It("should have no common related object metrics after clean up", func() {
		By("Checking metric endpoint for related object gauges")
		Eventually(func() interface{} {
			return utils.GetMetrics("common_related_objects",
				fmt.Sprintf(`policy=\"%s/%s\"`, testNamespace, policy1Name))
		}, defaultTimeoutSeconds, 1).Should(Equal([]string{}))
		Eventually(func() interface{} {
			return utils.GetMetrics("common_related_objects",
				fmt.Sprintf(`policy=\"%s/%s\"`, testNamespace, policy2Name))
		}, defaultTimeoutSeconds, 1).Should(Equal([]string{}))
	})
})
