// Copyright (c) 2021 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package e2e

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"open-cluster-management.io/config-policy-controller/test/utils"
)

var _ = Describe("Test resource creation when there are empty labels in configurationPolicy", Ordered, func() {
	const (
		case33ConfigPolicy     = "../resources/case33_empty_label_handling/empty-labels-config.yaml"
		case33ConfigMapYaml    = "../resources/case33_empty_label_handling/configmap.yaml"
		case33ConfigPolicyName = "case33-empty-labels"
		case33ConfigMapName    = "case33-configmap"
		case33Label            = "new-label"
	)

	It("verifies the configmap"+case33ConfigMapName+" exists in "+testNamespace, func() {
		By("creating the configmap " + case33ConfigMapName)
		utils.Kubectl("apply", "-f", case33ConfigMapYaml, "-n", testNamespace)

		By("checking configmap exists in " + testNamespace)
		cm := utils.GetWithTimeout(clientManagedDynamic, gvrConfigMap, case33ConfigMapName,
			testNamespace, true, defaultTimeoutSeconds)
		Expect(cm).NotTo(BeNil())
	})

	It("verifies the policy "+case33ConfigPolicyName+" exists in "+testNamespace, func() {
		By("creating the ConfigurationPolicy " + case33ConfigPolicyName)
		utils.Kubectl("apply", "-f", case33ConfigPolicy, "-n", testNamespace)

		By("checking ConfigurationPolicy exists in " + testNamespace)
		cfPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
			case33ConfigPolicyName, testNamespace, true, defaultTimeoutSeconds)
		Expect(cfPlc).NotTo(BeNil())
	})

	It("verifies the configmap "+case33ConfigMapName+" has the proper labels", func() {
		By("waiting for policy compliance")
		Eventually(func() interface{} {
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case33ConfigPolicyName, testNamespace, true, defaultTimeoutSeconds)

			return utils.GetComplianceState(plc)
		}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))

		By("verifying label exists and correct")
		Eventually(func() interface{} {
			configmap, _ := clientManaged.CoreV1().ConfigMaps(testNamespace).
				Get(context.TODO(), case33ConfigMapName, metav1.GetOptions{})
			labelMap := configmap.GetLabels()

			return labelMap
		}, defaultTimeoutSeconds, 1).Should(HaveKeyWithValue(case33Label, ""))
	})

	AfterAll(func() {
		deleteConfigPolicies([]string{case33ConfigPolicyName})
		utils.KubectlDelete("configmap", case33ConfigMapName, "-n", testNamespace)
	})
})
