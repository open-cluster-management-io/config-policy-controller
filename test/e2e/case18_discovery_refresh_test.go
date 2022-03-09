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

	"open-cluster-management.io/config-policy-controller/test/utils"
)

const (
	case18PolicyName            = "policy-c18"
	case18Policy                = "../resources/case18_discovery_refresh/policy.yaml"
	case18PolicyTemplateName    = "policy-c18-template"
	case18PolicyTemplatePreReqs = "../resources/case18_discovery_refresh/prereqs-for-template-policy.yaml"
	case18PolicyTemplate        = "../resources/case18_discovery_refresh/policy-template.yaml"
	case18ConfigMapName         = "c18-configmap"
)

var _ = Describe("Test discovery info refresh", func() {
	It("Verifies that the discovery info is refreshed after a CRD is installed", func() {
		By("Creating " + case18PolicyName + " on managed")
		utils.Kubectl("apply", "-f", case18Policy, "-n", testNamespace)
		policy := utils.GetWithTimeout(
			clientManagedDynamic,
			gvrConfigPolicy,
			case18PolicyName,
			testNamespace,
			true,
			defaultTimeoutSeconds,
		)
		Expect(policy).NotTo(BeNil())

		By("Verifying " + case18PolicyName + " becomes compliant")
		Eventually(func() interface{} {
			policy := utils.GetWithTimeout(
				clientManagedDynamic,
				gvrConfigPolicy,
				case18PolicyName,
				testNamespace,
				true,
				defaultTimeoutSeconds,
			)

			return utils.GetComplianceState(policy)
		}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
	})

	It("Verifies that the discovery info is refreshed when a template references a new object kind", func() {
		By("Creating the prerequisites on managed")
		// This needs to be wrapped in an eventually since the object can't be created immediately after the CRD
		// is created.
		Eventually(
			func() interface{} {
				cmd := exec.Command("kubectl", "apply", "-f", case18PolicyTemplatePreReqs)

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

		By("Creating " + case18PolicyTemplateName + " on managed")
		utils.Kubectl("apply", "-f", case18PolicyTemplate, "-n", testNamespace)
		policy := utils.GetWithTimeout(
			clientManagedDynamic,
			gvrConfigPolicy,
			case18PolicyTemplateName,
			testNamespace,
			true,
			defaultTimeoutSeconds,
		)
		Expect(policy).NotTo(BeNil())

		By("Verifying " + case18PolicyTemplateName + " becomes compliant")
		Eventually(func() interface{} {
			policy := utils.GetWithTimeout(
				clientManagedDynamic,
				gvrConfigPolicy,
				case18PolicyTemplateName,
				testNamespace,
				true,
				defaultTimeoutSeconds,
			)

			return utils.GetComplianceState(policy)
		}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
	})

	It("Cleans up", func() {
		err := clientManagedDynamic.Resource(gvrConfigPolicy).Namespace(testNamespace).Delete(
			context.TODO(), case18PolicyName, metav1.DeleteOptions{},
		)
		if !k8serrors.IsNotFound(err) {
			Expect(err).To(BeNil())
		}

		err = clientManagedDynamic.Resource(gvrCRD).Delete(
			context.TODO(), "pizzaslices.food.example.com", metav1.DeleteOptions{},
		)
		if !k8serrors.IsNotFound(err) {
			Expect(err).To(BeNil())
		}

		err = clientManagedDynamic.Resource(gvrConfigPolicy).Namespace(testNamespace).Delete(
			context.TODO(), case18PolicyTemplateName, metav1.DeleteOptions{},
		)
		if !k8serrors.IsNotFound(err) {
			Expect(err).To(BeNil())
		}

		err = clientManagedDynamic.Resource(gvrCRD).Delete(
			context.TODO(), "pizzaslices.diner.example.com", metav1.DeleteOptions{},
		)
		if !k8serrors.IsNotFound(err) {
			Expect(err).To(BeNil())
		}

		err = clientManaged.CoreV1().ConfigMaps("default").Delete(
			context.TODO(), case18ConfigMapName, metav1.DeleteOptions{},
		)
		if !k8serrors.IsNotFound(err) {
			Expect(err).To(BeNil())
		}
	})
})
