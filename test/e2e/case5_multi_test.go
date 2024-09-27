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
		configPolicyNameCombo   string = "policy-pod-multi-combo"
		podName1                string = "case5-nginx-pod-1"
		podName2                string = "case5-nginx-pod-2"
		informYaml              string = "../resources/case5_multi/case5_multi_mh.yaml"
		enforceYaml             string = "../resources/case5_multi/case5_multi_enforce.yaml"
		comboYaml               string = "../resources/case5_multi/case5_multi_combo.yaml"
		singleYaml              string = "../resources/case5_multi/case5_single_mh.yaml"
	)

	Describe("Create a policy on managed cluster in ns:"+testNamespace, Ordered, func() {
		It("should be created properly on the managed cluster", func() {
			By("Creating " + configPolicyNameInform + " and " + configPolicyNameCombo + " on managed")
			utils.Kubectl("apply", "-f", informYaml, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				configPolicyNameInform, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func(g Gomega) {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					configPolicyNameInform, testNamespace, true, defaultTimeoutSeconds)

				utils.CheckComplianceStatus(g, managedPlc, "NonCompliant")
			}, defaultTimeoutSeconds, 1).Should(Succeed())
			utils.Kubectl("apply", "-f", comboYaml, "-n", testNamespace)
			plc = utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				configPolicyNameCombo, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func(g Gomega) {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					configPolicyNameCombo, testNamespace, true, defaultTimeoutSeconds)

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
			Eventually(func(g Gomega) {
				comboPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					configPolicyNameCombo, testNamespace, true, defaultTimeoutSeconds)

				utils.CheckComplianceStatus(g, comboPlc, "NonCompliant")
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
				configPolicyNameCombo,
			}

			deleteConfigPolicies(policies)

			By("Delete pods")
			pods := []string{podName1, podName2}
			namespaces := []string{"default"}
			deletePods(pods, namespaces)
		})
	})

	Describe("Check messages when it is multiple namespaces and multiple obj-templates", Ordered, func() {
		const (
			case5MultiNamespace1               string = "n1"
			case5MultiNamespace2               string = "n2"
			case5MultiNamespace3               string = "n3"
			case5MultiNSConfigPolicyName       string = "policy-multi-namespace-enforce"
			case5MultiNSInformConfigPolicyName string = "policy-multi-namespace-inform"
			case5MultiObjNSConfigPolicyName    string = "policy-pod-multi-obj-temp-enforce"
			case5InformYaml                    string = "../resources/case5_multi/case5_multi_namespace_inform.yaml"
			case5EnforceYaml                   string = "../resources/case5_multi/case5_multi_namespace_enforce.yaml"
			case5MultiObjTmpYaml               string = "../resources/case5_multi/case5_multi_obj_template_enforce.yaml"
			case5MultiEnforceErrYaml           string = "../resources/case5_multi/" +
				"case5_multi_namespace_enforce_error.yaml"
			case5KindMissPlcName     string = "policy-multi-namespace-enforce-kind-missing"
			case5KindNameMissPlcName string = "policy-multi-namespace-enforce-both-missing"
			case5NameMissPlcName     string = "policy-multi-namespace-enforce-name-missing"
		)

		BeforeAll(func() {
			nss := []string{
				case5MultiNamespace1,
				case5MultiNamespace2,
				case5MultiNamespace3,
			}

			for _, ns := range nss {
				utils.Kubectl("create", "ns", ns)
			}
		})
		It("Should handle when kind or name missing without any panic", func() {
			utils.Kubectl("apply", "-f", case5MultiEnforceErrYaml)

			By("Kind missing")
			kindErrMsg := "Decoding error, please check your policy file! Aborting handling the " +
				"object template at index [0] in policy `policy-multi-namespace-enforce-kind-missing` with " +
				"error = `Object 'Kind' is missing in '{\"apiVersion\":\"v1\",\"metadata\":" +
				"{\"name\":\"case5-multi-namespace-enforce-kind-missing-pod\"}," +
				"\"spec\":{\"containers\":[{\"image\":\"nginx:1.7.9\",\"imagePullPolicy\":\"Never\",\"name\":" +
				"\"nginx\",\"ports\":[{\"containerPort\":80}]}]}}'`"
			utils.DoConfigPolicyMessageTest(clientManagedDynamic, gvrConfigPolicy, testNamespace,
				case5KindMissPlcName, 0, defaultTimeoutSeconds, kindErrMsg)

			By("Kind and Name missing")
			kindNameErrMsg := "Decoding error, please check your policy file! Aborting handling the " +
				"object template at index [0] in policy `policy-multi-namespace-enforce-both-missing` " +
				"with error = `Object 'Kind' is missing in " +
				"'{\"apiVersion\":\"v1\",\"metadata\":{\"name\":\"\"},\"spec\":{\"containers\":" +
				"[{\"image\":\"nginx:1.7.9\",\"imagePullPolicy\":\"Never\"," +
				"\"name\":\"nginx\",\"ports\":[{\"containerPort\":80}]}]}}'`"
			utils.DoConfigPolicyMessageTest(clientManagedDynamic, gvrConfigPolicy, testNamespace,
				case5KindNameMissPlcName, 0, defaultTimeoutSeconds, kindNameErrMsg)

			By("Name missing")
			nameErrMsg := "pods not found in namespaces: n1, n2, n3"
			utils.DoConfigPolicyMessageTest(clientManagedDynamic, gvrConfigPolicy, testNamespace,
				case5NameMissPlcName, 0, defaultTimeoutSeconds, nameErrMsg)
		})
		It("Should show merged Noncompliant messages when it is multiple namespaces and inform", func() {
			expectedMsg := "pods [case5-multi-namespace-inform-pod] not found in namespaces: n1, n2, n3"
			utils.Kubectl("apply", "-f", case5InformYaml)
			utils.DoConfigPolicyMessageTest(clientManagedDynamic, gvrConfigPolicy, testNamespace,
				case5MultiNSInformConfigPolicyName, 0, defaultTimeoutSeconds, expectedMsg)
		})
		It("Should show merged messages when it is multiple namespaces", func() {
			expectedMsg := "pods [case5-multi-namespace-enforce-pod] found as specified in namespaces: n1, n2, n3"
			utils.Kubectl("apply", "-f", case5EnforceYaml)
			utils.DoConfigPolicyMessageTest(clientManagedDynamic, gvrConfigPolicy, testNamespace,
				case5MultiNSConfigPolicyName, 0, defaultTimeoutSeconds, expectedMsg)
		})
		It("Should show 3 merged messages when it is multiple namespaces and multiple obj-template", func() {
			firstMsg := "pods [case5-multi-obj-temp-pod-11] found as specified in namespaces: n1, n2, n3"
			secondMsg := "pods [case5-multi-obj-temp-pod-22] found as specified in namespaces: n1, n2, n3"
			thirdMsg := "pods [case5-multi-obj-temp-pod-33] found as specified in namespaces: n1, n2, n3"
			utils.Kubectl("apply", "-f", case5MultiObjTmpYaml)
			utils.DoConfigPolicyMessageTest(clientManagedDynamic, gvrConfigPolicy, testNamespace,
				case5MultiObjNSConfigPolicyName, 0, defaultTimeoutSeconds, firstMsg)
			utils.DoConfigPolicyMessageTest(clientManagedDynamic, gvrConfigPolicy, testNamespace,
				case5MultiObjNSConfigPolicyName, 1, defaultTimeoutSeconds, secondMsg)
			utils.DoConfigPolicyMessageTest(clientManagedDynamic, gvrConfigPolicy, testNamespace,
				case5MultiObjNSConfigPolicyName, 2, defaultTimeoutSeconds, thirdMsg)

			By("Configuration policy name " + case5NameMissPlcName +
				" should be compliant after apply " + case5MultiObjTmpYaml)

			Eventually(func(g Gomega) {
				plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case5NameMissPlcName, testNamespace, true, defaultTimeoutSeconds)

				utils.CheckComplianceStatus(g, plc, "Compliant")
			}, defaultTimeoutSeconds, 1).Should(Succeed())
		})
		cleanup := func() {
			policies := []string{
				case5MultiNSConfigPolicyName,
				case5MultiNSInformConfigPolicyName,
				case5MultiObjNSConfigPolicyName,
				case5NameMissPlcName,
				case5KindNameMissPlcName,
				case5KindMissPlcName,
			}

			deleteConfigPolicies(policies)
			nss := []string{
				case5MultiNamespace1,
				case5MultiNamespace2,
				case5MultiNamespace3,
			}

			for _, ns := range nss {
				utils.KubectlDelete("ns", ns)
			}
		}
		AfterAll(cleanup)
	})
})
