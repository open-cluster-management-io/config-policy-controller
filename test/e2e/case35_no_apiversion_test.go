// Copyright (c) 2023 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package e2e

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"open-cluster-management.io/config-policy-controller/test/utils"
)

var _ = Describe("Test a policy with an objectDefinition with an invalid apiVersion", func() {
	const (
		rsrcPath      = "../resources/case35_no_apiversion/"
		policyYAML    = rsrcPath + "policy.yaml"
		policyName    = "case35-parent"
		cfgPlcYAML    = rsrcPath + "config-policy.yaml"
		cfgPlcName    = "case35-cfgpol"
		complianceMsg = "^NonCompliant; violation - The kind and apiVersion fields are required on the object " +
			"template at index 0 in policy " + cfgPlcName + "; violation - couldn't find mapping resource with kind " +
			"OooglyBoogly in API version kubeymckkube.com/v6alpha6, please check if you have CRD deployed$"
	)

	It("Should have the expected events", func() {
		By("Setting up the policy")
		createObjWithParent(policyYAML, policyName, cfgPlcYAML, testNamespace, gvrPolicy, gvrConfigPolicy)

		By("Checking there is a NonCompliant event on the policy")
		Eventually(func() interface{} {
			return utils.GetMatchingEvents(
				clientManaged, testNamespace, policyName, cfgPlcName, complianceMsg, defaultTimeoutSeconds,
			)
		}, defaultTimeoutSeconds, 5).ShouldNot(BeEmpty())

		By("Checking there are no Compliant events on the policy")
		Consistently(func() interface{} {
			return utils.GetMatchingEvents(clientManaged, testNamespace,
				policyName, cfgPlcName, "^Compliant;", defaultTimeoutSeconds)
		}, defaultConsistentlyDuration, 5).Should(BeEmpty())
	})

	AfterEach(func() {
		utils.KubectlDelete("policy", policyName, "-n", "managed")
		configPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
			cfgPlcName, "managed", false, defaultTimeoutSeconds,
		)
		Expect(configPlc).To(BeNil())
		utils.KubectlDelete("event", "--field-selector=involvedObject.name="+policyName, "-n", "managed")
		utils.KubectlDelete("event", "--field-selector=involvedObject.name="+cfgPlcName, "-n", "managed")
	})
})
