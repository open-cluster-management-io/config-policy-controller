// Copyright (c) 2021 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package e2e

import (
	"context"
	"strconv"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"open-cluster-management.io/config-policy-controller/test/utils"
)

var _ = Describe("Test policy history messages when KubeAPI omits values in the returned object", Ordered, func() {
	doHistoryTest := func(policyName, configPolicyName string) {
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
			doHistoryTest(policyName, configPolicyName)
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
			doHistoryTest(policyName, configPolicyName)
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
			doHistoryTest(policyName, configPolicyName)
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

	Describe("status should not toggle when a struct might be omitted", Ordered, func() {
		const (
			policyYAML       = rsrcPath + "event-policy-emptystruct.yaml"
			policyName       = "test-policy-security-emptystruct"
			configPolicyYAML = rsrcPath + "event-config-policy-emptystruct.yaml"
			configPolicyName = "config-policy-event-emptystruct"
		)

		It("sets up a configuration policy with struct set to null", func() {
			createConfigPolicyWithParent(policyYAML, policyName, configPolicyYAML)
		})

		It("checks the policy's history", func() {
			doHistoryTest(policyName, configPolicyName)
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
	Describe("policy message should not be truncated", func() {
		const (
			case31LMPolicy           = "../resources/case31_policy_history/long-message-policy.yaml"
			case31LMConfigPolicy     = "../resources/case31_policy_history/long-message-config-policy.yaml"
			case31LMPolicyName       = "long-message-policy"
			case31LMConfigPolicyName = "long-message-config-policy"
			namespacePrefix          = "innovafertanimvsmvtatasdicereformascorporinnovafertanimvsmvt"
		)
		It("Test policy message length is over 1024 ", func() {
			By("Create namespaces")
			for i := range [15]int{} {
				utils.Kubectl("create", "ns", namespacePrefix+strconv.Itoa(i+1))
			}
			utils.Kubectl("apply", "-f", case31LMPolicy, "-n", "managed")
			By("bind policy and configurationpolicy")
			parent := utils.GetWithTimeout(clientManagedDynamic, gvrPolicy,
				case31LMPolicyName, testNamespace, true, defaultTimeoutSeconds)
			Expect(parent).NotTo(BeNil())

			plcDef := utils.ParseYaml(case31LMConfigPolicy)
			ownerRefs := plcDef.GetOwnerReferences()
			ownerRefs[0].UID = parent.GetUID()
			plcDef.SetOwnerReferences(ownerRefs)
			_, err := clientManagedDynamic.Resource(gvrConfigPolicy).Namespace(testNamespace).
				Create(context.TODO(), plcDef, metav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred())

			By("check configurationpolicy exist")
			Eventually(func() interface{} {
				plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case31LMConfigPolicyName, testNamespace, true, defaultTimeoutSeconds)
				compliant := utils.GetComplianceState(plc)

				return compliant
			}, 30, 5).Should(Equal("NonCompliant"))

			By("check message longer than 1024")
			Eventually(func() int {
				event := utils.GetMatchingEvents(clientManaged, testNamespace,
					case31LMPolicyName, case31LMConfigPolicyName, "NonCompliant", defaultTimeoutSeconds)

				Expect(event).ShouldNot(BeEmpty())
				message := event[len(event)-1].Message

				return len(message)
			}, 30, 5).Should(BeNumerically(">", 1024))
		})
		AfterAll(func() {
			utils.Kubectl("delete", "policy", case31LMPolicyName, "-n",
				"managed", "--ignore-not-found")
			configlPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case31LMPolicyName, "managed", false, defaultTimeoutSeconds,
			)
			Expect(configlPlc).To(BeNil())
			utils.Kubectl("delete", "event",
				"--field-selector=involvedObject.name="+case31LMPolicyName, "-n", "managed")
			utils.Kubectl("delete", "event",
				"--field-selector=involvedObject.name="+case31LMConfigPolicy, "-n", "managed")
			for i := range [15]int{} {
				utils.Kubectl("delete", "ns", namespacePrefix+strconv.Itoa(i+1),
					"--ignore-not-found", "--force", "--grace-period=0")
			}
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
	Expect(err).ToNot(HaveOccurred())

	By("Verifying the configuration policy exists")

	plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
		plcDef.GetName(), testNamespace, true, defaultTimeoutSeconds)
	Expect(plc).NotTo(BeNil())
}
