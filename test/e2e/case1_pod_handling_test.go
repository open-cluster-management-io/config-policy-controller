// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package e2e

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"open-cluster-management.io/config-policy-controller/test/utils"
)

var _ = Describe("Test pod obj template handling", Ordered, func() {
	Describe("Test pod obj handling on managed cluster in ns:"+testNamespace, Ordered, func() {
		const (
			configPolicyNameInform       string = "policy-pod-create-inform"
			configPolicyNameEnforce      string = "policy-pod-create"
			podName                      string = "nginx-pod-e2e"
			policyYamlInform             string = "../resources/case1_pod_handling/case1_pod_create_inform.yaml"
			policyYamlEnforce            string = "../resources/case1_pod_handling/case1_pod_create_enforce.yaml"
			policyCheckMNHYaml           string = "../resources/case1_pod_handling/case1_pod_check-mnh.yaml"
			policyCheckMOHYaml           string = "../resources/case1_pod_handling/case1_pod_check-moh.yaml"
			policyCheckMHYaml            string = "../resources/case1_pod_handling/case1_pod_check-mh.yaml"
			policyYamlEnforceEmpty       string = "../resources/case1_pod_handling/case1_pod_create_empty_list.yaml"
			policyYamlInformEmpty        string = "../resources/case1_pod_handling/case1_pod_check_empty_list.yaml"
			policyCheckMNHIncompleteYaml string = "../resources/case1_pod_handling/case1_pod_check-mnh-incomplete.yaml"
			policyYamlMultipleCreate     string = "../resources/case1_pod_handling/case1_pod_create_multiple.yaml"
			policyYamlMultipleCheckMH    string = "../resources/case1_pod_handling/case1_pod_check_multiple_mh.yaml"
		)

		It("should be created properly on the managed cluster", func() {
			By("Creating " + policyYamlInform + " on managed")
			utils.Kubectl("apply", "-f", policyYamlInform, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				configPolicyNameInform, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func(g Gomega) {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					configPolicyNameInform, testNamespace, true, defaultTimeoutSeconds)

				utils.CheckComplianceStatus(g, managedPlc, "NonCompliant")
			}, defaultTimeoutSeconds, 1).Should(Succeed())
		})
		It("should create pod on managed cluster", func() {
			By("creating " + policyYamlEnforce + " on hub with spec.remediationAction = enforce")
			utils.Kubectl("apply", "-f", policyYamlEnforce, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				configPolicyNameEnforce, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func(g Gomega) {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					configPolicyNameEnforce, testNamespace, true, defaultTimeoutSeconds)

				utils.CheckComplianceStatus(g, managedPlc, "Compliant")
			}, defaultTimeoutSeconds, 1).Should(Succeed())
			Eventually(func(g Gomega) {
				informPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					configPolicyNameInform, testNamespace, true, defaultTimeoutSeconds)

				utils.CheckComplianceStatus(g, informPlc, "Compliant")
			}, defaultTimeoutSeconds, 1).Should(Succeed())
			pod := utils.GetWithTimeout(clientManagedDynamic, gvrPod,
				podName, "default", true, defaultTimeoutSeconds)
			Expect(pod).NotTo(BeNil())
			utils.Kubectl("apply", "-f", policyCheckMHYaml, "-n", testNamespace)
			plcMH := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				"policy-pod-check-mh-list", testNamespace, true, defaultTimeoutSeconds)
			Expect(plcMH).NotTo(BeNil())
			Eventually(func(g Gomega) {
				mHPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					"policy-pod-check-mh-list", testNamespace, true, defaultTimeoutSeconds)

				utils.CheckComplianceStatus(g, mHPlc, "Compliant")
			}, defaultTimeoutSeconds, 1).Should(Succeed())
			utils.Kubectl("apply", "-f", policyYamlEnforceEmpty, "-n", testNamespace)
			plcEmpty := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				"policy-pod-emptycontainerlist", testNamespace, true, defaultTimeoutSeconds)
			Expect(plcEmpty).NotTo(BeNil())
			Eventually(func(g Gomega) {
				emptyPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					"policy-pod-emptycontainerlist", testNamespace, true, defaultTimeoutSeconds)

				utils.CheckComplianceStatus(g, emptyPlc, "Compliant")
			}, defaultTimeoutSeconds, 1).Should(Succeed())
			utils.Kubectl("apply", "-f", policyYamlMultipleCreate, "-n", testNamespace)
			plcMultiple := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				"policy-pod-create-multiple", testNamespace, true, defaultTimeoutSeconds)
			Expect(plcMultiple).NotTo(BeNil())
			Eventually(func(g Gomega) {
				multiPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					"policy-pod-create-multiple", testNamespace, true, defaultTimeoutSeconds)

				utils.CheckComplianceStatus(g, multiPlc, "Compliant")
			}, defaultTimeoutSeconds, 1).Should(Succeed())
		})
		It("should create violations properly", func() {
			utils.Kubectl("apply", "-f", policyCheckMNHYaml, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				"policy-pod-check-mnh", testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func(g Gomega) {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					"policy-pod-check-mnh", testNamespace, true, defaultTimeoutSeconds)

				utils.CheckComplianceStatus(g, managedPlc, "NonCompliant")
			}, defaultTimeoutSeconds, 1).Should(Succeed())
			utils.Kubectl("apply", "-f", policyCheckMNHIncompleteYaml, "-n", testNamespace)
			plc = utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				"policy-pod-check-mnh-incomplete", testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func(g Gomega) {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					"policy-pod-check-mnh-incomplete", testNamespace, true, defaultTimeoutSeconds)

				utils.CheckComplianceStatus(g, managedPlc, "NonCompliant")
			}, defaultTimeoutSeconds, 1).Should(Succeed())
			utils.Kubectl("apply", "-f", policyCheckMOHYaml, "-n", testNamespace)
			plc = utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				"policy-pod-check-moh", testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func(g Gomega) {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					"policy-pod-check-moh", testNamespace, true, defaultTimeoutSeconds)

				utils.CheckComplianceStatus(g, managedPlc, "NonCompliant")
			}, defaultTimeoutSeconds, 1).Should(Succeed())
			utils.Kubectl("apply", "-f", policyYamlInformEmpty, "-n", testNamespace)
			plc = utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				"policy-pod-check-emptycontainerlist", testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func(g Gomega) {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					"policy-pod-check-emptycontainerlist", testNamespace, true, defaultTimeoutSeconds)

				utils.CheckComplianceStatus(g, managedPlc, "NonCompliant")
			}, defaultTimeoutSeconds, 1).Should(Succeed())
			utils.Kubectl("apply", "-f", policyYamlMultipleCheckMH, "-n", testNamespace)
			plc = utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				"policy-pod-check-multiple-mh", testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func(g Gomega) {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					"policy-pod-check-multiple-mh", testNamespace, true, defaultTimeoutSeconds)

				utils.CheckComplianceStatus(g, managedPlc, "Compliant")
			}, defaultTimeoutSeconds, 1).Should(Succeed())
		})
		AfterAll(func() {
			policies := []string{
				configPolicyNameInform,
				configPolicyNameEnforce,
				"policy-pod-check-emptycontainerlist",
				"policy-pod-check-mh-list",
				"policy-pod-check-mnh-incomplete",
				"policy-pod-check-mnh",
				"policy-pod-check-multiple-mh",
				"policy-pod-check-moh",
				"policy-pod-create-multiple",
				"policy-pod-emptycontainerlist",
			}
			deleteConfigPolicies(policies)

			By("Delete pods")
			pods := []string{podName, podName + "-empty", podName + "-multi"}
			namespaces := []string{testNamespace, "default"}
			deletePods(pods, namespaces)
		})
	})
})
