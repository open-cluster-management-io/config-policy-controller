// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package e2e

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"open-cluster-management.io/config-policy-controller/test/utils"
)

const (
	case27ConfigPolicyName string = "case27-policy-cfgmap-create"
	case27CreateYaml       string = "../resources/case27_showupdateinstatus/case27-create-cfgmap-policy.yaml"
	case27UpdateYaml       string = "../resources/case27_showupdateinstatus/case27-update-cfgmap-policy.yaml"
)

var _ = Describe("Verify status update after updating object", Ordered, func() {
	It("configmap should be created properly on the managed cluster", func() {
		By("Creating " + case27ConfigPolicyName + " on managed")
		utils.Kubectl("apply", "-f", case27CreateYaml, "-n", testNamespace)
		plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
			case27ConfigPolicyName, testNamespace, true, defaultTimeoutSeconds)
		Expect(plc).NotTo(BeNil())
		Eventually(func() interface{} {
			managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case27ConfigPolicyName, testNamespace, true, defaultTimeoutSeconds)

			return utils.GetStatusMessage(managedPlc)
		}, 120, 1).Should(Equal("configmaps [case27-map] found as specified in namespace default"))
	})
	It("configmap and status should be updated properly on the managed cluster", func() {
		By("Updating " + case27ConfigPolicyName + " on managed")
		utils.Kubectl("apply", "-f", case27UpdateYaml, "-n", testNamespace)
		Eventually(func() interface{} {
			managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case27ConfigPolicyName, testNamespace, true, defaultTimeoutSeconds)

			return utils.GetStatusMessage(managedPlc)
		}, 30, 0.5).Should(Equal("configmaps [case27-map] was updated successfully in namespace default"))
	})

	AfterAll(func() {
		deleteConfigPolicies([]string{case27ConfigPolicyName})
		utils.Kubectl("delete", "configmap", "case27-map")
	})
})
