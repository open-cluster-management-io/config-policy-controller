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

	Describe("Test multiple object definitions targeting same object", Ordered, func() {
		const (
			podConfigmapPolicyName string = "policy-create-pod-configmap"
			sameObjPolicyName      string = "policy-pod-multiple-same-obj-name"
			sameObjDefPolicyName   string = "policy-configmap-multiple-same-obj-def"
			podConfigmapYaml       string = "../resources/case5_multi/case5_pod_configmap.yaml"
			sameObjPolicyYaml      string = "../resources/case5_multi/case5_multi_same_obj_name.yaml"
			sameObjDefYaml         string = "../resources/case5_multi/case5_multi_same_objdef.yaml"
		)

		BeforeAll(func() {
			By("Creating nginx pod and configmap with enforce policy from case5")
			utils.Kubectl("apply", "-f", podConfigmapYaml, "-n", testNamespace)
			pod := utils.GetWithTimeout(clientManagedDynamic, gvrPod,
				podName1, "default", true, defaultTimeoutSeconds)
			Expect(pod).NotTo(BeNil())
		})

		type testCase struct {
			policyName string
			policyYaml string
			desc       string
		}

		DescribeTable("should evaluate object definitions and apply enforcement",
			func(tc testCase) {
				By("Creating " + tc.policyName + " with object-templates targeting the same pod")
				utils.Kubectl("apply", "-f", tc.policyYaml, "-n", testNamespace)
				plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					tc.policyName, testNamespace, true, defaultTimeoutSeconds)
				Expect(plc).NotTo(BeNil())

				Eventually(func(g Gomega) {
					By("Verifying the policy is NonCompliant")
					managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
						tc.policyName, testNamespace, true, defaultTimeoutSeconds)

					utils.CheckComplianceStatus(g, managedPlc, "NonCompliant")

					By("Verifying both compliancyDetails entries exist")
					details, _, _ := unstructured.NestedSlice(managedPlc.Object, "status", "compliancyDetails")
					g.Expect(details).To(HaveLen(2))
				}, defaultTimeoutSeconds, 1).Should(Succeed())

				By("Changing the policy to enforce mode")
				utils.EnforceConfigurationPolicy(tc.policyName, testNamespace)
				plc = utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					tc.policyName, testNamespace, true, defaultTimeoutSeconds)
				Expect(plc).NotTo(BeNil())

				Eventually(func(g Gomega) {
					By("Verifying the pod is Compliant")
					managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
						tc.policyName, testNamespace, true, defaultTimeoutSeconds)
					utils.CheckComplianceStatus(g, managedPlc, "Compliant")
				}, defaultTimeoutSeconds, 1).Should(Succeed())

				By("Deleting policy " + tc.policyName)
				deleteConfigPolicies([]string{tc.policyName})

				By("Resetting the pod for the next test case")
				deletePods([]string{podName1}, []string{"default"})
				utils.Kubectl("apply", "-f", podConfigmapYaml, "-n", testNamespace)
				pod := utils.GetWithTimeout(clientManagedDynamic, gvrPod,
					podName1, "default", true, defaultTimeoutSeconds)
				Expect(pod).NotTo(BeNil())
				configmap := utils.GetWithTimeout(clientManagedDynamic, gvrConfigMap,
					"case5-configmap-1", "default", true, defaultTimeoutSeconds)
				Expect(configmap).NotTo(BeNil())
			},
			Entry("with same object name", testCase{
				policyName: sameObjPolicyName,
				policyYaml: sameObjPolicyYaml,
				desc:       "policy-pod-multiple-same-obj-name",
			}),
			Entry("with same object definition and different complianceType", testCase{
				policyName: sameObjDefPolicyName,
				policyYaml: sameObjDefYaml,
				desc:       "policy-configmap-multiple-same-obj-def",
			}),
		)

		AfterAll(func() {
			deleteConfigPolicies([]string{podConfigmapPolicyName, sameObjPolicyName, sameObjDefPolicyName})
		})
	})
})
