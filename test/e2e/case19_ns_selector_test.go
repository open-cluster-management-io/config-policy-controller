// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package e2e

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"open-cluster-management.io/config-policy-controller/test/utils"
)

var _ = Describe("Test results of namespace selection", Ordered, func() {
	const (
		prereqYaml string = "../resources/case19_ns_selector/case19_results_prereq.yaml"
		policyYaml string = "../resources/case19_ns_selector/case19_results_policy.yaml"
		policyName string = "selector-results-e2e"

		noMatchesMsg string = "namespaced object configmap-selector-e2e of kind ConfigMap has no " +
			"namespace specified from the policy namespaceSelector nor the object metadata"
		notFoundMsgFmt  string = "configmaps [configmap-selector-e2e] not found in namespaces: %s"
		filterErrMsgFmt string = "Error filtering namespaces with provided namespaceSelector: %s"
	)

	// Test setup for namespace selection policy tests:
	// - Namespaces `case19a-[1-5]-e2e`, each with a `case19a: <ns-name>` label
	// - Single deployed Configmap `configmap-selector-e2e` in namespace `case19a-1-e2e`
	// - Deployed policy should be compliant since it matches the single deployed ConfigMap
	// - Policies are patched so that the namespace doesn't match and should be NonCompliant
	BeforeAll(func() {
		By("Applying prerequisites")
		utils.Kubectl("apply", "-f", prereqYaml)
		DeferCleanup(func() {
			utils.Kubectl("delete", "-f", prereqYaml)
		})

		utils.Kubectl("apply", "-f", policyYaml, "-n", testNamespace)
		DeferCleanup(func() {
			utils.Kubectl("delete", "-f", policyYaml, "-n", testNamespace)
		})
	})

	DescribeTable("Checking results of different namespaceSelectors", func(patch string, message string) {
		patchFmt := `--patch=[{"op":"replace","path":"/spec/namespaceSelector","value":%s}]`

		By("patching policy with the test selector")
		utils.Kubectl("patch", "--namespace=managed", "configurationpolicy", policyName, "--type=json",
			fmt.Sprintf(patchFmt, patch),
		)
		Eventually(func() interface{} {
			managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				policyName, testNamespace, true, defaultTimeoutSeconds)

			return utils.GetStatusMessage(managedPlc)
		}, defaultTimeoutSeconds, 1).Should(Equal(message))
	},
		Entry("No namespaceSelector specified",
			"{}",
			noMatchesMsg),
		Entry("LabelSelector and exclude",
			`{"exclude":["*19a-[3-4]-e2e"],"matchExpressions":[{"key":"case19a","operator":"Exists"}]}`,
			fmt.Sprintf(notFoundMsgFmt, "case19a-2-e2e, case19a-5-e2e"),
		),
		Entry("A non-matching LabelSelector",
			`{"matchLabels":{"name":"not-a-namespace"}}`,
			noMatchesMsg),
		Entry("Empty LabelSelector and include/exclude",
			`{"include":["case19a-[2-5]-e2e"],"exclude":["*-[3-4]-e2e"],"matchLabels":{},"matchExpressions":[]}`,
			fmt.Sprintf(notFoundMsgFmt, "case19a-2-e2e, case19a-5-e2e"),
		),
		Entry("LabelSelector",
			`{"matchExpressions":[{"key":"case19a","operator":"Exists"}]}`,
			fmt.Sprintf(notFoundMsgFmt, "case19a-2-e2e, case19a-3-e2e, case19a-4-e2e, case19a-5-e2e"),
		),
		Entry("Malformed filepath in include",
			`{"include":["*-[a-z-*"]}`,
			fmt.Sprintf(filterErrMsgFmt, "error parsing 'include' pattern '*-[a-z-*': syntax error in pattern"),
		),
		Entry("MatchExpressions with incorrect operator",
			`{"matchExpressions":[{"key":"name","operator":"Seriously"}]}`,
			fmt.Sprintf(filterErrMsgFmt, "error parsing namespace LabelSelector: "+
				`"Seriously" is not a valid label selector operator`),
		),
		Entry("MatchExpressions with missing values",
			`{"matchExpressions":[{"key":"name","operator":"In","values":[]}]}`,
			fmt.Sprintf(filterErrMsgFmt, "error parsing namespace LabelSelector: "+
				"values: Invalid value: []string(nil): for 'in', 'notin' operators, values set can't be empty"),
		),
	)
})

var _ = Describe("Test behavior of namespace selection as namespaces change", Ordered, Label("jkulikau"), func() {
	const (
		prereqYaml string = "../resources/case19_ns_selector/case19_behavior_prereq.yaml"
		policyYaml string = "../resources/case19_ns_selector/case19_behavior_policy.yaml"
		policyName string = "selector-behavior-e2e"

		notFoundMsgFmt string = "configmaps [configmap-selector-e2e] not found in namespaces: %s"
	)

	BeforeAll(func() {
		By("Applying prerequisites")
		utils.Kubectl("apply", "-f", prereqYaml)
		// cleaned up in an AfterAll because that will cover other namespaces created in the tests

		utils.Kubectl("apply", "-f", policyYaml, "-n", testNamespace)
		DeferCleanup(func() {
			utils.Kubectl("delete", "-f", policyYaml, "-n", testNamespace)
		})

		By("Verifying initial compliance message")
		Eventually(func() interface{} {
			managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				policyName, testNamespace, true, defaultTimeoutSeconds)

			return utils.GetStatusMessage(managedPlc)
		}, defaultTimeoutSeconds, 1).Should(Equal(fmt.Sprintf(notFoundMsgFmt,
			"case19b-1-e2e, case19b-2-e2e")))
	})

	AfterAll(func() {
		utils.Kubectl("delete", "ns", "case19b-1-e2e", "--ignore-not-found")
		utils.Kubectl("delete", "ns", "case19b-2-e2e", "--ignore-not-found")
		utils.Kubectl("delete", "ns", "case19b-3-e2e", "--ignore-not-found")
		utils.Kubectl("delete", "ns", "case19b-4-e2e", "--ignore-not-found")
		utils.Kubectl("delete", "ns", "kube-case19b-e2e", "--ignore-not-found")
	})

	It("should evaluate when a matching labeled namespace is added", func() {
		_, err := clientManaged.CoreV1().Namespaces().Create(context.TODO(), &v1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "case19b-3-e2e",
				Labels: map[string]string{
					"case19b": "case19b-3-e2e",
				},
			},
		}, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		Eventually(func() interface{} {
			managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				policyName, testNamespace, true, defaultTimeoutSeconds)

			return utils.GetStatusMessage(managedPlc)
		}, defaultTimeoutSeconds, 1).Should(Equal(fmt.Sprintf(notFoundMsgFmt,
			"case19b-1-e2e, case19b-2-e2e, case19b-3-e2e")))
	})

	It("should not evaluate early if a non-matching namespace is added", func() {
		managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
			policyName, testNamespace, true, defaultTimeoutSeconds)

		evalTime, found, err := unstructured.NestedString(managedPlc.Object, "status", "lastEvaluated")
		Expect(evalTime).ToNot(BeEmpty())
		Expect(found).To(BeTrue())
		Expect(err).ToNot(HaveOccurred())

		clientManaged.CoreV1().Namespaces().Create(context.TODO(), &v1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: "case19b-4-e2e"},
		}, metav1.CreateOptions{})

		Consistently(func() interface{} {
			managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				policyName, testNamespace, true, defaultTimeoutSeconds)

			newEvalTime, found, err := unstructured.NestedString(managedPlc.Object, "status", "lastEvaluated")
			Expect(newEvalTime).ToNot(BeEmpty())
			Expect(found).To(BeTrue())
			Expect(err).ToNot(HaveOccurred())

			return newEvalTime
		}, "20s", 1).Should(Equal(evalTime))
	})

	It("should evaluate when a namespace is labeled to match", func() {
		utils.Kubectl("label", "ns", "case19b-4-e2e", "case19b=case19b-4-e2e")

		Eventually(func() interface{} {
			managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				policyName, testNamespace, true, defaultTimeoutSeconds)

			return utils.GetStatusMessage(managedPlc)
		}, defaultTimeoutSeconds, 1).Should(Equal(fmt.Sprintf(notFoundMsgFmt,
			"case19b-1-e2e, case19b-2-e2e, case19b-3-e2e, case19b-4-e2e")))
	})

	It("should evaluate when a matching namespace label is removed", func() {
		utils.Kubectl("label", "ns", "case19b-3-e2e", "case19b-")

		Eventually(func() interface{} {
			managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				policyName, testNamespace, true, defaultTimeoutSeconds)

			return utils.GetStatusMessage(managedPlc)
		}, defaultTimeoutSeconds, 1).Should(Equal(fmt.Sprintf(notFoundMsgFmt,
			"case19b-1-e2e, case19b-2-e2e, case19b-4-e2e")))
	})

	It("should evaluate when an excluded namespace is added", Label("buggy-behavior"), func() {
		managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
			policyName, testNamespace, true, defaultTimeoutSeconds)

		evalTime, found, err := unstructured.NestedString(managedPlc.Object, "status", "lastEvaluated")
		Expect(evalTime).ToNot(BeEmpty())
		Expect(found).To(BeTrue())
		Expect(err).ToNot(HaveOccurred())

		clientManaged.CoreV1().Namespaces().Create(context.TODO(), &v1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "kube-case19b-e2e",
				Labels: map[string]string{
					"case19b": "kube-case19b-e2e",
				},
			},
		}, metav1.CreateOptions{})

		Eventually(func() interface{} {
			managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				policyName, testNamespace, true, defaultTimeoutSeconds)

			newEvalTime, found, err := unstructured.NestedString(managedPlc.Object, "status", "lastEvaluated")
			Expect(newEvalTime).ToNot(BeEmpty())
			Expect(found).To(BeTrue())
			Expect(err).ToNot(HaveOccurred())

			return newEvalTime
		}, defaultTimeoutSeconds, 1).ShouldNot(Equal(evalTime))
	})

	It("should evaluate when a matched namespace is changed", Label("buggy-behavior"), func() {
		managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
			policyName, testNamespace, true, defaultTimeoutSeconds)

		evalTime, found, err := unstructured.NestedString(managedPlc.Object, "status", "lastEvaluated")
		Expect(evalTime).ToNot(BeEmpty())
		Expect(found).To(BeTrue())
		Expect(err).ToNot(HaveOccurred())

		utils.Kubectl("label", "ns", "case19b-1-e2e", "extra-label=hello")

		Eventually(func() interface{} {
			managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				policyName, testNamespace, true, defaultTimeoutSeconds)

			newEvalTime, found, err := unstructured.NestedString(managedPlc.Object, "status", "lastEvaluated")
			Expect(newEvalTime).ToNot(BeEmpty())
			Expect(found).To(BeTrue())
			Expect(err).ToNot(HaveOccurred())

			return newEvalTime
		}, defaultTimeoutSeconds, 1).ShouldNot(Equal(evalTime))
	})
})
