// Copyright Contributors to the Open Cluster Management project

package e2e

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"open-cluster-management.io/config-policy-controller/test/utils"
)

const (
	case32ConfigPolicyName string = "case32config"
	case32CreatePolicyYaml string = "../resources/case32_secret_stringdata/case32_create_secret.yaml"
)

var _ = Describe("Test converted stringData being decoded before comparison for Secrets", Ordered, func() {
	It("Config should be created properly on the managed cluster", func() {
		By("Creating " + case32ConfigPolicyName + " on managed")
		utils.Kubectl("apply", "-f", case32CreatePolicyYaml, "-n", testNamespace)
		cfg := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
			case32ConfigPolicyName, testNamespace, true, defaultTimeoutSeconds)
		Expect(cfg).NotTo(BeNil())
	})

	It("Verifies the config policy is initially compliant "+case32ConfigPolicyName+" in "+testNamespace, func() {
		By("Waiting for " + case32ConfigPolicyName + " to become Compliant")
		Eventually(func() interface{} {
			cfgplc := utils.GetWithTimeout(
				clientManagedDynamic, gvrConfigPolicy,
				case32ConfigPolicyName, testNamespace,
				true, defaultTimeoutSeconds,
			)

			return utils.GetComplianceState(cfgplc)
		}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
	})

	It("Verifies that a secret is created by "+case32ConfigPolicyName+" in openshift-config", func() {
		By("Grabbing htpasswd-secret from namespace openshift-config")
		scrt := utils.GetWithTimeout(
			clientManagedDynamic, gvrSecret, "htpasswd-secret", testNamespace, true, defaultTimeoutSeconds,
		)

		Expect(scrt).NotTo(BeNil())
	})

	It("Verifies the config policy "+case32ConfigPolicyName+" does not update in "+testNamespace, func() {
		By("Checking the events on the configuration policy" + case32ConfigPolicyName)
		Consistently(func() int {
			eventlen := len(utils.GetMatchingEvents(
				clientManaged, testNamespace,
				case32ConfigPolicyName, case32ConfigPolicyName,
				"updated", defaultTimeoutSeconds))

			return eventlen
		}, 30, 2).Should(BeNumerically("<", 1))
	})

	AfterAll(func() {
		utils.Kubectl("delete", "configurationpolicy", case32ConfigPolicyName, "-n", testNamespace)
		utils.Kubectl("delete", "secret", "htpasswd-secret", "-n", testNamespace)
		utils.Kubectl("delete", "event",
			"--field-selector=involvedObject.name="+case32ConfigPolicyName, "-n", "managed")
	})
})
