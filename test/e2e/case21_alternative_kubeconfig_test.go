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

	"open-cluster-management.io/config-policy-controller/test/utils"
)

var _ = Describe("Test an alternative kubeconfig for policy evaluation", Ordered, Label("hosted-mode"), func() {
	const (
		namespaceName    = "e2e-test-ns"
		policyName       = "create-ns"
		policyYAML       = "../resources/case21_alternative_kubeconfig/policy.yaml"
		parentPolicyName = "parent-create-ns"
		parentPolicyYAML = "../resources/case21_alternative_kubeconfig/parent-policy.yaml"
	)

	AfterAll(func() {
		deleteConfigPolicies([]string{policyName})

		err := clientManagedDynamic.Resource(gvrPolicy).Namespace(testNamespace).Delete(
			context.TODO(), parentPolicyName, metav1.DeleteOptions{},
		)
		if !errors.IsNotFound(err) {
			Expect(err).ToNot(HaveOccurred())
		}

		err = targetK8sClient.CoreV1().Namespaces().Delete(context.TODO(), namespaceName, metav1.DeleteOptions{})
		if !errors.IsNotFound(err) {
			Expect(err).ToNot(HaveOccurred())
		}
	})

	It("should create the namespace using the alternative kubeconfig", func() {
		createObjWithParent(parentPolicyYAML, parentPolicyName, policyYAML, testNamespace, gvrPolicy, gvrConfigPolicy)

		By("Verifying that the " + policyName + " policy is compliant")
		Eventually(func() interface{} {
			managedPlc := utils.GetWithTimeout(
				clientManagedDynamic, gvrConfigPolicy, policyName, testNamespace, true, defaultTimeoutSeconds,
			)

			return utils.GetComplianceState(managedPlc)
		}, defaultTimeoutSeconds*2, 1).Should(Equal("Compliant"))

		By("Verifying that the " + policyName + " was created using the alternative kubeconfig")
		_, err := targetK8sClient.CoreV1().Namespaces().Get(context.TODO(), namespaceName, metav1.GetOptions{})
		Expect(err).ToNot(HaveOccurred())

		By("Verifying that a compliance event was created on the parent policy")
		Eventually(func() []v1.Event {
			return utils.GetMatchingEvents(clientManaged, testNamespace, parentPolicyName,
				fmt.Sprintf("policy: %v/%v", testNamespace, policyName), "^Compliant;", defaultTimeoutSeconds)
		}, defaultTimeoutSeconds, 1).ShouldNot(BeEmpty())
	})
})
