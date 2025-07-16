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

	policyv1 "open-cluster-management.io/config-policy-controller/api/v1"
	"open-cluster-management.io/config-policy-controller/test/utils"
)

var _ = Describe("Test evaluation interval", Ordered, func() {
	const (
		case17ParentPolicyName     = "parent-policy-c17-create-ns"
		case17ParentPolicy         = "../resources/case17_evaluation_interval/parent-policy.yaml"
		case17Policy               = "../resources/case17_evaluation_interval/policy.yaml"
		case17PolicyName           = "policy-c17-create-ns"
		case17PolicyNever          = "../resources/case17_evaluation_interval/policy-never-reevaluate.yaml"
		case17PolicyNeverName      = "policy-c17-create-ns-never"
		case17CreatedNamespaceName = "case17-test-never"
	)

	It("Verifies that status.lastEvaluated is properly set", func() {
		createObjWithParent(case17ParentPolicy, case17ParentPolicyName,
			case17Policy, testNamespace, gvrPolicy, gvrConfigPolicy)

		By("Getting status.lastEvaluated")
		var managedPlc *unstructured.Unstructured

		Eventually(func() interface{} {
			managedPlc = utils.GetWithTimeout(
				clientManagedDynamic, gvrConfigPolicy, case17PolicyName, testNamespace, true, defaultTimeoutSeconds,
			)

			lastEvaluated, _ := utils.GetLastEvaluated(managedPlc)

			return lastEvaluated
		}, defaultTimeoutSeconds, 1).ShouldNot(Equal(""))

		lastEvaluated, lastEvaluatedGeneration := utils.GetLastEvaluated(managedPlc)
		Expect(lastEvaluatedGeneration).To(Equal(managedPlc.GetGeneration()))

		lastEvaluatedParsed, err := time.Parse(time.RFC3339, lastEvaluated)
		Expect(err).ToNot(HaveOccurred())

		By("Waiting for status.lastEvaluated to refresh")
		Eventually(func() interface{} {
			managedPlc = utils.GetWithTimeout(
				clientManagedDynamic, gvrConfigPolicy, case17PolicyName, testNamespace, true, defaultTimeoutSeconds,
			)

			lastEvalRefreshed, _ := utils.GetLastEvaluated(managedPlc)

			return lastEvalRefreshed
		}, defaultTimeoutSeconds, 1).ShouldNot(Equal(lastEvaluated))

		lastEvalRefreshed, lastEvalGenerationRefreshed := utils.GetLastEvaluated(managedPlc)
		Expect(lastEvalGenerationRefreshed).To(Equal(lastEvaluatedGeneration))

		lastEvalRefreshedParsed, err := time.Parse(time.RFC3339, lastEvalRefreshed)
		Expect(err).ToNot(HaveOccurred())

		Expect(lastEvaluatedParsed.Before(lastEvalRefreshedParsed)).To(BeTrue())

		By("Verifying that only one event was sent for the configuration policy")
		Eventually(func(g Gomega) {
			events := utils.GetMatchingEvents(
				clientManaged,
				testNamespace,
				case17PolicyName,
				"",
				"Policy status is NonCompliant",
				defaultTimeoutSeconds,
			)
			g.Expect(events).To(HaveLen(1))
			g.Expect(events[0].Count).To(Equal(int32(1)))
		}, defaultTimeoutSeconds, 1).Should(Succeed())

		By("Verifying that the policy status history is stable")
		currentEvents := utils.GetHistoryEvents(clientManagedDynamic, gvrConfigPolicy,
			case17PolicyName, testNamespace, "")
		Expect(currentEvents).ShouldNot(BeEmpty())

		Consistently(func() []policyv1.HistoryEvent {
			return utils.GetHistoryEvents(clientManagedDynamic, gvrConfigPolicy,
				case17PolicyName, testNamespace, "")
		}, defaultConsistentlyDuration, 5).Should(HaveLen(len(currentEvents)))

		By("Verifying that only one event was sent for the parent policy")
		Eventually(func(g Gomega) {
			parentEvents := utils.GetMatchingEvents(
				clientManaged,
				testNamespace,
				case17ParentPolicyName,
				"policy: "+testNamespace+"/"+case17PolicyName,
				"^NonCompliant;",
				defaultTimeoutSeconds,
			)
			g.Expect(parentEvents).To(HaveLen(1))
			g.Expect(parentEvents[0].Count).To(Equal(int32(1)))
		}, defaultTimeoutSeconds, 1).Should(Succeed())
	})

	It("Verifies that a compliant policy is not reevaluated when set to never", func() {
		By("Creating " + case17PolicyNeverName + " on the managed cluster")
		utils.Kubectl("apply", "-f", case17PolicyNever, "-n", testNamespace)
		plc := utils.GetWithTimeout(
			clientManagedDynamic, gvrConfigPolicy, case17PolicyNeverName, testNamespace, true, defaultTimeoutSeconds,
		)
		Expect(plc).NotTo(BeNil())

		By("Getting status.lastEvaluated")
		var managedPlc *unstructured.Unstructured

		Eventually(func(g Gomega) {
			managedPlc = utils.GetWithTimeout(
				clientManagedDynamic,
				gvrConfigPolicy,
				case17PolicyNeverName,
				testNamespace,
				true,
				defaultTimeoutSeconds,
			)

			utils.CheckComplianceStatus(g, managedPlc, "Compliant")
		}, defaultTimeoutSeconds, 1).Should(Succeed())

		lastEvaluated, _ := utils.GetLastEvaluated(managedPlc)
		_, err := time.Parse(time.RFC3339, lastEvaluated)
		Expect(err).ToNot(HaveOccurred())

		By("Verifying that compliance message mentions it won't be reevaluated")
		msg, ok := utils.GetStatusMessage(managedPlc).(string)
		Expect(ok).To(BeTrue())

		expectedSuffix := `. This policy will not be evaluated again due to spec.evaluationInterval.compliant being ` +
			`set to "never".`
		Expect(strings.HasSuffix(msg, expectedSuffix)).To(BeTrue())

		By("Verifying that status.lastEvaluated will not change after waiting 15 seconds")
		time.Sleep(15 * time.Second)

		managedPlc = utils.GetWithTimeout(
			clientManagedDynamic, gvrConfigPolicy, case17PolicyNeverName, testNamespace, true, defaultTimeoutSeconds,
		)
		lastEvalRefreshed, _ := utils.GetLastEvaluated(managedPlc)
		Expect(lastEvalRefreshed).To(Equal(lastEvaluated))
	})

	AfterAll(func() {
		utils.KubectlDelete("-f", case17ParentPolicy, "-n", testNamespace)
		utils.KubectlDelete("-f", case17Policy, "-n", testNamespace)
		utils.KubectlDelete("-f", case17PolicyNever, "-n", testNamespace)

		err := clientManaged.CoreV1().Namespaces().Delete(
			context.TODO(), case17CreatedNamespaceName, v1.DeleteOptions{},
		)
		if !k8serrors.IsNotFound(err) {
			Expect(err).ToNot(HaveOccurred())
		}

		events, err := clientManaged.CoreV1().Events(testNamespace).List(context.TODO(), v1.ListOptions{})
		Expect(err).ToNot(HaveOccurred())

		for _, event := range events.Items {
			name := event.GetName()

			if strings.HasPrefix(name, case17ParentPolicyName) ||
				strings.HasPrefix(name, case17PolicyName) ||
				strings.HasPrefix(name, case17PolicyNeverName) {
				err = clientManaged.CoreV1().Events(testNamespace).Delete(context.TODO(), name, v1.DeleteOptions{})
				Expect(err).ToNot(HaveOccurred())
			}
		}
	})
})
