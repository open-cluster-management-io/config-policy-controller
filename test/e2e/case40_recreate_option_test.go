// Copyright Contributors to the Open Cluster Management project

package e2e

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"open-cluster-management.io/config-policy-controller/test/utils"
)

var _ = Describe("Recreate options", Ordered, func() {
	const (
		deploymentYAML           = "../resources/case40_recreate_option/deployment.yaml"
		configMapYAML            = "../resources/case40_recreate_option/configmap-with-finalizer.yaml"
		policyNoRecreateYAML     = "../resources/case40_recreate_option/policy-no-recreate-options.yaml"
		policyAlwaysRecreateYAML = "../resources/case40_recreate_option/policy-always-recreate-option.yaml"
	)

	AfterAll(func(ctx SpecContext) {
		deleteConfigPolicies([]string{"case40"})

		By("Deleting the case40 Deployment in the default namespace")
		utils.Kubectl("-n", "default", "delete", "deployment", "case40", "--ignore-not-found")

		By("Deleting the case40 ConfigMap in the default namespace")
		configmap, err := clientManagedDynamic.Resource(gvrConfigMap).Namespace("default").Get(
			ctx, "case40", metav1.GetOptions{},
		)
		if k8serrors.IsNotFound(err) {
			return
		}

		Expect(err).ToNot(HaveOccurred())

		if len(configmap.GetFinalizers()) != 0 {
			utils.Kubectl(
				"-n", "default",
				"patch",
				"configmap",
				"case40",
				"--type",
				"json",
				`-p=[{"op":"remove","path":"/metadata/finalizers"}]`,
			)
		}

		utils.Kubectl("-n", "default", "delete", "configmap", "case40", "--ignore-not-found")
	})

	It("should fail to update due to immutable fields not matching", func() {
		By("Creating the case40 Deployment in the default namespace")
		utils.Kubectl("create", "-f", deploymentYAML)

		By("Creating the case40 ConfigurationPolicy with different selectors on the Deployment")
		utils.Kubectl("-n", testNamespace, "create", "-f", policyNoRecreateYAML)

		By("Verifying the ConfigurationPolicy is NonCompliant")
		var managedPlc *unstructured.Unstructured

		Eventually(func() interface{} {
			managedPlc = utils.GetWithTimeout(
				clientManagedDynamic,
				gvrConfigPolicy,
				"case40",
				testNamespace,
				true,
				defaultTimeoutSeconds,
			)

			return utils.GetComplianceState(managedPlc)
		}, defaultTimeoutSeconds, 1).Should(Equal("NonCompliant"))

		By("Verifying the diff is present")
		relatedObjects, _, err := unstructured.NestedSlice(managedPlc.Object, "status", "relatedObjects")
		Expect(err).ToNot(HaveOccurred())
		Expect(relatedObjects).To(HaveLen(1))

		diff, _, _ := unstructured.NestedString(relatedObjects[0].(map[string]interface{}), "properties", "diff")
		Expect(diff).To(ContainSubstring("-      app: case40\n+      app: case40-2\n"))

		By("Verifying the compliance message is correct and doesn't change")
		Consistently(func(g Gomega) {
			managedPlc = utils.GetWithTimeout(
				clientManagedDynamic,
				gvrConfigPolicy,
				"case40",
				testNamespace,
				true,
				defaultTimeoutSeconds,
			)

			expected := `deployments [case40] in namespace default cannot be updated, likely due to immutable fields ` +
				`not matching, you may set spec["object-templates"][].recreateOption to recreate the object`

			helpMsg := "Expected the cached evaluation to not change the message"
			g.Expect(utils.GetStatusMessage(managedPlc)).To(Equal(expected), helpMsg)
		}, defaultConsistentlyDuration, 1).Should(Succeed())
	})

	It("should update the immutable fields when recreateOption=IfRequired", func(ctx SpecContext) {
		By("Setting recreateOption=IfRequired on the case40 ConfigurationPolicy")
		deployment, err := clientManagedDynamic.Resource(gvrDeployment).Namespace("default").Get(
			ctx, "case40", metav1.GetOptions{},
		)
		Expect(err).ToNot(HaveOccurred())

		uid := deployment.GetUID()

		utils.Kubectl(
			"-n", testNamespace, "patch", "configurationpolicy", "case40", "--type=json", "-p",
			`[{ "op": "replace", "path": "/spec/object-templates/0/recreateOption", "value": 'IfRequired' }]`,
		)

		By("Verifying the ConfigurationPolicy is Compliant")
		var managedPlc *unstructured.Unstructured

		Eventually(func() interface{} {
			managedPlc = utils.GetWithTimeout(
				clientManagedDynamic,
				gvrConfigPolicy,
				"case40",
				testNamespace,
				true,
				defaultTimeoutSeconds,
			)

			return utils.GetComplianceState(managedPlc)
		}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))

		By("Verifying the diff is not set")
		relatedObjects, _, err := unstructured.NestedSlice(managedPlc.Object, "status", "relatedObjects")
		Expect(err).ToNot(HaveOccurred())
		Expect(relatedObjects).To(HaveLen(1))

		relatedObject := relatedObjects[0].(map[string]interface{})

		diff, _, _ := unstructured.NestedString(relatedObject, "properties", "diff")
		Expect(diff).To(BeEmpty())

		propsUID, _, _ := unstructured.NestedString(relatedObject, "properties", "uid")
		Expect(propsUID).ToNot(BeEmpty())

		createdByPolicy, _, _ := unstructured.NestedBool(relatedObject, "properties", "createdByPolicy")
		Expect(createdByPolicy).To(BeTrue())

		deployment, err = clientManagedDynamic.Resource(gvrDeployment).Namespace("default").Get(
			ctx, "case40", metav1.GetOptions{},
		)
		Expect(err).ToNot(HaveOccurred())

		By("Verifying the Deployment was recreated")
		Expect(deployment.GetUID()).ToNot(Equal(uid), "Expected a new UID on the Deployment after it got recreated")
		Expect(propsUID).To(
			BeEquivalentTo(deployment.GetUID()), "Expect the object properties UID to match the new Deployment",
		)

		selector, _, _ := unstructured.NestedString(deployment.Object, "spec", "selector", "matchLabels", "app")
		Expect(selector).To(Equal("case40-2"))

		deleteConfigPolicies([]string{"case40"})
	})

	It("should timeout on the delete when there is a finalizer", func(ctx SpecContext) {
		By("Creating the case40 ConfigMap in the default namespace")
		utils.Kubectl("create", "-f", configMapYAML)

		configmap, err := clientManagedDynamic.Resource(gvrConfigMap).Namespace("default").Get(
			ctx, "case40", metav1.GetOptions{},
		)
		Expect(err).ToNot(HaveOccurred())

		uid := configmap.GetUID()

		By("Creating the case40 ConfigurationPolicy with different data on the ConfigMap")
		utils.Kubectl("-n", testNamespace, "create", "-f", policyAlwaysRecreateYAML)

		expected := `configmaps [case40] in namespace default timed out waiting for the object to delete during ` +
			`recreate, will retry on the next policy evaluation`

		By("Verifying the ConfigurationPolicy is NonCompliant")
		var managedPlc *unstructured.Unstructured

		Eventually(func(g Gomega) {
			managedPlc = utils.GetWithTimeout(
				clientManagedDynamic,
				gvrConfigPolicy,
				"case40",
				testNamespace,
				true,
				defaultTimeoutSeconds,
			)

			g.Expect(utils.GetComplianceState(managedPlc)).To(Equal("NonCompliant"))
			g.Expect(utils.GetStatusMessage(managedPlc)).To(Equal(expected))
		}, defaultTimeoutSeconds, 1).Should(Succeed())

		By("Removing the finalizer on the ConfigMap")
		utils.Kubectl(
			"-n", "default",
			"patch",
			"configmap",
			"case40",
			"--type",
			"json",
			`-p=[{"op":"remove","path":"/metadata/finalizers"}]`,
		)

		By("Verifying the ConfigurationPolicy is Compliant")
		Eventually(func() interface{} {
			managedPlc = utils.GetWithTimeout(
				clientManagedDynamic,
				gvrConfigPolicy,
				"case40",
				testNamespace,
				true,
				defaultTimeoutSeconds,
			)

			return utils.GetComplianceState(managedPlc)
		}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))

		By("Verifying the ConfigMap was recreated")

		configmap, err = clientManagedDynamic.Resource(gvrConfigMap).Namespace("default").Get(
			ctx, "case40", metav1.GetOptions{},
		)
		Expect(err).ToNot(HaveOccurred())

		Expect(configmap.GetUID()).ToNot(Equal(uid), "Expected a new UID on the ConfigMap after it got recreated")

		city, _, _ := unstructured.NestedString(configmap.Object, "data", "city")
		Expect(city).To(Equal("Raleigh"))

		deleteConfigPolicies([]string{"case40"})
	})
})
