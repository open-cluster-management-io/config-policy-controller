// Copyright (c) 2021 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package e2e

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"open-cluster-management.io/config-policy-controller/test/utils"
)

var _ = Describe("Test policy history messages when KubeAPI omits values in the returned object", Ordered, func() {
	doHistoryTest := func(policyYAML, policyName, configPolicyYAML, configPolicyName string) {
		By("Waiting until the policy is initially compliant")
		Eventually(func() interface{} {
			managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				configPolicyName, testNamespace, true, defaultTimeoutSeconds)

			return utils.GetComplianceState(managedPlc)
		}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))

		By("Checking the events on the configuration policy")
		Consistently(func() int {
			eventlen := len(utils.GetMatchingEvents(clientManaged, testNamespace,
				configPolicyName, configPolicyName, "NonCompliant;", defaultTimeoutSeconds))

			return eventlen
		}, 30, 5).Should(BeNumerically("<", 2))

		By("Checking the events on the parent policy")
		// NOTE: pick policy event, these event's reason include ConfigPolicyName
		Consistently(func() int {
			eventlen := len(utils.GetMatchingEvents(clientManaged, testNamespace,
				policyName, configPolicyName, "NonCompliant;", defaultTimeoutSeconds))

			return eventlen
		}, 30, 5).Should(BeNumerically("<", 3))
	}

	const (
		rsrcPath = "../resources/case31_policy_history/"
	)

	Describe("status should not toggle when a boolean field might be omitted", Ordered, func() {
		const (
			policyYAML       = rsrcPath + "pod-policy.yaml"
			policyName       = "test-policy-security"
			configPolicyYAML = rsrcPath + "pod-config-policy.yaml"
			configPolicyName = "config-policy-pod"
		)

		It("sets up a configuration policy with an omitempty boolean set to false", func() {
			createConfigPolicyWithParent(policyYAML, policyName, configPolicyYAML)
		})

		It("checks the policy's history", func() {
			doHistoryTest(policyYAML, policyName, configPolicyYAML, configPolicyName)
		})

		AfterAll(func() {
			utils.Kubectl("delete", "policy", policyName, "-n", "managed", "--ignore-not-found")
			configlPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				configPolicyName, "managed", false, defaultTimeoutSeconds,
			)
			utils.Kubectl("delete", "event", "--field-selector=involvedObject.name="+policyName, "-n", "managed")
			utils.Kubectl("delete", "event", "--field-selector=involvedObject.name="+configPolicyName, "-n", "managed")
			ExpectWithOffset(1, configlPlc).To(BeNil())
		})
	})

	Describe("status should not toggle when a numerical field might be omitted", Ordered, func() {
		const (
			policyYAML       = rsrcPath + "pod-policy-number.yaml"
			policyName       = "test-policy-security-number"
			configPolicyYAML = rsrcPath + "pod-config-policy-number.yaml"
			configPolicyName = "config-policy-pod-number"
		)

		It("sets up a configuration policy with an omitempty number set to 0", func() {
			createConfigPolicyWithParent(policyYAML, policyName, configPolicyYAML)
		})

		It("checks the policy's history", func() {
			doHistoryTest(policyYAML, policyName, configPolicyYAML, configPolicyName)
		})

		AfterAll(func() {
			utils.Kubectl("delete", "policy", policyName, "-n", "managed", "--ignore-not-found")
			configlPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				configPolicyName, "managed", false, defaultTimeoutSeconds,
			)
			utils.Kubectl("delete", "event", "--field-selector=involvedObject.name="+policyName, "-n", "managed")
			utils.Kubectl("delete", "event", "--field-selector=involvedObject.name="+configPolicyName, "-n", "managed")
			ExpectWithOffset(1, configlPlc).To(BeNil())
		})
	})

	Describe("status should not toggle when an array might be omitted", Ordered, func() {
		const (
			policyYAML       = rsrcPath + "rb-policy-emptyarray.yaml"
			policyName       = "test-policy-security-emptyarray"
			configPolicyYAML = rsrcPath + "rb-config-policy-emptyarray.yaml"
			configPolicyName = "config-policy-rb-emptyarray"
		)

		It("sets up a configuration policy with an omitempty number set to 0", func() {
			createConfigPolicyWithParent(policyYAML, policyName, configPolicyYAML)
		})

		It("checks the policy's history", func() {
			doHistoryTest(policyYAML, policyName, configPolicyYAML, configPolicyName)
		})

		AfterAll(func() {
			utils.Kubectl("delete", "policy", policyName, "-n", "managed", "--ignore-not-found")
			configlPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				configPolicyName, "managed", false, defaultTimeoutSeconds,
			)
			utils.Kubectl("delete", "event", "--field-selector=involvedObject.name="+policyName, "-n", "managed")
			utils.Kubectl("delete", "event", "--field-selector=involvedObject.name="+configPolicyName, "-n", "managed")
			ExpectWithOffset(1, configlPlc).To(BeNil())
		})
	})
})

func createConfigPolicyWithParent(parentPolicyYAML, parentPolicyName, configPolicyYAML string) {
	By("Creating the parent policy")
	utils.Kubectl("apply", "-f", parentPolicyYAML, "-n", testNamespace)
	parent := utils.GetWithTimeout(clientManagedDynamic, gvrPolicy,
		parentPolicyName, testNamespace, true, defaultTimeoutSeconds)
	Expect(parent).NotTo(BeNil())

	plcDef := utils.ParseYaml(configPolicyYAML)
	ownerRefs := plcDef.GetOwnerReferences()
	ownerRefs[0].UID = parent.GetUID()
	plcDef.SetOwnerReferences(ownerRefs)

	By("Creating the configuration policy with the owner reference")

	_, err := clientManagedDynamic.Resource(gvrConfigPolicy).Namespace(testNamespace).
		Create(context.TODO(), plcDef, metav1.CreateOptions{})
	Expect(err).To(BeNil())

	By("Verifying the configuration policy exists")

	plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
		plcDef.GetName(), testNamespace, true, defaultTimeoutSeconds)
	Expect(plc).NotTo(BeNil())
}
