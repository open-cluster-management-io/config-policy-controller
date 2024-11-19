// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package e2e

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"open-cluster-management.io/config-policy-controller/test/utils"
)

var _ = Describe("Test results of resource selection", Ordered, func() {
	const (
		objectSelectorPatchFmt = `--patch=[{
				"op":"replace",
				"path":"/spec/object-templates/0/objectSelector",
				"value":%s
			}]`

		targetNs   = "case42-e2e-1"
		prereqYaml = "../resources/case42_resource_selector/case42_results_prereq.yaml"
		policyYaml = "../resources/case42_resource_selector/case42_results_policy.yaml"
		policyName = "case42-selector-results-e2e"

		filterErrMsgFmt = "Error parsing provided objectSelector in the object-template at index [0]: %s"
		noMatchesMsg    = "object of kind FakeAPI has no name specified from " +
			"the policy objectSelector nor the object metadata"
	)

	// Test setup for resource selection policy tests:
	// - FakeAPIs `case42-[1-5]-e2e`, each with a `case42: <name>` label
	// - Deployed policy should be Compliant since the objectSelector is empty and acts as unnamed objectDefinition
	// - Policies are patched so that the objects don't match and should be NonCompliant
	BeforeAll(func() {
		By("Applying prerequisites")
		utils.Kubectl("apply", "-n", targetNs, "-f", prereqYaml)
		DeferCleanup(func() {
			utils.KubectlDelete("-n", targetNs, "-f", prereqYaml)
		})

		utils.Kubectl("apply", "-f", policyYaml, "-n", testNamespace)
		DeferCleanup(func() {
			utils.KubectlDelete("-f", policyYaml, "-n", testNamespace)
		})
	})

	Describe("No objectSelector specified", func() {
		It("Verifies policy is compliant with unnamed objectDefinition", func() {
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					policyName, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetStatusMessage(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal(
				"fakeapis [case42-1-e2e, case42-2-e2e, case42-3-e2e, case42-4-e2e, case42-5-e2e]" +
					" found as specified in namespace " + targetNs))
		})
	})

	DescribeTable("ObjectSelector matching all is specified", func(patch string) {
		By("Verifying policy is noncompliant and returns no objects")
		utils.Kubectl("patch", "--namespace=managed", "configurationpolicy", policyName, "--type=json",
			fmt.Sprintf(objectSelectorPatchFmt, `{"matchLabels":{"selects":"nothing"}}`),
		)
		Eventually(func() interface{} {
			managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				policyName, testNamespace, true, defaultTimeoutSeconds)

			return utils.GetStatusMessage(managedPlc)
		}, defaultTimeoutSeconds, 1).Should(Equal(noMatchesMsg))

		By("Verifying policy is compliant and returns all objects")
		utils.Kubectl("patch", "--namespace=managed", "configurationpolicy", policyName, "--type=json",
			fmt.Sprintf(objectSelectorPatchFmt, patch),
		)
		Eventually(func() interface{} {
			managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				policyName, testNamespace, true, defaultTimeoutSeconds)

			return utils.GetStatusMessage(managedPlc)
		}, defaultTimeoutSeconds, 1).Should(Equal(fmt.Sprintf(
			"fakeapis [case42-1-e2e] found as specified in namespace %[1]s; "+
				"fakeapis [case42-2-e2e] found as specified in namespace %[1]s; "+
				"fakeapis [case42-3-e2e] found as specified in namespace %[1]s; "+
				"fakeapis [case42-4-e2e] found as specified in namespace %[1]s; "+
				"fakeapis [case42-5-e2e] found as specified in namespace %[1]s", targetNs)))
	},
		Entry("Empty label selector", `{}`),
		Entry("Empty matchLabels", `{"matchLabels":{}}`),
		Entry("Empty matchExpressions", `{"matchExpressions":[]}`),
		Entry("Empty matchLabels/matchExpressions", `{"matchLabels":{},"matchExpressions":[]}`),
		Entry("Matching matchExpressions", `{"matchExpressions":[{"key":"case42","operator":"Exists"}]}`),
	)

	DescribeTable("Checking results of different objectSelectors", func(patch string, message string) {
		By("patching policy with the test selector")
		utils.Kubectl("patch", "--namespace=managed", "configurationpolicy", policyName, "--type=json",
			fmt.Sprintf(objectSelectorPatchFmt, patch),
		)
		Eventually(func() interface{} {
			managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				policyName, testNamespace, true, defaultTimeoutSeconds)

			return utils.GetStatusMessage(managedPlc)
		}, defaultTimeoutSeconds, 1).Should(
			Equal(message),
			fmt.Sprintf("Unexpected message using patch '%s'", patch))
	},
		Entry("A non-matching LabelSelector",
			`{"matchLabels":{"name":"not-a-fakeapi"}}`,
			noMatchesMsg),
		Entry("MatchExpressions with incorrect operator",
			`{"matchExpressions":[{"key":"name","operator":"Seriously"}]}`,
			fmt.Sprintf(filterErrMsgFmt, `"Seriously" is not a valid label selector operator`),
		),
		Entry("MatchExpressions with missing values",
			`{"matchExpressions":[{"key":"name","operator":"In","values":[]}]}`,
			fmt.Sprintf(filterErrMsgFmt,
				"values: Invalid value: []string(nil): for 'in', 'notin' operators, values set can't be empty"),
		),
	)
})

var _ = Describe("Test behavior of resource selection as resources change", Ordered, func() {
	const (
		targetNs    = "case42-e2e-2"
		extraYaml   = "../resources/case42_resource_selector/case42_behavior_extraobj.yaml"
		nomatchYaml = "../resources/case42_resource_selector/case42_behavior_nomatch.yaml"
		prereqYaml  = "../resources/case42_resource_selector/case42_behavior_prereq.yaml"
		policyYaml  = "../resources/case42_resource_selector/case42_behavior_policy.yaml"
		policyName  = "case42-selector-behavior-e2e"
	)

	BeforeAll(func() {
		By("Applying prerequisites")
		utils.Kubectl("apply", "-n", targetNs, "-f", prereqYaml)
		DeferCleanup(func() {
			utils.KubectlDelete("-n", targetNs, "-f", prereqYaml)
		})

		utils.Kubectl("apply", "-f", policyYaml, "-n", testNamespace)
		DeferCleanup(func() {
			utils.KubectlDelete("-f", policyYaml, "-n", testNamespace)
		})

		By("Verifying initial compliance message")
		Eventually(func() interface{} {
			managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				policyName, testNamespace, true, defaultTimeoutSeconds)

			return utils.GetStatusMessage(managedPlc)
		}, defaultTimeoutSeconds, 1).Should(Equal(fmt.Sprintf(
			"fakeapis [case42-1-e2e] found but not as specified in namespace %[1]s; "+
				"fakeapis [case42-2-e2e] found but not as specified in namespace %[1]s", targetNs)))
	})

	It("should evaluate when a matching labeled resource is added", func(ctx SpecContext) {
		By("Creating additional matching object case42-3-e2e")
		utils.Kubectl("apply", "-n", targetNs, "-f", extraYaml)

		Eventually(func() interface{} {
			managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				policyName, testNamespace, true, defaultTimeoutSeconds)

			return utils.GetStatusMessage(managedPlc)
		}, defaultTimeoutSeconds, 1).Should(HaveSuffix(
			fmt.Sprintf("; fakeapis [case42-3-e2e] found but not as specified in namespace %s", targetNs)))
	})

	It("should not change when a non-matching resource is added", func(ctx SpecContext) {
		By("Creating additional matching object case42-4-e2e")
		utils.Kubectl("apply", "-n", targetNs, "-f", nomatchYaml)

		Consistently(func() interface{} {
			managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				policyName, testNamespace, true, defaultTimeoutSeconds)

			return utils.GetStatusMessage(managedPlc)
		}, defaultConsistentlyDuration, 1).ShouldNot(ContainSubstring("case42-4-e2e"))
	})

	It("should evaluate when a resource is labeled to match", func() {
		utils.Kubectl("label", "-n", targetNs, "fakeapi", "case42-4-e2e", "case42=case42-4-e2e")

		Eventually(func() interface{} {
			managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				policyName, testNamespace, true, defaultTimeoutSeconds)

			return utils.GetStatusMessage(managedPlc)
		}, defaultTimeoutSeconds, 1).Should(HaveSuffix(
			fmt.Sprintf("; fakeapis [case42-4-e2e] found but not as specified in namespace %s", targetNs)))
	})

	It("should evaluate when a matching resource label is removed", func() {
		utils.Kubectl("label", "-n", targetNs, "fakeapi", "case42-3-e2e", "case42-")

		Eventually(func() interface{} {
			managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				policyName, testNamespace, true, defaultTimeoutSeconds)

			return utils.GetStatusMessage(managedPlc)
		}, defaultTimeoutSeconds, 1).ShouldNot(ContainSubstring(
			fmt.Sprintf("fakeapis [case42-3-e2e] found but not as specified in namespace %s", targetNs)))
	})

	It("should become compliant when enforced", func() {
		utils.Kubectl("patch", "--namespace=managed", "configurationpolicy", policyName, "--type=json",
			`--patch=[{
				"op":"replace",
				"path":"/spec/remediationAction",
				"value":"enforce"
			}]`,
		)

		Eventually(func() interface{} {
			managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				policyName, testNamespace, true, defaultTimeoutSeconds)

			return utils.GetStatusMessage(managedPlc)
		}, defaultTimeoutSeconds, 1).Should(Equal(fmt.Sprintf(
			"fakeapis [case42-1-e2e] found as specified in namespace %[1]s; "+
				"fakeapis [case42-2-e2e] found as specified in namespace %[1]s; "+
				"fakeapis [case42-4-e2e] found as specified in namespace %[1]s", targetNs)))
	})

	It("should ignore the objectSelector when a name is provided", func() {
		utils.Kubectl("patch", "--namespace=managed", "configurationpolicy", policyName, "--type=json",
			`--patch=[{
				"op": "replace",
				"path":"/spec/object-templates/0/objectDefinition/metadata/name",
				"value":"case42-3-e2e"
			}]`,
		)

		Eventually(func() interface{} {
			managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				policyName, testNamespace, true, defaultTimeoutSeconds)

			return utils.GetStatusMessage(managedPlc)
		}, defaultTimeoutSeconds, 1).Should(Equal(fmt.Sprintf(
			"fakeapis [case42-3-e2e] found as specified in namespace %s", targetNs)))
	})
})
