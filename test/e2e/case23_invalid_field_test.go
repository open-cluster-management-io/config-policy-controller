// Copyright Contributors to the Open Cluster Management project

package e2e

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"open-cluster-management.io/config-policy-controller/test/utils"
)

var _ = Describe("Test an objectDefinition with an invalid field", Ordered, func() {
	const (
		configMapName = "case23"
		policyName    = "case23-invalid-field"
		policyYAML    = "../resources/case23_invalid_field/policy.yaml"
	)

	It("Fails when an invalid field is provided", func() {
		By("Creating the " + policyName + " policy")
		utils.Kubectl("apply", "-f", policyYAML, "-n", testNamespace)

		By("Verifying that the " + policyName + " policy is noncompliant")
		var managedPlc *unstructured.Unstructured

		Eventually(func() interface{} {
			managedPlc = utils.GetWithTimeout(
				clientManagedDynamic, gvrConfigPolicy, policyName, testNamespace, true, defaultTimeoutSeconds,
			)

			return utils.GetComplianceState(managedPlc)
		}, defaultTimeoutSeconds, 1).Should(Equal("NonCompliant"))

		// expectedMsg := "Error validating the object case23, the error is `ValidationError(ConfigMap): unknown " +
		// 	"field \"invalid\" in io.k8s.api.core.v1.ConfigMap`"
		expectedMsg := "configmaps [case23] in namespace default is missing, and cannot be created, reason: " +
			"`ValidationError(ConfigMap): unknown field \"invalid\" in io.k8s.api.core.v1.ConfigMap`"
		Expect(utils.GetStatusMessage(managedPlc)).To(Equal(expectedMsg))

		By("Verifying the message is correct when the " + configMapName + " ConfigMap already exists")
		configmap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name: configMapName,
			},
		}
		_, err := clientManaged.CoreV1().ConfigMaps("default").Create(context.TODO(), configmap, metav1.CreateOptions{})
		Expect(err).To(BeNil())

		expectedMsg = "Error validating the object case23, the error is `ValidationError(ConfigMap): unknown " +
			"field \"invalid\" in io.k8s.api.core.v1.ConfigMap`"
		Eventually(func() interface{} {
			managedPlc = utils.GetWithTimeout(
				clientManagedDynamic, gvrConfigPolicy, policyName, testNamespace, true, defaultTimeoutSeconds,
			)

			return utils.GetStatusMessage(managedPlc)
		}, defaultTimeoutSeconds, 1).Should(Equal(expectedMsg))
	})

	AfterAll(func() {
		deleteConfigPolicies([]string{policyName})

		err := clientManaged.CoreV1().ConfigMaps("default").Delete(
			context.TODO(), configMapName, metav1.DeleteOptions{},
		)
		if !k8serrors.IsNotFound(err) {
			Expect(err).To(BeNil())
		}
	})
})
