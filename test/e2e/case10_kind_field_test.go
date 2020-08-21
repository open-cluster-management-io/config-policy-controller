// Copyright (c) 2020 Red Hat, Inc.

package e2e

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/open-cluster-management/config-policy-controller/test/utils"
)

const case10ConfigPolicyNamePod string = "policy-pod-c10-create"
const case10ConfigPolicyNameCheck string = "policy-kind-labels"
const case10ConfigPolicyNameFail string = "policy-kind-labels-fail"
const case10PolicyYamlPod string = "../resources/case10_kind_field/case10_pod_create.yaml"
const case10PolicyYamlCheck string = "../resources/case10_kind_field/case10_kind_check.yaml"
const case10PolicyYamlFail string = "../resources/case10_kind_field/case10_kind_fail.yaml"

var _ = Describe("Test pod obj template handling", func() {
	Describe("Create a pod policy on managed cluster in ns:"+testNamespace, func() {
		It("should create a policy properly on the managed cluster", func() {
			By("Creating " + case10ConfigPolicyNamePod + " on managed")
			utils.Kubectl("apply", "-f", case10PolicyYamlPod, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy, case10ConfigPolicyNamePod, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy, case10ConfigPolicyNamePod, testNamespace, true, defaultTimeoutSeconds)
				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
		})
		It("should check annotations of all pods", func() {
			By("Creating " + case10ConfigPolicyNameCheck + " on managed")
			utils.Kubectl("apply", "-f", case10PolicyYamlCheck, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy, case10ConfigPolicyNameCheck, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy, case10ConfigPolicyNameCheck, testNamespace, true, defaultTimeoutSeconds)
				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
		})
		It("should return noncompliant if no pods match annotations", func() {
			By("Creating " + case10ConfigPolicyNameFail + " on managed")
			utils.Kubectl("apply", "-f", case10PolicyYamlFail, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy, case10ConfigPolicyNameFail, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy, case10ConfigPolicyNameFail, testNamespace, true, defaultTimeoutSeconds)
				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("NonCompliant"))
		})
	})
})
