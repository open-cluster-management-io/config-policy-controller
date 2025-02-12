// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package e2e

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"open-cluster-management.io/config-policy-controller/test/utils"
)

var _ = Describe("Test multiple obj template handling", Ordered, func() {
	const (
		configPolicyNameInform  string = "policy-pod-multi-mh"
		configPolicyNameEnforce string = "policy-pod-multi-create"
		podName1                string = "case5-nginx-pod-1"
		podName2                string = "case5-nginx-pod-2"
		informYaml              string = "../resources/case5_multi/case5_multi_mh.yaml"
		enforceYaml             string = "../resources/case5_multi/case5_multi_enforce.yaml"
		singleYaml              string = "../resources/case5_multi/case5_single_mh.yaml"
	)

	Describe("Create a policy on managed cluster in ns:"+testNamespace, Ordered, func() {
		It("should be created properly on the managed cluster", func() {
			By("Creating " + configPolicyNameInform + " on managed")
			utils.Kubectl("apply", "-f", informYaml, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				configPolicyNameInform, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func(g Gomega) {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					configPolicyNameInform, testNamespace, true, defaultTimeoutSeconds)

				utils.CheckComplianceStatus(g, managedPlc, "NonCompliant")
			}, defaultTimeoutSeconds, 1).Should(Succeed())
		})
		It("should create pods on managed cluster", func() {
			By("creating " + configPolicyNameEnforce + " on hub with spec.remediationAction = enforce")
			utils.Kubectl("apply", "-f", enforceYaml, "-n", testNamespace)
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
			pod1 := utils.GetWithTimeout(clientManagedDynamic, gvrPod,
				podName1, "default", true, defaultTimeoutSeconds)
			Expect(pod1).NotTo(BeNil())
			pod2 := utils.GetWithTimeout(clientManagedDynamic, gvrPod,
				podName2, "default", true, defaultTimeoutSeconds)
			Expect(pod2).NotTo(BeNil())
		})
		It("should only have compliancy details on the current objects when templates are removed", func() {
			By("confirming the current details on the policy")
			Eventually(func(g Gomega) {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					configPolicyNameInform, testNamespace, true, defaultTimeoutSeconds)
				details, _, _ := unstructured.NestedSlice(managedPlc.Object, "status", "compliancyDetails")
				g.Expect(details).To(HaveLen(2))
			}, defaultTimeoutSeconds, 1).Should(Succeed())

			By("removing the second template on the policy")
			utils.Kubectl("apply", "-f", singleYaml, "-n", testNamespace)

			By("checking the new details on the policy")
			Eventually(func(g Gomega) {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					configPolicyNameInform, testNamespace, true, defaultTimeoutSeconds)
				details, _, _ := unstructured.NestedSlice(managedPlc.Object, "status", "compliancyDetails")
				g.Expect(details).To(HaveLen(1))
			}, defaultTimeoutSeconds, 1).Should(Succeed())
		})
		AfterAll(func() {
			policies := []string{
				configPolicyNameInform,
				configPolicyNameEnforce,
			}

			deleteConfigPolicies(policies)

			By("Delete pods")
			pods := []string{podName1, podName2}
			namespaces := []string{"default"}
			deletePods(pods, namespaces)
		})
	})
})
