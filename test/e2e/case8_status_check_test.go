// Copyright (c) 2020 Red Hat, Inc.

package e2e

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/open-cluster-management/config-policy-controller/test/utils"
)

const case8ConfigPolicyNamePod string = "policy-pod-to-check"
const case8ConfigPolicyNameCheck string = "policy-status-checker"
const case8ConfigPolicyNameCheckFail string = "policy-status-checker-fail"
const case8PolicyYamlPod string = "../resources/case8_status_check/case8_pod.yaml"
const case8PolicyYamlCheck string = "../resources/case8_status_check/case8_status_check.yaml"
const case8PolicyYamlCheckFail string = "../resources/case8_status_check/case8_status_check_fail.yaml"

var _ = Describe("Test pod obj template handling", func() {
	Describe("Create a policy on managed cluster in ns:"+testNamespace, func() {
		It("should create a policy properly on the managed cluster", func() {
			By("Creating " + case8ConfigPolicyNamePod + " on managed")
			utils.Kubectl("apply", "-f", case8PolicyYamlPod, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy, case8ConfigPolicyNamePod, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy, case8ConfigPolicyNamePod, testNamespace, true, defaultTimeoutSeconds)
				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
		})
		It("should check status of the created policy", func() {
			By("Creating " + case8ConfigPolicyNameCheck + " on managed")
			utils.Kubectl("apply", "-f", case8PolicyYamlCheck, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy, case8ConfigPolicyNameCheck, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy, case8ConfigPolicyNameCheck, testNamespace, true, defaultTimeoutSeconds)
				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
		})
		It("should return nonCompliant if status does not match", func() {
			By("Creating " + case8ConfigPolicyNameCheckFail + " on managed")
			utils.Kubectl("apply", "-f", case8PolicyYamlCheckFail, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy, case8ConfigPolicyNameCheckFail, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy, case8ConfigPolicyNameCheckFail, testNamespace, true, defaultTimeoutSeconds)
				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("NonCompliant"))
		})
	})
})
