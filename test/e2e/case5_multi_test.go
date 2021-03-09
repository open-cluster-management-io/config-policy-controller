// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project


package e2e

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/open-cluster-management/config-policy-controller/test/utils"
)

const case5ConfigPolicyNameInform string = "policy-pod-multi-mh"
const case5ConfigPolicyNameEnforce string = "policy-pod-multi-create"
const case5ConfigPolicyNameCombo string = "policy-pod-multi-combo"
const case5PodName1 string = "nginx-pod-1"
const case5PodName2 string = "nginx-pod-1"
const case5InformYaml string = "../resources/case5_multi/case5_multi_mh.yaml"
const case5EnforceYaml string = "../resources/case5_multi/case5_multi_enforce.yaml"
const case5ComboYaml string = "../resources/case5_multi/case5_multi_combo.yaml"

var _ = Describe("Test multiple obj template handling", func() {
	Describe("Create a policy on managed cluster in ns:"+testNamespace, func() {
		It("should be created properly on the managed cluster", func() {
			By("Creating " + case5ConfigPolicyNameInform + " and " + case5ConfigPolicyNameCombo + " on managed")
			utils.Kubectl("apply", "-f", case5InformYaml, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy, case5ConfigPolicyNameInform, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy, case5ConfigPolicyNameInform, testNamespace, true, defaultTimeoutSeconds)
				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("NonCompliant"))
			utils.Kubectl("apply", "-f", case5ComboYaml, "-n", testNamespace)
			plc = utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy, case5ConfigPolicyNameCombo, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy, case5ConfigPolicyNameCombo, testNamespace, true, defaultTimeoutSeconds)
				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("NonCompliant"))
		})
		It("should create pods on managed cluster", func() {
			By("creating " + case5ConfigPolicyNameEnforce + " on hub with spec.remediationAction = enforce")
			utils.Kubectl("apply", "-f", case5EnforceYaml, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy, case5ConfigPolicyNameEnforce, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy, case5ConfigPolicyNameEnforce, testNamespace, true, defaultTimeoutSeconds)
				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
			Eventually(func() interface{} {
				informPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy, case5ConfigPolicyNameInform, testNamespace, true, defaultTimeoutSeconds)
				return utils.GetComplianceState(informPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
			Eventually(func() interface{} {
				comboPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy, case5ConfigPolicyNameCombo, testNamespace, true, defaultTimeoutSeconds)
				return utils.GetComplianceState(comboPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("NonCompliant"))
			pod1 := utils.GetWithTimeout(clientManagedDynamic, gvrPod, case5PodName1, "default", true, defaultTimeoutSeconds)
			Expect(pod1).NotTo(BeNil())
			pod2 := utils.GetWithTimeout(clientManagedDynamic, gvrPod, case5PodName2, "default", true, defaultTimeoutSeconds)
			Expect(pod2).NotTo(BeNil())
		})
	})
})
