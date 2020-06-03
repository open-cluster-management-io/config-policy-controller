// Copyright (c) 2020 Red Hat, Inc.

package e2e

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/open-cluster-management/config-policy-controller/test/utils"
)

const case1ConfigPolicyNameInform string = "policy-pod-create-inform"
const case1ConfigPolicyNameEnforce string = "policy-pod-create"
const case1PodName string = "nginx-pod-e2e"
const case1PolicyYamlInform string = "../resources/case1_pod_handling/case1_pod_create_inform.yaml"
const case1PolicyYamlEnforce string = "../resources/case1_pod_handling/case1_pod_create_enforce.yaml"
const case1PolicyCheckMNHYaml string = "../resources/case1_pod_handling/case1_pod_check-mnh.yaml"
const case1PolicyCheckMOHYaml string = "../resources/case1_pod_handling/case1_pod_check-moh.yaml"

var _ = Describe("Test pod obj template handling", func() {
	Describe("Create a policy on managed cluster in ns:"+testNamespace, func() {
		It("should be created properly on the managed cluster", func() {
			By("Creating " + case1PolicyYamlInform + " on managed")
			utils.Kubectl("apply", "-f", case1PolicyYamlInform, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy, case1ConfigPolicyNameInform, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy, case1ConfigPolicyNameInform, testNamespace, true, defaultTimeoutSeconds)
				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("NonCompliant"))
		})
		It("should create pod on managed cluster", func() {
			By("creating " + case1PolicyYamlEnforce + " on hub with spec.remediationAction = enforce")
			utils.Kubectl("apply", "-f", case1PolicyYamlEnforce, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy, case1ConfigPolicyNameEnforce, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy, case1ConfigPolicyNameEnforce, testNamespace, true, defaultTimeoutSeconds)
				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
			Eventually(func() interface{} {
				informPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy, case1ConfigPolicyNameInform, testNamespace, true, defaultTimeoutSeconds)
				return utils.GetComplianceState(informPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
			pod := utils.GetWithTimeout(clientManagedDynamic, gvrPod, case1PodName, "default", true, defaultTimeoutSeconds)
			Expect(pod).NotTo(BeNil())
		})
		It("should create violations properly", func() {
			utils.Kubectl("apply", "-f", case1PolicyCheckMNHYaml, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy, "policy-pod-check-mnh", testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy, "policy-pod-check-mnh", testNamespace, true, defaultTimeoutSeconds)
				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("NonCompliant"))
			utils.Kubectl("apply", "-f", case1PolicyCheckMOHYaml, "-n", testNamespace)
			plc = utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy, "policy-pod-check-moh", testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy, "policy-pod-check-moh", testNamespace, true, defaultTimeoutSeconds)
				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("NonCompliant"))
		})
	})
})
