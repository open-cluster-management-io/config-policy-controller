// Copyright Contributors to the Open Cluster Management project

package e2e

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"open-cluster-management.io/config-policy-controller/test/utils"
)

var _ = Describe("Test setting an unset field", Ordered, func() {
	const (
		sriovCRD    = "../resources/case45_set_empty_field/sriov_config_crd.yaml"
		sriovConfig = "../resources/case45_set_empty_field/sriov_config.yaml"
		policyName  = "case45-policy"
		policyFile  = "../resources/case45_set_empty_field/policy.yaml"
	)

	gvrSriovConfig := schema.GroupVersionResource{
		Group:    "sriovnetwork.openshift.io",
		Version:  "v1",
		Resource: "sriovoperatorconfigs",
	}

	BeforeEach(func() {
		By("applying the sriov CRD")
		utils.Kubectl("apply", "-f", sriovCRD)
		By("applying the sriov config instance")
		utils.Kubectl("apply", "-f", sriovConfig)
	})

	AfterEach(func() {
		By("deleting the sriov config instance")
		utils.KubectlDelete("-f", sriovConfig)
		By("deleting the sriov CRD")
		utils.KubectlDelete("-f", sriovCRD)
		deleteConfigPolicies([]string{policyName})
	})

	It("verifies the policy "+policyName+" works correctly", func() {
		By("checking the config resource is in the expected state")
		sriovObj := utils.GetWithTimeout(
			clientManagedDynamic,
			gvrSriovConfig,
			"default",
			"default",
			true,
			defaultTimeoutSeconds,
		)
		_, found, err := unstructured.NestedBool(sriovObj.Object, "spec", "enableInjector")
		Expect(err).ToNot(HaveOccurred())
		Expect(found).To(BeFalse())

		By("creating the policy " + policyName)
		utils.Kubectl("apply", "-f", policyFile, "-n", testNamespace)

		By("checking if the policy " + policyName + " is NonCompliant")
		Eventually(func(g Gomega) {
			managedPlc := utils.GetWithTimeout(
				clientManagedDynamic,
				gvrConfigPolicy,
				policyName,
				testNamespace,
				true,
				defaultTimeoutSeconds,
			)

			utils.CheckComplianceStatus(g, managedPlc, "NonCompliant")
		}, defaultTimeoutSeconds, 1).Should(Succeed())

		By("checking the policy can be enforced and become compliant")
		utils.EnforceConfigurationPolicy(policyName, testNamespace)
		Eventually(func(g Gomega) {
			managedPlc := utils.GetWithTimeout(
				clientManagedDynamic,
				gvrConfigPolicy,
				policyName,
				testNamespace,
				true,
				defaultTimeoutSeconds,
			)

			utils.CheckComplianceStatus(g, managedPlc, "Compliant")
		}, defaultTimeoutSeconds, 1).Should(Succeed())

		By("checking the config resource is in the updated state")
		sriovObj = utils.GetWithTimeout(
			clientManagedDynamic,
			gvrSriovConfig,
			"default",
			"default",
			true,
			defaultTimeoutSeconds,
		)
		val, found, err := unstructured.NestedBool(sriovObj.Object, "spec", "enableInjector")
		Expect(err).ToNot(HaveOccurred())
		Expect(found).To(BeTrue())
		Expect(val).To(BeFalse())
	})
})
