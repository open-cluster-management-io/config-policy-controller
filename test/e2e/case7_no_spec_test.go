// Copyright (c) 2020 Red Hat, Inc.

package e2e

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/open-cluster-management/config-policy-controller/test/utils"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const case7ConfigPolicyName string = "policy-securitycontextconstraints-1-sample-restricted-scc"
const case7ObjName string = "sample-restricted-scc"
const case7PolicyYaml string = "../resources/case7_no_spec/case7_no_spec_enforce.yaml"

// GetPriority parses status field of object to get priority, a nullable field. if updateTemplate fails the field will be null
func GetPriority(managedPlc *unstructured.Unstructured) (result interface{}) {
	if managedPlc.Object["priority"] != nil {
		return managedPlc.Object["priority"]
	}
	return nil
}

var _ = Describe("Test cluster version obj template handling", func() {
	Describe("create scc policy in namespace "+testNamespace, func() {
		It("should be created properly on the managed cluster", func() {
			By("Creating " + case7ConfigPolicyName + " on managed")
			utils.Kubectl("apply", "-f", case7PolicyYaml, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy, case7ConfigPolicyName, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy, case7ConfigPolicyName, testNamespace, true, defaultTimeoutSeconds)
				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
		})
		It("should handle nullable fields properly", func() {
			Consistently(func() interface{} {
				managedObj := utils.GetClusterLevelWithTimeout(clientManagedDynamic, gvrSCC, case7ObjName, true, defaultTimeoutSeconds)
				return GetPriority(managedObj)
			}, defaultTimeoutSeconds, 1).Should(Equal(int64(10)))
		})
	})
})
