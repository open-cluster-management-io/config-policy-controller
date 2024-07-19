// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package e2e

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"open-cluster-management.io/config-policy-controller/test/utils"
)

const (
	case7ConfigPolicyName        string = "policy-securitycontextconstraints-1-sample-restricted-scc"
	case7ObjName                 string = "sample-restricted-scc"
	case7PolicyYaml              string = "../resources/case7_no_spec/case7_no_spec_enforce.yaml"
	case7ConfigPolicyNameNull    string = "policy-securitycontextconstraints-1-sample-restricted-scc-null"
	case7PolicyYamlNull          string = "../resources/case7_no_spec/case7_no_spec_enforce_null.yaml"
	case7ConfigPolicyNameInvalid string = "policy-securitycontextconstraints-1-sample-restricted-scc-invalid"
	case7PolicyYamlInvalid       string = "../resources/case7_no_spec/case7_no_spec_invalid_type.yaml"
	//nolint:lll
	case7ConfigPolicyNameInvalidInform string = "policy-securitycontextconstraints-1-sample-restricted-scc-invalid-inform"
	case7PolicyYamlInvalidInform       string = "../resources/case7_no_spec/case7_no_spec_invalid_type_inform.yaml"
)

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

// GetPriority parses status field of object to get priority, a nullable field.
// if updateTemplate fails the field will be null
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
	Describe("create scc policy in namespace "+testNamespace, Ordered, func() {
		It("should be created properly on the managed cluster", func() {
			By("Creating " + case7ConfigPolicyName + " on managed")
			utils.Kubectl("apply", "-f", case7PolicyYaml, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case7ConfigPolicyName, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func(g Gomega) {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case7ConfigPolicyName, testNamespace, true, defaultTimeoutSeconds)

				utils.CheckComplianceStatus(g, managedPlc, "Compliant")
			}, defaultTimeoutSeconds, 1).Should(Succeed())
			utils.KubectlDelete("configurationpolicy", case7ConfigPolicyName, "-n", testNamespace)
		})
		It("should handle nullable fields properly", func() {
			Consistently(func() interface{} {
				managedObj := utils.GetClusterLevelWithTimeout(clientManagedDynamic, gvrSCC,
					case7ObjName, true, defaultTimeoutSeconds)

				return matchToExpected(managedObj)
			}, defaultConsistentlyDuration, 1).Should(Equal(true))
		})
		It("should handle change field to null", func() {
			By("Creating " + case7ConfigPolicyNameNull + " on managed")
			utils.Kubectl("apply", "-f", case7PolicyYamlNull, "-n", testNamespace, "--validate=false")
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case7ConfigPolicyNameNull, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func(g Gomega) {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case7ConfigPolicyNameNull, testNamespace, true, defaultTimeoutSeconds)

				utils.CheckComplianceStatus(g, managedPlc, "Compliant")
			}, defaultTimeoutSeconds, 1).Should(Succeed())
			utils.KubectlDelete("configurationpolicy", case7ConfigPolicyNameNull, "-n", testNamespace)
			expectedObj["priority"] = nil
			Eventually(func() interface{} {
				managedObj := utils.GetClusterLevelWithTimeout(clientManagedDynamic, gvrSCC,
					case7ObjName, true, defaultTimeoutSeconds)

				return matchToExpected(managedObj)
			}, defaultTimeoutSeconds, 1).Should(Equal(true))
		})
		It("should change field back to 10", func() {
			By("Creating " + case7ConfigPolicyName + " on managed")
			utils.Kubectl("apply", "-f", case7PolicyYaml, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case7ConfigPolicyName, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func(g Gomega) {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case7ConfigPolicyName, testNamespace, true, defaultTimeoutSeconds)

				utils.CheckComplianceStatus(g, managedPlc, "Compliant")
			}, defaultTimeoutSeconds, 1).Should(Succeed())
			expectedObj["priority"] = int64(10)
			Eventually(func() interface{} {
				managedObj := utils.GetClusterLevelWithTimeout(clientManagedDynamic, gvrSCC,
					case7ObjName, true, defaultTimeoutSeconds)

				return matchToExpected(managedObj)
			}, defaultTimeoutSeconds, 1).Should(Equal(true))
			utils.KubectlDelete("configurationpolicy", case7ConfigPolicyName, "-n", testNamespace)
		})
		It("should generate violation if field type is invalid (enforce)", func() {
			By("Creating " + case7ConfigPolicyNameInvalid + " on managed")
			utils.Kubectl("apply", "-f", case7PolicyYamlInvalid, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case7ConfigPolicyNameInvalid, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func(g Gomega) {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case7ConfigPolicyNameInvalid, testNamespace, true, defaultTimeoutSeconds)

				utils.CheckComplianceStatus(g, managedPlc, "NonCompliant")
			}, defaultTimeoutSeconds, 1).Should(Succeed())
		})
		It("should generate violation if field type is invalid (inform)", func() {
			By("Creating " + case7ConfigPolicyNameInvalidInform + " on managed")
			utils.Kubectl("apply", "-f", case7PolicyYamlInvalidInform, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case7ConfigPolicyNameInvalidInform, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func(g Gomega) {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case7ConfigPolicyNameInvalidInform, testNamespace, true, defaultTimeoutSeconds)

				utils.CheckComplianceStatus(g, managedPlc, "NonCompliant")
			}, defaultTimeoutSeconds, 1).Should(Succeed())
		})
		AfterAll(func() {
			policies := []string{
				case7ConfigPolicyName,
				case7ConfigPolicyNameNull,
				case7ConfigPolicyNameInvalid,
				case7ConfigPolicyNameInvalidInform,
			}

			deleteConfigPolicies(policies)
		})
	})
})
