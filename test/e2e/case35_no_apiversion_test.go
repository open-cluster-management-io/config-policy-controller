// Copyright (c) 2023 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package e2e

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"open-cluster-management.io/config-policy-controller/test/utils"
)

var _ = Describe("Test a policy with an objectDefinition that is missing apiVersion", func() {
	const (
		rsrcPath   = "../resources/case35_no_apiversion/"
		policyYAML = rsrcPath + "policy.yaml"
		policyName = "case35-parent"
		cfgPlcYAML = rsrcPath + "config-policy.yaml"
		cfgPlcName = "case35-cfgpol"
	)

	It("Should have the expected events", func() {
		By("Setting up the policy")
		createObjWithParent(policyYAML, policyName, cfgPlcYAML, testNamespace, gvrPolicy, gvrConfigPolicy)

		By("Checking there is a NonCompliant event on the policy")
		Eventually(func() interface{} {
			return utils.GetMatchingEvents(clientManaged, testNamespace,
				policyName, cfgPlcName, "^NonCompliant;.*missing apiVersion", defaultTimeoutSeconds)
		}, defaultTimeoutSeconds, 5).ShouldNot(BeEmpty())

		By("Checking there are no Compliant events on the policy")
		Consistently(func() interface{} {
			return utils.GetMatchingEvents(clientManaged, testNamespace,
				policyName, cfgPlcName, "^Compliant;", defaultTimeoutSeconds)
		}, defaultConsistentlyDuration, 5).Should(BeEmpty())
	})

	AfterEach(func() {
		utils.Kubectl("delete", "policy", policyName, "-n", "managed", "--ignore-not-found")
		configPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
			cfgPlcName, "managed", false, defaultTimeoutSeconds,
		)
		Expect(configPlc).To(BeNil())
		utils.Kubectl("delete", "event", "--field-selector=involvedObject.name="+policyName, "-n", "managed")
		utils.Kubectl("delete", "event", "--field-selector=involvedObject.name="+cfgPlcName, "-n", "managed")
	})
})
