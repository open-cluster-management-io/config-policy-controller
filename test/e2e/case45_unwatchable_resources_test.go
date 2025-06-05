// Copyright Contributors to the Open Cluster Management project

package e2e

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"open-cluster-management.io/config-policy-controller/test/utils"
)

var _ = Describe("Testing behavior with unwatchable resources", Ordered, func() {
	const (
		policyName       = "case45"
		policyYAML       = "../resources/case45_unwatchable/config-policy.yaml"
		policyWithEval   = "../resources/case45_unwatchable/config-policy-5s.yaml"
		configmapName    = "case45"
		parentPolicyName = "case45-parent"
		parentPolicyYAML = "../resources/case45_unwatchable/parent-policy.yaml"
	)

	AfterAll(func() {
		deleteConfigPolicies([]string{policyName})

		err := clientManagedDynamic.Resource(gvrPolicy).Namespace(testNamespace).Delete(
			context.TODO(), parentPolicyName, metav1.DeleteOptions{},
		)
		if !errors.IsNotFound(err) {
			Expect(err).ToNot(HaveOccurred())
		}

		err = clientManagedDynamic.Resource(gvrConfigMap).Namespace("default").Delete(
			context.TODO(), configmapName, metav1.DeleteOptions{},
		)
		if !errors.IsNotFound(err) {
			Expect(err).ToNot(HaveOccurred())
		}
	})

	It("should be noncompliant under the default (watch) evaluationInterval", func() {
		createObjWithParent(parentPolicyYAML, parentPolicyName, policyYAML, testNamespace, gvrPolicy, gvrConfigPolicy)

		By("Verifying that the " + policyName + " policy is noncompliant")
		Eventually(func(g Gomega) {
			managedPlc := utils.GetWithTimeout(
				clientManagedDynamic, gvrConfigPolicy, policyName, testNamespace, true, defaultTimeoutSeconds,
			)

			utils.CheckComplianceStatus(g, managedPlc, "NonCompliant")
		}, defaultTimeoutSeconds, 1).Should(Succeed())

		By("Verifying that compliance message mentions that evaluationInterval should be set")
		tmpl1msg := `violation - Error with object-template at index \[0\], ` +
			"it may require evaluationInterval to be set.*"
		tmpl2msg := "violation - failed to resolve the template .* watch not supported on this resource " +
			"- this template may require evaluationInterval to be set"
		desiredMessage := "NonCompliant; " + tmpl1msg + tmpl2msg
		Eventually(func() []v1.Event {
			return utils.GetMatchingEvents(clientManaged, testNamespace, parentPolicyName,
				fmt.Sprintf("policy: %v/%v", testNamespace, policyName), desiredMessage, defaultTimeoutSeconds)
		}, defaultTimeoutSeconds, 1).ShouldNot(BeEmpty())
	})

	It("should have a successful evaluation when evaluationInterval is set", func() {
		utils.Kubectl("apply", "-f", policyWithEval, "-n", testNamespace)

		By("Verifying that the compliance message has changed")
		tmpl1msg := `notification - packagemanifests \[example-operator\] found as specified in namespace default`
		tmpl2msg := `violation - configmaps \[case45\] not found in namespace default`
		desiredMessage := "NonCompliant; " + tmpl1msg + "; " + tmpl2msg

		Eventually(func() []v1.Event {
			return utils.GetMatchingEvents(clientManaged, testNamespace, parentPolicyName,
				fmt.Sprintf("policy: %v/%v", testNamespace, policyName), desiredMessage, defaultTimeoutSeconds)
		}, defaultTimeoutSeconds, 1).ShouldNot(BeEmpty())
	})

	It("should become compliant when enforced", func() {
		utils.EnforceConfigurationPolicy(policyName, testNamespace)

		By("Verifying that the policy becomes compliant")
		Eventually(func(g Gomega) {
			managedPlc := utils.GetWithTimeout(
				clientManagedDynamic, gvrConfigPolicy, policyName, testNamespace, true, defaultTimeoutSeconds,
			)

			utils.CheckComplianceStatus(g, managedPlc, "NonCompliant")
		}, defaultTimeoutSeconds, 1).Should(Succeed())

		By("Verifying the content of the configmap")
		Eventually(func(g Gomega) {
			cm := utils.GetWithTimeout(
				clientManagedDynamic, gvrConfigMap, policyName, "default", true, defaultTimeoutSeconds,
			)

			val, found, err := unstructured.NestedString(cm.Object, "data", "src")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(found).To(BeTrue())
			g.Expect(val).To(Equal("grc-mock-source"))
		}, defaultTimeoutSeconds, 1).Should(Succeed())
	})
})
