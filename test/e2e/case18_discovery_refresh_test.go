// Copyright (c) 2022 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package e2e

import (
	"context"
	"os/exec"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"open-cluster-management.io/config-policy-controller/test/utils"
)

var _ = Describe("Test discovery info refresh", Ordered, func() {
	const (
		policyName            = "policy-c18"
		policyYaml            = "../resources/case18_discovery_refresh/policy.yaml"
		policyTemplateName    = "policy-c18-template"
		policyTemplatePreReqs = "../resources/case18_discovery_refresh/prereqs-for-template-policy.yaml"
		policyTemplateYaml    = "../resources/case18_discovery_refresh/policy-template.yaml"
		configMapName         = "c18-configmap"
		badAPIServiceYaml     = "../resources/case18_discovery_refresh/bad-apiservice.yaml"
	)

	It("Verifies that the discovery info is refreshed after a CRD is installed", func() {
		By("Creating " + policyName + " on managed")
		utils.Kubectl("apply", "-f", policyYaml, "-n", testNamespace)
		policy := utils.GetWithTimeout(
			clientManagedDynamic,
			gvrConfigPolicy,
			policyName,
			testNamespace,
			true,
			defaultTimeoutSeconds,
		)
		Expect(policy).NotTo(BeNil())

		By("Verifying " + policyName + " becomes compliant")
		Eventually(func() interface{} {
			policy := utils.GetWithTimeout(
				clientManagedDynamic,
				gvrConfigPolicy,
				policyName,
				testNamespace,
				true,
				defaultTimeoutSeconds,
			)

			return utils.GetComplianceState(policy)
		}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
	})

	It("Verifies that the discovery info is refreshed when a template references a new object kind", func() {
		By("Adding a non-functional API service")
		utils.Kubectl("apply", "-f", badAPIServiceYaml)

		By("Checking that the API causes an error during API discovery")
		Eventually(
			func(g Gomega) {
				apiService := utils.GetWithTimeout(
					clientManagedDynamic,
					gvrAPIService,
					"v1beta1.pizza.example.com",
					"",
					true,
					defaultTimeoutSeconds,
				)

				apiStatus, _, err := unstructured.NestedSlice(apiService.Object, "status", "conditions")
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(apiStatus).ToNot(BeEmpty())

				statusFound := false
				for _, status := range apiStatus {
					if status, ok := status.(map[string]interface{}); ok {
						if status["type"] == "Available" {
							g.Expect(status["status"]).To(Equal("False"))
							g.Expect(status["reason"]).To(Equal("ServiceNotFound"))
							statusFound = true
						}
					}
				}

				if !statusFound {
					Fail("Failed to find 'Available' status for APIService")
				}
			},
			defaultTimeoutSeconds,
			1,
		).Should(Succeed())

		By("Creating the prerequisites on managed")
		// This needs to be wrapped in an eventually since the object can't be created immediately after the CRD
		// is created.
		Eventually(
			func() interface{} {
				cmd := exec.Command("kubectl", "apply", "-f", policyTemplatePreReqs)

				err := cmd.Start()
				if err != nil {
					return err
				}

				err = cmd.Wait()

				return err
			},
			defaultTimeoutSeconds,
			1,
		).Should(BeNil())

		By("Creating " + policyTemplateName + " on managed")
		utils.Kubectl("apply", "-f", policyTemplateYaml, "-n", testNamespace)
		policy := utils.GetWithTimeout(
			clientManagedDynamic,
			gvrConfigPolicy,
			policyTemplateName,
			testNamespace,
			true,
			defaultTimeoutSeconds,
		)
		Expect(policy).NotTo(BeNil())

		By("Verifying " + policyTemplateName + " becomes compliant")
		Eventually(func() interface{} {
			policy := utils.GetWithTimeout(
				clientManagedDynamic,
				gvrConfigPolicy,
				policyTemplateName,
				testNamespace,
				true,
				defaultTimeoutSeconds,
			)

			return utils.GetComplianceState(policy)
		}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
	})

	AfterAll(func() {
		err := clientManagedDynamic.Resource(gvrConfigPolicy).Namespace(testNamespace).Delete(
			context.TODO(), policyName, metav1.DeleteOptions{},
		)
		if !k8serrors.IsNotFound(err) {
			Expect(err).ToNot(HaveOccurred())
		}

		err = clientManagedDynamic.Resource(gvrCRD).Delete(
			context.TODO(), "pizzaslices.food.example.com", metav1.DeleteOptions{},
		)
		if !k8serrors.IsNotFound(err) {
			Expect(err).ToNot(HaveOccurred())
		}

		err = clientManagedDynamic.Resource(gvrConfigPolicy).Namespace(testNamespace).Delete(
			context.TODO(), policyTemplateName, metav1.DeleteOptions{},
		)
		if !k8serrors.IsNotFound(err) {
			Expect(err).ToNot(HaveOccurred())
		}

		err = clientManagedDynamic.Resource(gvrCRD).Delete(
			context.TODO(), "pizzaslices.diner.example.com", metav1.DeleteOptions{},
		)
		if !k8serrors.IsNotFound(err) {
			Expect(err).ToNot(HaveOccurred())
		}

		err = clientManaged.CoreV1().ConfigMaps("default").Delete(
			context.TODO(), configMapName, metav1.DeleteOptions{},
		)
		if !k8serrors.IsNotFound(err) {
			Expect(err).ToNot(HaveOccurred())
		}

		gvrAPIService := schema.GroupVersionResource{
			Group:    "apiregistration.k8s.io",
			Version:  "v1",
			Resource: "apiservices",
		}

		err = clientManagedDynamic.Resource(gvrAPIService).Delete(
			context.TODO(), "v1beta1.pizza.example.com", metav1.DeleteOptions{},
		)
		if !k8serrors.IsNotFound(err) {
			Expect(err).ToNot(HaveOccurred())
		}
	})
})
