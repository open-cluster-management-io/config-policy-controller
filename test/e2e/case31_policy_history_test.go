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

const (
	case31Policy                 = "../resources/case31_policy_history/pod-policy.yaml"
	case31ConfigPolicy           = "../resources/case31_policy_history/pod-config-policy.yaml"
	case31PolicyName             = "test-policy-security"
	case31ConfigPolicyName       = "config-policy-pod"
	case31PolicyNumber           = "../resources/case31_policy_history/pod-policy-number.yaml"
	case31ConfigPolicyNumber     = "../resources/case31_policy_history/pod-config-policy-number.yaml"
	case31PolicyNumberName       = "test-policy-security-number"
	case31ConfigPolicyNumberName = "config-policy-pod-number"
)

var _ = Describe("Test policy history message when KubeAPI return "+
	"omits values in the returned object", Ordered, func() {
	Describe("status toggling should not be generated When Policy include default value,", Ordered, func() {
		It("creates the policyconfiguration "+case31Policy, func() {
			utils.Kubectl("apply", "-f", case31Policy, "-n", "managed")
		})

		It("verifies the policy "+case31PolicyName+" in "+testNamespace, func() {
			By("bind policy and configurationpolicy")
			parent := utils.GetWithTimeout(clientManagedDynamic, gvrPolicy,
				case31PolicyName, testNamespace, true, defaultTimeoutSeconds)
			Expect(parent).NotTo(BeNil())

			plcDef := utils.ParseYaml(case31ConfigPolicy)
			ownerRefs := plcDef.GetOwnerReferences()
			ownerRefs[0].UID = parent.GetUID()
			plcDef.SetOwnerReferences(ownerRefs)
			_, err := clientManagedDynamic.Resource(gvrConfigPolicy).Namespace(testNamespace).
				Create(context.TODO(), plcDef, metav1.CreateOptions{})
			Expect(err).To(BeNil())

			By("check configurationpolicy exist")
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case31ConfigPolicyName, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
		})

		It("check history toggling", func() {
			By("wait until pod is up")
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case31ConfigPolicyName, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))

			By("check events")
			Consistently(func() int {
				eventlen := len(utils.GetMatchingEvents(clientManaged, testNamespace,
					case31ConfigPolicyName, case31ConfigPolicyName, "NonCompliant;", defaultTimeoutSeconds))

				return eventlen
			}, 30, 5).Should(BeNumerically("<", 2))

			Consistently(func() int {
				eventlen := len(utils.GetMatchingEvents(clientManaged, testNamespace,
					case31PolicyName, case31ConfigPolicyName, "NonCompliant;", defaultTimeoutSeconds))

				return eventlen
			}, 30, 5).Should(BeNumerically("<", 3))
		})
		AfterAll(func() {
			utils.Kubectl("delete", "policy", case31PolicyName, "-n",
				"managed", "--ignore-not-found")
			configlPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case31ConfigPolicyName, "managed", false, defaultTimeoutSeconds,
			)
			utils.Kubectl("delete", "event",
				"--field-selector=involvedObject.name="+case31PolicyName, "-n", "managed")
			utils.Kubectl("delete", "event",
				"--field-selector=involvedObject.name="+case31ConfigPolicyName, "-n", "managed")
			ExpectWithOffset(1, configlPlc).To(BeNil())
		})
	})
	Describe("status should not toggle When Policy include default value of number", Ordered, func() {
		It("creates the policyconfiguration "+case31PolicyNumber, func() {
			utils.Kubectl("apply", "-f", case31PolicyNumber, "-n", "managed")
		})

		It("verifies the policy "+case31PolicyNumberName+" in "+testNamespace, func() {
			By("bind policy and configurationpolicy")
			parent := utils.GetWithTimeout(clientManagedDynamic, gvrPolicy,
				case31PolicyNumberName, testNamespace, true, defaultTimeoutSeconds)
			Expect(parent).NotTo(BeNil())

			plcDef := utils.ParseYaml(case31ConfigPolicyNumber)
			ownerRefs := plcDef.GetOwnerReferences()
			ownerRefs[0].UID = parent.GetUID()
			plcDef.SetOwnerReferences(ownerRefs)
			_, err := clientManagedDynamic.Resource(gvrConfigPolicy).Namespace(testNamespace).
				Create(context.TODO(), plcDef, metav1.CreateOptions{})
			Expect(err).To(BeNil())

			By("check configurationpolicy exist")
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case31ConfigPolicyNumberName, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
		})

		It("check history toggling", func() {
			By("wait until pod is up")
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case31ConfigPolicyNumberName, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))

			By("check events")
			Consistently(func() int {
				eventLen := len(utils.GetMatchingEvents(clientManaged, testNamespace, case31ConfigPolicyNumberName,
					case31ConfigPolicyNumberName, "NonCompliant;", defaultTimeoutSeconds))

				return eventLen
			}, 30, 5).Should(BeNumerically("<", 2))

			// NOTE: pick policy event, these event's reason include ConfigPolicyName
			Consistently(func() int {
				eventLen := len(utils.GetMatchingEvents(clientManaged, testNamespace,
					case31PolicyNumberName, case31ConfigPolicyNumberName, "NonCompliant;", defaultTimeoutSeconds))

				return eventLen
			}, 30, 5).Should(BeNumerically("<", 3))
		})
		AfterAll(func() {
			utils.Kubectl("delete", "policy", case31PolicyNumberName, "-n",
				"managed", "--ignore-not-found")
			configlPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case31ConfigPolicyName, "managed", false, defaultTimeoutSeconds,
			)
			utils.Kubectl("delete", "event",
				"--field-selector=involvedObject.name="+case31PolicyNumberName, "-n", "managed")
			utils.Kubectl("delete", "event",
				"--field-selector=involvedObject.name="+case31ConfigPolicyNumberName, "-n", "managed")

			ExpectWithOffset(1, configlPlc).To(BeNil())
		})
	})
})
