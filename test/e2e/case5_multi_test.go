// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package e2e

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"open-cluster-management.io/config-policy-controller/test/utils"
)

const (
	case5ConfigPolicyNameInform  string = "policy-pod-multi-mh"
	case5ConfigPolicyNameEnforce string = "policy-pod-multi-create"
	case5ConfigPolicyNameCombo   string = "policy-pod-multi-combo"
	case5PodName1                string = "case5-nginx-pod-1"
	case5PodName2                string = "case5-nginx-pod-2"
	case5InformYaml              string = "../resources/case5_multi/case5_multi_mh.yaml"
	case5EnforceYaml             string = "../resources/case5_multi/case5_multi_enforce.yaml"
	case5ComboYaml               string = "../resources/case5_multi/case5_multi_combo.yaml"
)

var _ = Describe("Test multiple obj template handling", Ordered, func() {
	Describe("Create a policy on managed cluster in ns:"+testNamespace, Ordered, func() {
		It("should be created properly on the managed cluster", func() {
			By("Creating " + case5ConfigPolicyNameInform + " and " + case5ConfigPolicyNameCombo + " on managed")
			utils.Kubectl("apply", "-f", case5InformYaml, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case5ConfigPolicyNameInform, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case5ConfigPolicyNameInform, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("NonCompliant"))
			utils.Kubectl("apply", "-f", case5ComboYaml, "-n", testNamespace)
			plc = utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case5ConfigPolicyNameCombo, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case5ConfigPolicyNameCombo, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("NonCompliant"))
		})
		It("should create pods on managed cluster", func() {
			By("creating " + case5ConfigPolicyNameEnforce + " on hub with spec.remediationAction = enforce")
			utils.Kubectl("apply", "-f", case5EnforceYaml, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case5ConfigPolicyNameEnforce, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case5ConfigPolicyNameEnforce, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
			Eventually(func() interface{} {
				informPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case5ConfigPolicyNameInform, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(informPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
			Eventually(func() interface{} {
				comboPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case5ConfigPolicyNameCombo, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(comboPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("NonCompliant"))
			pod1 := utils.GetWithTimeout(clientManagedDynamic, gvrPod,
				case5PodName1, "default", true, defaultTimeoutSeconds)
			Expect(pod1).NotTo(BeNil())
			pod2 := utils.GetWithTimeout(clientManagedDynamic, gvrPod,
				case5PodName2, "default", true, defaultTimeoutSeconds)
			Expect(pod2).NotTo(BeNil())
		})
		AfterAll(func() {
			policies := []string{
				case5ConfigPolicyNameInform,
				case5ConfigPolicyNameEnforce,
				case5ConfigPolicyNameCombo,
			}

			deleteConfigPolicies(policies)

			By("Delete pods")
			pods := []string{case5PodName1, case5PodName2}
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

			Eventually(func() interface{} {
				plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case5NameMissPlcName, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(plc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
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
