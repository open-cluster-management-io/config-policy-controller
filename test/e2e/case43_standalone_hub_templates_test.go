// Copyright Contributors to the Open Cluster Management project

package e2e

import (
	"context"
	"fmt"
	"os"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"open-cluster-management.io/config-policy-controller/test/utils"
)

var _ = Describe("Testing behavior when standalone-hub-templates are not enabled", Ordered, func() {
	const (
		policyName       = "case43-with-hub-template"
		policyYAML       = "../resources/case43_standalone_hub_templates/config-policy.yaml"
		parentPolicyName = "case43-parent"
		parentPolicyYAML = "../resources/case43_standalone_hub_templates/parent-policy.yaml"
	)

	AfterAll(func() {
		deleteConfigPolicies([]string{policyName})

		err := clientManagedDynamic.Resource(gvrPolicy).Namespace(testNamespace).Delete(
			context.TODO(), parentPolicyName, metav1.DeleteOptions{},
		)
		if !errors.IsNotFound(err) {
			Expect(err).ToNot(HaveOccurred())
		}
	})

	It("should be noncompliant", func() {
		createObjWithParent(parentPolicyYAML, parentPolicyName, policyYAML, testNamespace, gvrPolicy, gvrConfigPolicy)

		By("Verifying that the " + policyName + " policy is noncompliant")
		Eventually(func(g Gomega) {
			managedPlc := utils.GetWithTimeout(
				clientManagedDynamic, gvrConfigPolicy, policyName, testNamespace, true, defaultTimeoutSeconds,
			)

			utils.CheckComplianceStatus(g, managedPlc, "NonCompliant")
		}, defaultTimeoutSeconds*2, 1).Should(Succeed())

		By("Verifying that the message warns the user that hub templates are not currently allowed")
		desiredMessage := "NonCompliant; violation - the governance-standalone-hub-templating addon " +
			"must be enabled to resolve hub templates on the managed cluster"
		Eventually(func() []string {
			return utils.GetHistoryMessages(clientManagedDynamic, gvrConfigPolicy,
				policyName, testNamespace, "")
		}, defaultTimeoutSeconds, 1).Should(ContainElement(ContainSubstring(desiredMessage)))
		Eventually(func() []v1.Event {
			return utils.GetMatchingEvents(clientManaged, testNamespace, parentPolicyName,
				fmt.Sprintf("policy: %v/%v", testNamespace, policyName), desiredMessage, defaultTimeoutSeconds)
		}, defaultTimeoutSeconds, 1).ShouldNot(BeEmpty())
	})
})

var _ = Describe("When standalone-hub-templates is enabled", Ordered, Label("hub-templates-enabled"), func() {
	KubectlHub := func(args ...string) {
		if hubTmplKubeconfig := os.Getenv("HUB_TEMPLATES_KUBECONFIG_PATH"); hubTmplKubeconfig != "" {
			args = append(args, "--kubeconfig="+hubTmplKubeconfig)

			utils.Kubectl(args...)
		} else {
			Fail("HUB_TEMPLATES_KUBECONFIG_PATH not configured")
		}
	}

	const (
		parentPolicyName = "case43-parent"
		parentPolicyYAML = "../resources/case43_standalone_hub_templates/parent-policy.yaml"
		hubConfigmapName = "case43-input"
		hubConfigmapYAML = "../resources/case43_standalone_hub_templates/hub-configmap.yaml"
	)

	Describe("Test ConfigurationPolicy", Ordered, func() {
		const (
			policyName   = "case43-with-hub-template"
			policyYAML   = "../resources/case43_standalone_hub_templates/config-policy.yaml"
			modifiedYAML = "../resources/case43_standalone_hub_templates/config-policy-modified.yaml"
		)

		var initialCacheContent string

		AfterAll(func() {
			deleteConfigPolicies([]string{policyName})

			err := clientManagedDynamic.Resource(gvrPolicy).Namespace(testNamespace).Delete(
				context.TODO(), parentPolicyName, metav1.DeleteOptions{},
			)
			if !errors.IsNotFound(err) {
				Expect(err).ToNot(HaveOccurred())
			}

			KubectlHub("delete", "configmap", hubConfigmapName, "--namespace=default", "--ignore-not-found")
			utils.Kubectl("delete", "events", "--namespace="+testNamespace,
				"--field-selector=involvedObject.name="+parentPolicyName)
		})

		It("should be noncompliant when the hub configmap doesn't exist", func() {
			createObjWithParent(parentPolicyYAML, parentPolicyName, policyYAML,
				testNamespace, gvrPolicy, gvrConfigPolicy)

			By("Verifying that the " + policyName + " policy is noncompliant")
			Eventually(func(g Gomega) {
				managedPlc := utils.GetWithTimeout(
					clientManagedDynamic, gvrConfigPolicy, policyName, testNamespace, true, defaultTimeoutSeconds,
				)

				utils.CheckComplianceStatus(g, managedPlc, "NonCompliant")
			}, defaultTimeoutSeconds, 1).Should(Succeed())

			By("Checking the compliance message")
			desiredMessage := "NonCompliant; violation - failed to resolve the template"
			Eventually(func() []string {
				return utils.GetHistoryMessages(clientManagedDynamic, gvrConfigPolicy,
					policyName, testNamespace, "")
			}, defaultTimeoutSeconds, 1).Should(ContainElement(ContainSubstring(desiredMessage)))
			Eventually(func() []v1.Event {
				return utils.GetMatchingEvents(clientManaged, testNamespace, parentPolicyName,
					fmt.Sprintf("policy: %v/%v", testNamespace, policyName), desiredMessage, defaultTimeoutSeconds)
			}, defaultTimeoutSeconds, 1).ShouldNot(BeEmpty())
		})

		It("should become compliant after the hub configmap is created", func() {
			By("Waiting 3 seconds to ensure reconciles continue retrying...")
			time.Sleep(3 * time.Second)

			KubectlHub("apply", "-f", hubConfigmapYAML)

			By("Verifying that the " + policyName + " policy is compliant")
			Eventually(func(g Gomega) {
				managedPlc := utils.GetWithTimeout(
					clientManagedDynamic, gvrConfigPolicy, policyName, testNamespace, true, defaultTimeoutSeconds,
				)

				utils.CheckComplianceStatus(g, managedPlc, "Compliant")
			}, defaultTimeoutSeconds, 1).Should(Succeed())

			By("Checking the compliance message")
			desiredMessage := "Compliant; notification - configmaps .* found as specified"
			Eventually(func() []string {
				return utils.GetHistoryMessages(clientManagedDynamic, gvrConfigPolicy,
					policyName, testNamespace, "")
			}, defaultTimeoutSeconds, 1).Should(ContainElement(MatchRegexp(desiredMessage)))
			Eventually(func() []v1.Event {
				return utils.GetMatchingEvents(clientManaged, testNamespace, parentPolicyName,
					fmt.Sprintf("policy: %v/%v", testNamespace, policyName), desiredMessage, defaultTimeoutSeconds)
			}, defaultTimeoutSeconds, 1).ShouldNot(BeEmpty())
		})

		It("should have created the cache-secret to cache the hub result", func() {
			configPolicy := utils.GetWithTimeout(
				clientManagedDynamic, gvrConfigPolicy, policyName, testNamespace, true, defaultTimeoutSeconds,
			)

			secretName := string(configPolicy.GetUID()) + "-last-resolved"
			cacheSecret := utils.GetWithTimeout(
				clientManagedDynamic, gvrSecret, secretName, testNamespace, true, defaultTimeoutSeconds,
			)

			initialCacheContent, _, _ = unstructured.NestedString(cacheSecret.Object, "data", "policy.json")
			Expect(initialCacheContent).ToNot(BeEmpty())
		})

		It("should update the cache-secret when the template changes", func() {
			utils.Kubectl("apply", "-n", testNamespace, "-f", modifiedYAML)

			configPolicy := utils.GetWithTimeout(
				clientManagedDynamic, gvrConfigPolicy, policyName, testNamespace, true, defaultTimeoutSeconds,
			)

			secretName := string(configPolicy.GetUID()) + "-last-resolved"

			Eventually(func() string {
				cacheSecret := utils.GetWithTimeout(
					clientManagedDynamic, gvrSecret, secretName, testNamespace, true, defaultTimeoutSeconds,
				)

				cachedPolicy, _, _ := unstructured.NestedString(cacheSecret.Object, "data", "policy.json")

				return cachedPolicy
			}, defaultTimeoutSeconds, 1).ShouldNot(Equal(initialCacheContent))
		})

		It("should remove the cache-secret when hub is accessible and the template has an error", func() {
			KubectlHub("delete", "configmap", hubConfigmapName, "--namespace=default")

			configPolicy := utils.GetWithTimeout(
				clientManagedDynamic, gvrConfigPolicy, policyName, testNamespace, true, defaultTimeoutSeconds,
			)

			secretName := string(configPolicy.GetUID()) + "-last-resolved"
			utils.GetWithTimeout(
				clientManagedDynamic, gvrSecret, secretName, testNamespace, false, defaultTimeoutSeconds,
			)
		})

		It("should clean up the secret when the configuration policy is deleted", func() {
			By("recreating the hub configmap")
			KubectlHub("apply", "-f", hubConfigmapYAML)

			By("Verifying that the " + policyName + " policy is compliant again")
			Eventually(func(g Gomega) {
				managedPlc := utils.GetWithTimeout(
					clientManagedDynamic, gvrConfigPolicy, policyName, testNamespace, true, defaultTimeoutSeconds,
				)

				utils.CheckComplianceStatus(g, managedPlc, "Compliant")
			}, defaultTimeoutSeconds, 1).Should(Succeed())

			configPolicy := utils.GetWithTimeout(
				clientManagedDynamic, gvrConfigPolicy, policyName, testNamespace, true, defaultTimeoutSeconds,
			)

			By("Verifying the secret was created again")
			secretName := string(configPolicy.GetUID()) + "-last-resolved"
			utils.GetWithTimeout(
				clientManagedDynamic, gvrSecret, secretName, testNamespace, true, defaultTimeoutSeconds,
			)

			deleteConfigPolicies([]string{policyName})

			utils.GetWithTimeout(
				clientManagedDynamic, gvrSecret, secretName, testNamespace, false, defaultTimeoutSeconds,
			)
		})
	})

	Describe("Test copySecretData in ConfigurationPolicy", Ordered, func() {
		const (
			policyName = "case43-copysecret"
		)

		It("should be able to correctly create the secret on the managed cluster", func() {
			KubectlHub("apply", "-f", "../resources/case43_standalone_hub_templates/hub-secret.yaml")

			createObjWithParent(parentPolicyYAML, parentPolicyName,
				"../resources/case43_standalone_hub_templates/copy-secret-data-cfgpol.yaml",
				testNamespace, gvrPolicy, gvrConfigPolicy)

			By("Verifying that the " + policyName + " policy becomes compliant")
			Eventually(func(g Gomega) {
				managedPlc := utils.GetWithTimeout(
					clientManagedDynamic, gvrConfigPolicy, policyName, testNamespace, true, defaultTimeoutSeconds,
				)

				utils.CheckComplianceStatus(g, managedPlc, "Compliant")
			}, defaultTimeoutSeconds, 1).Should(Succeed())
		})

		AfterAll(func() {
			deleteConfigPolicies([]string{policyName})

			err := clientManagedDynamic.Resource(gvrPolicy).Namespace(testNamespace).Delete(
				context.TODO(), parentPolicyName, metav1.DeleteOptions{},
			)
			if !errors.IsNotFound(err) {
				Expect(err).ToNot(HaveOccurred())
			}

			KubectlHub("delete", "secret", "test", "--namespace=ocm-standalone-template-test-src",
				"--ignore-not-found")
			KubectlHub("delete", "secret", "long-named-secret-to-test-more",
				"--namespace=ocm-standalone-template-test-src", "--ignore-not-found")
			utils.KubectlDelete("secret", "test", "--namespace=default")
			utils.KubectlDelete("secret", "test-long", "--namespace=default")
			utils.Kubectl("delete", "events", "--namespace="+testNamespace,
				"--field-selector=involvedObject.name="+parentPolicyName)
		})
	})

	Describe("Test OperatorPolicy", Ordered, func() {
		const (
			policyName   = "case43-oppol-with-hub-template"
			policyYAML   = "../resources/case43_standalone_hub_templates/operator-policy.yaml"
			modifiedYAML = "../resources/case43_standalone_hub_templates/hub-configmap-modified.yaml"
		)

		AfterAll(func(ctx SpecContext) {
			utils.KubectlDelete("-n", testNamespace, "-f", policyYAML)

			err := clientManagedDynamic.Resource(gvrPolicy).Namespace(testNamespace).Delete(
				ctx, parentPolicyName, metav1.DeleteOptions{},
			)
			if !errors.IsNotFound(err) {
				Expect(err).ToNot(HaveOccurred())
			}

			KubectlHub("delete", "configmap", hubConfigmapName, "--namespace=default", "--ignore-not-found")
			utils.Kubectl("delete", "events", "--namespace="+testNamespace,
				"--field-selector=involvedObject.name="+parentPolicyName)
		})

		It("should be noncompliant when the hub ConfigMap doesn't exist", func() {
			createObjWithParent(parentPolicyYAML, parentPolicyName, policyYAML,
				testNamespace, gvrPolicy, gvrOperatorPolicy)

			By("Verifying that the " + policyName + " policy is noncompliant")
			Eventually(func(g Gomega) {
				managedPlc := utils.GetWithTimeout(
					clientManagedDynamic, gvrOperatorPolicy, policyName, testNamespace, true, defaultTimeoutSeconds,
				)

				utils.CheckComplianceStatus(g, managedPlc, "NonCompliant")
			}, defaultTimeoutSeconds, 1).Should(Succeed())

			By("Checking the compliance message")
			desiredMessage := "NonCompliant; failed to resolve the template"
			Eventually(func() []string {
				return utils.GetHistoryMessages(clientManagedDynamic, gvrOperatorPolicy,
					policyName, testNamespace, "")
			}, defaultTimeoutSeconds, 1).Should(ContainElement(ContainSubstring(desiredMessage)))
			Eventually(func() []v1.Event {
				return utils.GetMatchingEvents(clientManaged, testNamespace, parentPolicyName,
					fmt.Sprintf("policy: %v/%v", testNamespace, policyName), desiredMessage, defaultTimeoutSeconds)
			}, defaultTimeoutSeconds, 1).ShouldNot(BeEmpty())
		})

		It("should have a different message after the hub configmap is created", func() {
			KubectlHub("apply", "-f", hubConfigmapYAML)

			By("Checking the compliance message")
			desiredMessage := `NonCompliant; the operator namespace \('hello'\) does not exist`
			Eventually(func() []string {
				return utils.GetHistoryMessages(clientManagedDynamic, gvrOperatorPolicy,
					policyName, testNamespace, "")
			}, defaultTimeoutSeconds, 1).Should(ContainElement(MatchRegexp(desiredMessage)))
			Eventually(func() []v1.Event {
				return utils.GetMatchingEvents(clientManaged, testNamespace, parentPolicyName,
					fmt.Sprintf("policy: %v/%v", testNamespace, policyName), desiredMessage, defaultTimeoutSeconds)
			}, defaultTimeoutSeconds, 1).ShouldNot(BeEmpty())
		})

		It("should reconcile when the hub configmap changes", func() {
			time.Sleep(3 * time.Second) // give initial reconciles time to settle

			By("Modifying the hub configmap")
			KubectlHub("apply", "-f", modifiedYAML)

			By("Checking the compliance message")
			desiredMessage := `NonCompliant; the operator namespace \('changed'\) does not exist`
			Eventually(func() []string {
				return utils.GetHistoryMessages(clientManagedDynamic, gvrOperatorPolicy,
					policyName, testNamespace, "")
			}, defaultTimeoutSeconds, 1).Should(ContainElement(MatchRegexp(desiredMessage)))
			Eventually(func() []v1.Event {
				return utils.GetMatchingEvents(clientManaged, testNamespace, parentPolicyName,
					fmt.Sprintf("policy: %v/%v", testNamespace, policyName), desiredMessage, defaultTimeoutSeconds)
			}, defaultTimeoutSeconds, 1).ShouldNot(BeEmpty())
		})

		It("should not resolve the hub template if templates are disabled by annotation", func() {
			utils.Kubectl("annotate", "operatorpolicy", policyName, "-n", testNamespace,
				"policy.open-cluster-management.io/disable-templates=true")

			By("Checking the compliance message")
			desiredMessage := `NonCompliant; the namespace '{{hub fromConfigMap`
			Eventually(func() []string {
				return utils.GetHistoryMessages(clientManagedDynamic, gvrOperatorPolicy,
					policyName, testNamespace, "")
			}, defaultTimeoutSeconds, 1).Should(ContainElement(ContainSubstring(desiredMessage)))
			Eventually(func() []v1.Event {
				return utils.GetMatchingEvents(clientManaged, testNamespace, parentPolicyName,
					fmt.Sprintf("policy: %v/%v", testNamespace, policyName), desiredMessage, defaultTimeoutSeconds)
			}, defaultTimeoutSeconds, 1).ShouldNot(BeEmpty())
		})
	})
})
