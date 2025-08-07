// Copyright Contributors to the Open Cluster Management project

package e2e

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"open-cluster-management.io/config-policy-controller/pkg/common"
	"open-cluster-management.io/config-policy-controller/pkg/triggeruninstall"
	"open-cluster-management.io/config-policy-controller/test/utils"
)

// This test only works when the controller is running in the cluster.
var _ = Describe("Clean up during uninstalls", Label("running-in-cluster"), Ordered, func() {
	const (
		configMapName        string = "case29-trigger-uninstall"
		deploymentName       string = "config-policy-controller"
		deploymentNamespace  string = "open-cluster-management-agent-addon"
		policyName           string = "case29-trigger-uninstall"
		policy2Name          string = "case29-trigger-uninstall2"
		policyYAMLPath       string = "../resources/case29_trigger_uninstall/policy.yaml"
		policy2YAMLPath      string = "../resources/case29_trigger_uninstall/policy2.yaml"
		pruneObjectFinalizer string = "policy.open-cluster-management.io/delete-related-objects"
	)

	It("verifies that finalizers are removed when being uninstalled", func() {
		By("Creating two configuration policies with pruneObjectBehavior")
		utils.Kubectl("apply", "-f", policyYAMLPath, "-n", testNamespace)
		utils.Kubectl("apply", "-f", policy2YAMLPath, "-n", testNamespace)

		By("Verifying that the configuration policies are compliant and have finalizers")
		Eventually(func(g Gomega) {
			policy := utils.GetWithTimeout(
				clientManagedDynamic, gvrConfigPolicy, policyName, testNamespace, true, defaultTimeoutSeconds,
			)
			g.Expect(utils.GetComplianceState(policy)).To(Equal("Compliant"))

			g.Expect(policy.GetFinalizers()).To(ContainElement(pruneObjectFinalizer))
		}, defaultTimeoutSeconds, 1).Should(Succeed())

		Eventually(func(g Gomega) {
			policy2 := utils.GetWithTimeout(
				clientManagedDynamic, gvrConfigPolicy, policy2Name, testNamespace, true, defaultTimeoutSeconds,
			)
			g.Expect(utils.GetComplianceState(policy2)).To(Equal("Compliant"))

			g.Expect(policy2.GetFinalizers()).To(ContainElement(pruneObjectFinalizer))
		}, defaultTimeoutSeconds, 1).Should(Succeed())

		By("Patching one of the policies to have an unresolved hub template")
		utils.Kubectl("patch", "configurationpolicy", "-n", testNamespace, policy2Name, "--type=json", "-p",
			`[{"op": "replace",
			   "path": "/spec/object-templates/0/objectDefinition/data/state",
			   "value": "{{hub something hub}}"}]`)

		By("Verifying that policy is now NonCompliant, and still has the finalizer")
		Eventually(func(g Gomega) {
			policy2 := utils.GetWithTimeout(
				clientManagedDynamic, gvrConfigPolicy, policy2Name, testNamespace, true, defaultTimeoutSeconds,
			)
			g.Expect(utils.GetComplianceState(policy2)).To(Equal("NonCompliant"))

			g.Expect(utils.GetStatusMessage(policy2)).To(
				ContainSubstring("governance-standalone-hub-templating addon must be enabled"))

			g.Expect(policy2.GetFinalizers()).To(ContainElement(pruneObjectFinalizer))
		}, defaultTimeoutSeconds, 1).Should(Succeed())

		By("Waiting 15 seconds to ensure there are no leftover reconciles")
		time.Sleep(15 * time.Second)

		By("Triggering an uninstall")
		config, err := LoadConfig("", kubeconfigManaged, "")
		Expect(err).ToNot(HaveOccurred())

		ctx, ctxCancel := context.WithDeadline(
			context.Background(),
			// Cancel the context after the default timeout seconds to avoid the test running forever if it doesn't
			// exit cleanly before then.
			time.Now().Add(time.Duration(defaultTimeoutSeconds)*time.Second),
		)
		defer ctxCancel()

		err = triggeruninstall.TriggerUninstall(
			ctx, config, deploymentName, deploymentNamespace, []string{testNamespace})
		Expect(err).ToNot(HaveOccurred())

		By("Verifying that the uninstall annotation was set on the Deployment")
		deployment, err := clientManaged.AppsV1().Deployments(deploymentNamespace).Get(
			context.TODO(), deploymentName, metav1.GetOptions{},
		)
		Expect(err).ToNot(HaveOccurred())
		Expect(deployment.GetAnnotations()).To(HaveKey(common.UninstallingAnnotation))

		By("Verifying that the ConfigurationPolicy finalizers have been removed")
		policy := utils.GetWithTimeout(
			clientManagedDynamic, gvrConfigPolicy, policyName, testNamespace, true, defaultTimeoutSeconds,
		)
		Expect(policy.GetFinalizers()).To(BeEmpty())

		policy2 := utils.GetWithTimeout(
			clientManagedDynamic, gvrConfigPolicy, policy2Name, testNamespace, true, defaultTimeoutSeconds,
		)
		Expect(policy2.GetFinalizers()).To(BeEmpty())
	})

	AfterAll(func() {
		deleteConfigPolicies([]string{policyName, policy2Name})

		err := clientManaged.CoreV1().ConfigMaps("default").Delete(
			context.TODO(), configMapName, metav1.DeleteOptions{},
		)
		if !k8serrors.IsNotFound(err) {
			Expect(err).ToNot(HaveOccurred())
		}

		// Use an eventually in case there are update conflicts and there needs to be a retry
		Eventually(func(g Gomega) {
			deployment, err := clientManaged.AppsV1().Deployments(deploymentNamespace).Get(
				context.TODO(), deploymentName, metav1.GetOptions{},
			)
			g.Expect(err).ToNot(HaveOccurred())

			annotations := deployment.GetAnnotations()
			if _, ok := annotations[common.UninstallingAnnotation]; !ok {
				return
			}

			delete(annotations, common.UninstallingAnnotation)
			deployment.SetAnnotations(annotations)

			_, err = clientManaged.AppsV1().Deployments(deploymentNamespace).Update(
				context.TODO(), deployment, metav1.UpdateOptions{},
			)
			g.Expect(err).ToNot(HaveOccurred())
		}, defaultTimeoutSeconds, 1).Should(Succeed())
	})
})
