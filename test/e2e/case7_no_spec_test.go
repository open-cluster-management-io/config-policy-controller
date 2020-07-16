// Copyright (c) 2020 Red Hat, Inc.

package e2e

import (
	"fmt"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/open-cluster-management/config-policy-controller/test/utils"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const case7ConfigPolicyName string = "policy-securitycontextconstraints-1-sample-restricted-scc"
const case7ObjName string = "sample-restricted-scc"
const case7PolicyYaml string = "../resources/case7_no_spec/case7_no_spec_enforce.yaml"
const case7ConfigPolicyNameNull string = "policy-securitycontextconstraints-1-sample-restricted-scc-null"
const case7PolicyYamlNull string = "../resources/case7_no_spec/case7_no_spec_enforce_null.yaml"

var expectedObj = map[string]interface{}{
	"allowHostDirVolumePlugin": false,
	"allowHostIPC":             false,
	"allowHostNetwork":         false,
	"allowHostPID":             false,
	"allowHostPorts":           false,
	"allowPrivilegeEscalation": true,
	"allowPrivilegedContainer": false,
	"allowedCapabilities":      []string{},
	"apiVersion":               "security.openshift.io/v1",
	"defaultAddCapabilities":   []string{},
	"fsGroup": map[string]string{
		"type": "MustRunAs",
	},
	"groups": []string{
		"system:authenticated",
	},
	"kind":                   "SecurityContextConstraints",
	"priority":               int64(10),
	"readOnlyRootFilesystem": false,
	"requiredDropCapabilities": []string{
		"KILL",
		"MKNOD",
		"SETUID",
		"SETGID",
	},
	"runAsUser": map[string]string{
		"type": "MustRunAsRange",
	},
	"seLinuxContext": map[string]string{
		"type": "MustRunAs",
	},
	"supplementalGroups": map[string]string{
		"type": "RunAsAny",
	},
	"users": []interface{}{},
	"volumes": []string{
		"configMap",
		"downwardAPI",
		"emptyDir",
		"persistentVolumeClaim",
		"projected",
		"secret",
	},
}

// GetPriority parses status field of object to get priority, a nullable field. if updateTemplate fails the field will be null
func matchToExpected(managedPlc *unstructured.Unstructured) (result bool) {
	createdObj := managedPlc.Object
	r := true
	for key, val := range expectedObj {
		if fmt.Sprintf("%v", createdObj[key]) != fmt.Sprintf("%v", val) {
			r = false
		}
	}
	return r
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
			utils.Kubectl("delete", "configurationpolicy", case7ConfigPolicyName, "-n", testNamespace)
		})
		It("should handle nullable fields properly", func() {
			Consistently(func() interface{} {
				managedObj := utils.GetClusterLevelWithTimeout(clientManagedDynamic, gvrSCC, case7ObjName, true, defaultTimeoutSeconds)
				return matchToExpected(managedObj)
			}, defaultTimeoutSeconds, 1).Should(Equal(true))
		})
		It("should handle change field to null", func() {
			By("Creating " + case7ConfigPolicyNameNull + " on managed")
			utils.Kubectl("apply", "-f", case7PolicyYamlNull, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy, case7ConfigPolicyNameNull, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy, case7ConfigPolicyNameNull, testNamespace, true, defaultTimeoutSeconds)
				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
			utils.Kubectl("delete", "configurationpolicy", case7ConfigPolicyNameNull, "-n", testNamespace)
			expectedObj["priority"] = nil
			Eventually(func() interface{} {
				managedObj := utils.GetClusterLevelWithTimeout(clientManagedDynamic, gvrSCC, case7ObjName, true, defaultTimeoutSeconds)
				return matchToExpected(managedObj)
			}, defaultTimeoutSeconds, 1).Should(Equal(true))
		})
		It("should change field back to 10", func() {
			By("Creating " + case7ConfigPolicyName + " on managed")
			utils.Kubectl("apply", "-f", case7PolicyYaml, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy, case7ConfigPolicyName, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy, case7ConfigPolicyName, testNamespace, true, defaultTimeoutSeconds)
				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
			expectedObj["priority"] = int64(10)
			Eventually(func() interface{} {
				managedObj := utils.GetClusterLevelWithTimeout(clientManagedDynamic, gvrSCC, case7ObjName, true, defaultTimeoutSeconds)
				return matchToExpected(managedObj)
			}, defaultTimeoutSeconds, 1).Should(Equal(true))
		})
	})
})
