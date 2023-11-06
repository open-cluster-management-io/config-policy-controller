// Copyright Contributors to the Open Cluster Management project

package e2e

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"golang.org/x/mod/semver"
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

	var serverVersion string
	BeforeAll(func() {
		serverVersion = utils.GetServerVersion(clientManaged)
	})

	It("Fails when an invalid field is provided", func() {
		By("Creating the " + policyName + " policy")
		utils.Kubectl("apply", "-f", policyYAML, "-n", testNamespace)

		expectedMsg := "configmaps [case23] in namespace default is missing, and cannot be created, reason: " +
			"`ConfigMap in version \"v1\" cannot be handled as a ConfigMap: strict decoding error: unknown " +
			"field \"invalid\"`"

		// if the server version is less than 1.25.0, we use client side validation
		if semver.Compare(serverVersion, "v1.25.0") < 0 {
			expectedMsg = "configmaps [case23] in namespace default is missing, and cannot be created, reason: " +
				"`ValidationError(ConfigMap): unknown field \"invalid\" in io.k8s.api.core.v1.ConfigMap`"
		}

		By("Verifying that the " + policyName + " policy is noncompliant")
		Eventually(func(g Gomega) {
			managedPlc := utils.GetWithTimeout(
				clientManagedDynamic, gvrConfigPolicy, policyName, testNamespace, true, defaultTimeoutSeconds,
			)

			g.Expect(utils.GetComplianceState(managedPlc)).To(Equal("NonCompliant"))
			g.Expect(utils.GetStatusMessage(managedPlc)).To(Equal(expectedMsg))
		}, defaultTimeoutSeconds, 1).Should(Succeed())

		By("Verifying events do not continue to be created after the first violation for created objects")
		startTime := metav1.NewTime(time.Now())

		msg := "ConfigMap in version \"v1\" cannot be handled as a ConfigMap: " +
			"strict decoding error: unknown field \"invalid\""
		if semver.Compare(serverVersion, "v1.25.0") < 0 {
			msg = "unknown field \"invalid\" in io.k8s.api.core.v1.ConfigMap"
		}
		Consistently(func() interface{} {
			compPlcEvents := utils.GetMatchingEvents(clientManaged, testNamespace,
				policyName,
				"Policy updated",
				msg,
				defaultTimeoutSeconds)

			if len(compPlcEvents) == 0 {
				return false
			}

			return startTime.After(compPlcEvents[len(compPlcEvents)-1].LastTimestamp.Time)
		}, 30, 1).Should(BeTrue())

		By("Verifying the message is correct when the " + configMapName + " ConfigMap already exists")
		configmap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name: configMapName,
			},
		}
		_, err := clientManaged.CoreV1().ConfigMaps("default").Create(context.TODO(), configmap, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		expectedMsg = "Error validating the object case23, the error is `ConfigMap in version \"v1\"" +
			" cannot be handled as a ConfigMap: strict decoding error: unknown field \"invalid\"`"
		// if the server version is less than 1.25.0, we use client side validation
		if semver.Compare(serverVersion, "v1.25.0") < 0 {
			expectedMsg = "Error validating the object case23, the error is `ValidationError(ConfigMap): unknown " +
				"field \"invalid\" in io.k8s.api.core.v1.ConfigMap`"
		}
		Eventually(func() interface{} {
			managedPlc := utils.GetWithTimeout(
				clientManagedDynamic, gvrConfigPolicy, policyName, testNamespace, true, defaultTimeoutSeconds,
			)

			return utils.GetStatusMessage(managedPlc)
		}, defaultTimeoutSeconds, 1).Should(Equal(expectedMsg))

		By("Verifying events do not continue to be created after the first violation for existing objects")
		alreadyExistsStartTime := metav1.NewTime(time.Now())

		msg = "strict decoding error: unknown field \"invalid\""
		if semver.Compare(serverVersion, "v1.25.0") < 0 {
			msg = "unknown field \"invalid\" in io.k8s.api.core.v1.ConfigMap"
		}
		Consistently(func() interface{} {
			compPlcEvents := utils.GetMatchingEvents(clientManaged, testNamespace,
				policyName,
				"",
				msg,
				defaultTimeoutSeconds)

			if len(compPlcEvents) == 0 {
				return false
			}

			return alreadyExistsStartTime.After(compPlcEvents[len(compPlcEvents)-1].LastTimestamp.Time)
		}, 30, 1).Should(BeTrue())
	})

	AfterAll(func() {
		deleteConfigPolicies([]string{policyName})

		err := clientManaged.CoreV1().ConfigMaps("default").Delete(
			context.TODO(), configMapName, metav1.DeleteOptions{},
		)
		if !k8serrors.IsNotFound(err) {
			Expect(err).ToNot(HaveOccurred())
		}
	})
})

var _ = Describe("Test an objectDefinition with a missing status field that should be ignored", Ordered, func() {
	const (
		podName    = "nginx-case23-missing-status"
		policyName = "case23-pod-missing-status-fields"
		policyYAML = "../resources/case23_invalid_field/policy-ignore-status-field.yaml"
	)

	It("Still enforces when a status is provided but is missing a required field", func() {
		By("Creating the " + policyName + " policy")
		utils.Kubectl("apply", "-f", policyYAML, "-n", testNamespace)

		By("Verifying that the " + policyName + " policy is compliant")
		var managedPlc *unstructured.Unstructured

		Eventually(func() interface{} {
			managedPlc = utils.GetWithTimeout(
				clientManagedDynamic, gvrConfigPolicy, policyName, testNamespace, true, defaultTimeoutSeconds,
			)

			return utils.GetComplianceState(managedPlc)
		}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
	})

	AfterAll(func() {
		deleteConfigPolicies([]string{policyName})

		err := clientManaged.CoreV1().Pods("default").Delete(context.TODO(), podName, metav1.DeleteOptions{})
		if !k8serrors.IsNotFound(err) {
			Expect(err).ToNot(HaveOccurred())
		}
	})
})
