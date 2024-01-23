package e2e

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"open-cluster-management.io/config-policy-controller/test/utils"
)

var _ = Describe("Test installing an operator from OperatorPolicy", Ordered, func() {
	const (
		case38BasePath              = "../resources/case38_operator_install/"
		case38OpPolicy              = case38BasePath + "case38_OpPlc.yaml"
		case38OpPolicyDefaultOg     = case38BasePath + "case38_OpPlc_default_og.yaml"
		case38OpPolicyName          = "test-operator-policy"
		case38OpPolicyDefaultOgName = "test-operator-policy-no-og"
		case38SubscriptionName      = "project-quay"
		case38DeploymentName        = "quay-operator-tng"
		case38OpPolicyNS            = "op-1"
		case38OpPolicyDefaultOgNS   = "op-2"
		case38OgName                = "og-single"
		case38DefaultOgName         = case38SubscriptionName + "-default-og"
	)

	Describe("The operator is being installed for the first time", Ordered, func() {
		Context("When there is a OperatorGroup spec specified", Ordered, func() {
			BeforeAll(func() {
				// Create ns op-1
				utils.Kubectl("create", "ns", case38OpPolicyNS)
				// Apply a policy in ns op-1
				utils.Kubectl("apply", "-f", case38OpPolicy, "-n", case38OpPolicyNS)
				// Check if applied properly
				op := utils.GetWithTimeout(clientManagedDynamic, gvrOperatorPolicy, case38OpPolicyName,
					case38OpPolicyNS, true, defaultTimeoutSeconds)
				Expect(op).NotTo(BeNil())
			})

			It("Should create the OperatorGroup", func() {
				og := utils.GetWithTimeout(clientManagedDynamic, gvrOperatorGroup,
					case38OgName, case38OpPolicyNS, true, defaultTimeoutSeconds)

				Expect(og).NotTo(BeNil())
			})

			It("Should create the Subscription from the spec", func() {
				sub := utils.GetWithTimeout(clientManagedDynamic, gvrSubscription,
					case38SubscriptionName, case38OpPolicyNS, true, defaultTimeoutSeconds)

				Expect(sub).NotTo(BeNil())
			})

			It("Should become Compliant", func() {
				OpPlc := utils.GetWithTimeout(clientManagedDynamic, gvrOperatorPolicy,
					case38OpPolicyName, case38OpPolicyNS, true, defaultTimeoutSeconds)

				Expect(utils.GetComplianceState(OpPlc)).To(Equal("Compliant"))
			})

			It("Should have installed the operator", func() {
				deployment := utils.GetWithTimeout(clientManagedDynamic, gvrDeployment,
					case38DeploymentName, case38OpPolicyNS, true, defaultTimeoutSeconds*2)

				Expect(deployment).NotTo(BeNil())
			})

			AfterAll(func() {
				// Delete related resources created during test
				utils.Kubectl("delete", "operatorpolicy", case38OpPolicyName, "-n", case38OpPolicyNS)
				utils.Kubectl("delete", "subscription", case38SubscriptionName, "-n", case38OpPolicyNS)
				utils.Kubectl("delete", "operatorgroup", case38OgName, "-n", case38OpPolicyNS)
				utils.Kubectl("delete", "ns", case38OpPolicyNS)
			})
		})

		Context("When there is no OperatorGroup spec specified", Ordered, func() {
			BeforeAll(func() {
				// Create ns op-2
				utils.Kubectl("create", "ns", case38OpPolicyDefaultOgNS)
				// Apply a policy in ns op-2
				utils.Kubectl("apply", "-f", case38OpPolicyDefaultOg, "-n", case38OpPolicyDefaultOgNS)
				// Check if applied properly
				op := utils.GetWithTimeout(clientManagedDynamic, gvrOperatorPolicy, case38OpPolicyDefaultOgName,
					case38OpPolicyDefaultOgNS, true, defaultTimeoutSeconds)
				Expect(op).NotTo(BeNil())
			})

			It("Should create a default OperatorGroup", func() {
				og := utils.GetWithTimeout(clientManagedDynamic, gvrOperatorGroup,
					case38DefaultOgName, case38OpPolicyDefaultOgNS, true, defaultTimeoutSeconds)

				Expect(og).NotTo(BeNil())
			})

			It("Should create the Subscription from the spec", func() {
				sub := utils.GetWithTimeout(clientManagedDynamic, gvrSubscription,
					case38SubscriptionName, case38OpPolicyDefaultOgNS, true, defaultTimeoutSeconds)

				Expect(sub).NotTo(BeNil())
			})

			It("Should become Compliant", func() {
				Eventually(func() interface{} {
					OpPlc := utils.GetWithTimeout(clientManagedDynamic, gvrOperatorPolicy,
						case38OpPolicyDefaultOgName, case38OpPolicyDefaultOgNS, true, defaultTimeoutSeconds)

					return utils.GetComplianceState(OpPlc)
				}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
			})

			It("Should have installed the operator", func() {
				deployment := utils.GetWithTimeout(clientManagedDynamic, gvrDeployment,
					case38DeploymentName, case38OpPolicyDefaultOgNS, true, defaultTimeoutSeconds*2)

				Expect(deployment).NotTo(BeNil())
			})

			AfterAll(func() {
				// Delete related resources created during test
				utils.Kubectl("delete", "operatorpolicy", case38OpPolicyDefaultOgName, "-n", case38OpPolicyDefaultOgNS)
				utils.Kubectl("delete", "subscription", case38SubscriptionName, "-n", case38OpPolicyDefaultOgNS)
				utils.Kubectl("delete", "operatorgroup", case38DefaultOgName, "-n", case38OpPolicyDefaultOgNS)
				utils.Kubectl("delete", "ns", case38OpPolicyDefaultOgNS)
			})
		})
	})
})
