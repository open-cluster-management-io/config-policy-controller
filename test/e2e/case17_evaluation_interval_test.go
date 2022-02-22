// Copyright (c) 2022 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package e2e

import (
	"context"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"open-cluster-management.io/config-policy-controller/test/utils"
)

const (
	case17policy               = "../resources/case17_evaluation_interval/policy.yaml"
	case17policyName           = "policy-c17-create-ns"
	case17policyNever          = "../resources/case17_evaluation_interval/policy-never-reevaluate.yaml"
	case17policyNeverName      = "policy-c17-create-ns-never"
	case17CreatedNamespaceName = "case17-test-never"
)

var _ = Describe("Test evaluation interval", func() {
	It("Verifies that status.lastEvaluated is properly set", func() {
		By("Creating " + case17policyName + " on the managed cluster")
		utils.Kubectl("apply", "-f", case17policy, "-n", testNamespace)
		plc := utils.GetWithTimeout(
			clientManagedDynamic, gvrConfigPolicy, case17policyName, testNamespace, true, defaultTimeoutSeconds,
		)
		Expect(plc).NotTo(BeNil())

		By("Getting status.lastEvaluated")
		var managedPlc *unstructured.Unstructured

		Eventually(func() interface{} {
			managedPlc = utils.GetWithTimeout(
				clientManagedDynamic, gvrConfigPolicy, case17policyName, testNamespace, true, defaultTimeoutSeconds,
			)

			lastEvaluated, _ := utils.GetLastEvaluated(managedPlc)

			return lastEvaluated
		}, defaultTimeoutSeconds, 1).ShouldNot(Equal(""))

		lastEvaluated, lastEvaluatedGeneration := utils.GetLastEvaluated(managedPlc)
		Expect(lastEvaluatedGeneration).To(Equal(managedPlc.GetGeneration()))

		lastEvaluatedParsed, err := time.Parse(time.RFC3339, lastEvaluated)
		Expect(err).To(BeNil())

		By("Waiting for status.lastEvaluated to refresh")
		Eventually(func() interface{} {
			managedPlc = utils.GetWithTimeout(
				clientManagedDynamic, gvrConfigPolicy, case17policyName, testNamespace, true, defaultTimeoutSeconds,
			)

			lastEvalRefreshed, _ := utils.GetLastEvaluated(managedPlc)

			return lastEvalRefreshed
		}, defaultTimeoutSeconds, 1).ShouldNot(Equal(lastEvaluated))

		lastEvalRefreshed, lastEvalGenerationRefreshed := utils.GetLastEvaluated(managedPlc)
		Expect(lastEvalGenerationRefreshed).To(Equal(lastEvaluatedGeneration))

		lastEvalRefreshedParsed, err := time.Parse(time.RFC3339, lastEvalRefreshed)
		Expect(err).To(BeNil())

		Expect(lastEvaluatedParsed.Before(lastEvalRefreshedParsed)).To(BeTrue())
	})

	It("Verifies that a compliant policy is not reevaluated when set to never", func() {
		By("Creating " + case17policyNeverName + " on the managed cluster")
		utils.Kubectl("apply", "-f", case17policyNever, "-n", testNamespace)
		plc := utils.GetWithTimeout(
			clientManagedDynamic, gvrConfigPolicy, case17policyNeverName, testNamespace, true, defaultTimeoutSeconds,
		)
		Expect(plc).NotTo(BeNil())

		By("Getting status.lastEvaluated")
		var managedPlc *unstructured.Unstructured

		Eventually(func() interface{} {
			managedPlc = utils.GetWithTimeout(
				clientManagedDynamic,
				gvrConfigPolicy,
				case17policyNeverName,
				testNamespace,
				true,
				defaultTimeoutSeconds,
			)

			return utils.GetComplianceState(managedPlc)
		}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))

		lastEvaluated, _ := utils.GetLastEvaluated(managedPlc)
		_, err := time.Parse(time.RFC3339, lastEvaluated)
		Expect(err).To(BeNil())

		By("Verifying that compliance message mentions it won't be reevaluated")
		msg, ok := utils.GetStatusMessage(managedPlc).(string)
		Expect(ok).To(BeTrue())

		expectedSuffix := `. This policy will not be evaluated again due to spec.evaluationInterval.compliant being ` +
			`set to "never".`
		Expect(strings.HasSuffix(msg, expectedSuffix)).To(BeTrue())

		By("Verifying that status.lastEvaluated will not change after waiting 15 seconds")
		time.Sleep(15 * time.Second)

		managedPlc = utils.GetWithTimeout(
			clientManagedDynamic, gvrConfigPolicy, case17policyNeverName, testNamespace, true, defaultTimeoutSeconds,
		)
		lastEvalRefreshed, _ := utils.GetLastEvaluated(managedPlc)
		Expect(lastEvalRefreshed).To(Equal(lastEvaluated))
	})

	It("Cleans up", func() {
		utils.Kubectl("delete", "-f", case17policy, "-n", testNamespace)
		utils.Kubectl("delete", "-f", case17policyNever, "-n", testNamespace)
		err := clientManaged.CoreV1().Namespaces().Delete(
			context.TODO(), case17CreatedNamespaceName, v1.DeleteOptions{},
		)
		if !k8serrors.IsNotFound(err) {
			Expect(err).To(BeNil())
		}
	})
})
