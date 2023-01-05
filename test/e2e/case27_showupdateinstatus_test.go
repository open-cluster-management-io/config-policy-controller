// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package e2e

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"open-cluster-management.io/config-policy-controller/test/utils"
)

const (
	case27ConfigPolicyName string = "policy-pod-create"
	case27CreateYaml       string = "../resources/case27_showupdateinstatus/case27-create-pod-policy.yaml"
	case27UpdateYaml       string = "../resources/case27_showupdateinstatus/case27-update-pod-policy.yaml"
)

var _ = Describe("Test cluster version obj template handling", func() {
	Describe("enforce patch on unnamespaced resource clusterversion "+testNamespace, func() {
		It("should be created properly on the managed cluster", func() {
			By("Creating " + case27ConfigPolicyName + " on managed")
			utils.Kubectl("apply", "-f", case27CreateYaml, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case27ConfigPolicyName, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case27ConfigPolicyName, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetStatusMessage(managedPlc)
			}, 120, 1).Should(Equal(
				"pods [nginx-pod-e2e] in namespace default found as specified, therefore this Object template is compliant"))
		})
		It("should be updated properly on the managed cluster", func() {
			By("Creating " + case27ConfigPolicyName + " on managed")
			utils.Kubectl("apply", "-f", case27UpdateYaml, "-n", testNamespace)
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case27ConfigPolicyName, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetStatusMessage(managedPlc)
			}, 30, 0.5).Should(Equal(
				"pods [nginx-pod-e2e] in namespace default was updated successfully"))
		})
		It("Cleans up", func() {
			deleteConfigPolicies([]string{case27ConfigPolicyName})
			utils.Kubectl("delete", "pod", "nginx-pod-e2e")
		})
	})
})
