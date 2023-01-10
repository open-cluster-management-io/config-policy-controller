// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package e2e

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"open-cluster-management.io/config-policy-controller/test/utils"
)

const (
	case1ConfigPolicyNameInform       string = "policy-pod-create-inform"
	case1ConfigPolicyNameEnforce      string = "policy-pod-create"
	case1PodName                      string = "nginx-pod-e2e"
	case1PolicyYamlInform             string = "../resources/case1_pod_handling/case1_pod_create_inform.yaml"
	case1PolicyYamlEnforce            string = "../resources/case1_pod_handling/case1_pod_create_enforce.yaml"
	case1PolicyCheckMNHYaml           string = "../resources/case1_pod_handling/case1_pod_check-mnh.yaml"
	case1PolicyCheckMOHYaml           string = "../resources/case1_pod_handling/case1_pod_check-moh.yaml"
	case1PolicyCheckMHYaml            string = "../resources/case1_pod_handling/case1_pod_check-mh.yaml"
	case1PolicyYamlEnforceEmpty       string = "../resources/case1_pod_handling/case1_pod_create_empty_list.yaml"
	case1PolicyYamlInformEmpty        string = "../resources/case1_pod_handling/case1_pod_check_empty_list.yaml"
	case1PolicyCheckMNHIncompleteYaml string = "../resources/case1_pod_handling/case1_pod_check-mnh-incomplete.yaml"
	case1PolicyYamlMultipleCreate     string = "../resources/case1_pod_handling/case1_pod_create_multiple.yaml"
	case1PolicyYamlMultipleCheckMH    string = "../resources/case1_pod_handling/case1_pod_check_multiple_mh.yaml"
)

var _ = Describe("Test pod obj template handling", func() {
	Describe("Create a policy on managed cluster in ns:"+testNamespace, func() {
		It("should be created properly on the managed cluster", func() {
			By("Creating " + case1PolicyYamlInform + " on managed")
			utils.Kubectl("apply", "-f", case1PolicyYamlInform, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case1ConfigPolicyNameInform, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case1ConfigPolicyNameInform, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("NonCompliant"))
		})
		It("should create pod on managed cluster", func() {
			By("creating " + case1PolicyYamlEnforce + " on hub with spec.remediationAction = enforce")
			utils.Kubectl("apply", "-f", case1PolicyYamlEnforce, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case1ConfigPolicyNameEnforce, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())

			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case1ConfigPolicyNameEnforce, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))

			By("verifying status.compliancyDetails[].conditions")
			Eventually(func(g Gomega) {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case1ConfigPolicyNameEnforce, testNamespace, true, defaultTimeoutSeconds)

				compliancyDetails, _, _ := unstructured.NestedSlice(managedPlc.Object, "status", "compliancyDetails")
				g.Expect(compliancyDetails).To(HaveLen(1))

				conditions, ok := compliancyDetails[0].(map[string]interface{})["conditions"].([]interface{})
				g.Expect(ok).To(BeTrue())
				g.Expect(conditions).To(HaveLen(3))
				g.Expect(conditions[0].(map[string]interface{})["reason"]).To(
					Equal("K8s does not have a `must have` object"),
				)
				g.Expect(conditions[1].(map[string]interface{})["reason"]).To(Equal("K8s creation success"))
				g.Expect(conditions[2].(map[string]interface{})["reason"]).To(
					Equal("K8s `must have` object already exists"),
				)
			}, defaultTimeoutSeconds, 1).Should(Succeed())

			By("verifying that " + case1ConfigPolicyNameInform + " is compliant")
			Eventually(func() interface{} {
				informPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case1ConfigPolicyNameInform, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(informPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
			pod := utils.GetWithTimeout(clientManagedDynamic, gvrPod,
				case1PodName, "default", true, defaultTimeoutSeconds)
			Expect(pod).NotTo(BeNil())
			utils.Kubectl("apply", "-f", case1PolicyCheckMHYaml, "-n", testNamespace)
			plcMH := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				"policy-pod-check-mh-list", testNamespace, true, defaultTimeoutSeconds)
			Expect(plcMH).NotTo(BeNil())
			Eventually(func() interface{} {
				mHPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					"policy-pod-check-mh-list", testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(mHPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
			utils.Kubectl("apply", "-f", case1PolicyYamlEnforceEmpty, "-n", testNamespace)
			plcEmpty := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				"policy-pod-emptycontainerlist", testNamespace, true, defaultTimeoutSeconds)
			Expect(plcEmpty).NotTo(BeNil())
			Eventually(func() interface{} {
				emptyPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					"policy-pod-emptycontainerlist", testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(emptyPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
			utils.Kubectl("apply", "-f", case1PolicyYamlMultipleCreate, "-n", testNamespace)
			plcMultiple := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				"policy-pod-create-multiple", testNamespace, true, defaultTimeoutSeconds)
			Expect(plcMultiple).NotTo(BeNil())
			Eventually(func() interface{} {
				multiPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					"policy-pod-create-multiple", testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(multiPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
		})
		It("should create violations properly", func() {
			utils.Kubectl("apply", "-f", case1PolicyCheckMNHYaml, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				"policy-pod-check-mnh", testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					"policy-pod-check-mnh", testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("NonCompliant"))
			utils.Kubectl("apply", "-f", case1PolicyCheckMNHIncompleteYaml, "-n", testNamespace)
			plc = utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				"policy-pod-check-mnh-incomplete", testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					"policy-pod-check-mnh-incomplete", testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("NonCompliant"))
			utils.Kubectl("apply", "-f", case1PolicyCheckMOHYaml, "-n", testNamespace)
			plc = utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				"policy-pod-check-moh", testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					"policy-pod-check-moh", testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("NonCompliant"))
			utils.Kubectl("apply", "-f", case1PolicyYamlInformEmpty, "-n", testNamespace)
			plc = utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				"policy-pod-check-emptycontainerlist", testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					"policy-pod-check-emptycontainerlist", testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("NonCompliant"))
			utils.Kubectl("apply", "-f", case1PolicyYamlMultipleCheckMH, "-n", testNamespace)
			plc = utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				"policy-pod-check-multiple-mh", testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					"policy-pod-check-multiple-mh", testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
		})
		It("Cleans up", func() {
			policies := []string{
				case1ConfigPolicyNameInform,
				case1ConfigPolicyNameEnforce,
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
		})
	})
})
