// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package e2e

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"open-cluster-management.io/config-policy-controller/test/utils"
)

const (
	case19PolicyName  string = "policy-configmap-selector-e2e"
	case19PolicyYaml  string = "../resources/case19_ns_selector/case19_cm_policy.yaml"
	case19PrereqYaml  string = "../resources/case19_ns_selector/case19_cm_manifest.yaml"
	case19PatchPrefix string = "[{\"op\":\"replace\",\"path\":\"/spec/namespaceSelector\",\"value\":"
	case19PatchSuffix string = "}]"
)

// Test setup for namespace selection policy tests:
// - Namespaces `case19-[1-5]-e2e`, each with a `name: <ns-name>` label
// - Single deployed Configmap `configmap-selector-e2e` in namespace `case19-1-e2e`
// - Deployed policy should be compliant since it matches the single deployed ConfigMap
// - Policies are patched so that the namespace doesn't match and should be NonCompliant
var _ = Describe("Test object namespace selection", Ordered, func() {
	// NamespaceSelector patches to test
	resetPatch := "{\"include\":[\"case19-1-e2e\"]}"
	allPatch := "{\"matchExpressions\":[{\"key\":\"name\",\"operator\":\"Exists\"}]}"
	patches := map[string]struct {
		patch   string
		message string
	}{
		"no namespaceSelector specified": {
			"{}",
			"namespaced object has no namespace specified" +
				" from the policy namespaceSelector nor the object metadata",
		},
		"a non-matching LabelSelector": {
			"{\"matchLabels\":{\"name\":\"not-a-namespace\"}}",
			"namespaced object has no namespace specified" +
				" from the policy namespaceSelector nor the object metadata",
		},
		"LabelSelector and exclude": {
			"{\"exclude\":[\"*-[3-4]-e2e\"],\"matchLabels\":{}," +
				"\"matchExpressions\":[{\"key\":\"name\",\"operator\":\"Exists\"}]}",
			"configmaps not found: [configmap-selector-e2e] in namespace case19-2-e2e missing; " +
				"[configmap-selector-e2e] in namespace case19-5-e2e missing",
		},
		"empty LabelSelector and include/exclude": {
			"{\"include\":[\"case19-[2-5]-e2e\"],\"exclude\":[\"*-[3-4]-e2e\"]," +
				"\"matchLabels\":{},\"matchExpressions\":[]}",
			"configmaps not found: [configmap-selector-e2e] in namespace case19-2-e2e missing; " +
				"[configmap-selector-e2e] in namespace case19-5-e2e missing",
		},
		"LabelSelector": {
			"{\"matchExpressions\":[{\"key\":\"name\",\"operator\":\"Exists\"}]}",
			"configmaps not found: [configmap-selector-e2e] in namespace case19-2-e2e missing; " +
				"[configmap-selector-e2e] in namespace case19-3-e2e missing; " +
				"[configmap-selector-e2e] in namespace case19-4-e2e missing; " +
				"[configmap-selector-e2e] in namespace case19-5-e2e missing",
		},
		"Malformed filepath in include": {
			"{\"include\":[\"*-[a-z-*\"]}",
			"Error filtering namespaces with provided namespaceSelector: " +
				"error parsing 'include' pattern '*-[a-z-*': syntax error in pattern",
		},
		"MatchExpressions with incorrect operator": {
			"{\"matchExpressions\":[{\"key\":\"name\",\"operator\":\"Seriously\"}]}",
			"Error filtering namespaces with provided namespaceSelector: " +
				"error parsing namespace LabelSelector: \"Seriously\" is not a valid pod selector operator",
		},
		"MatchExpressions with missing values": {
			"{\"matchExpressions\":[{\"key\":\"name\",\"operator\":\"In\",\"values\":[]}]}",
			"Error filtering namespaces with provided namespaceSelector: " +
				"error parsing namespace LabelSelector: " +
				"values: Invalid value: []string(nil): for 'in', 'notin' operators, values set can't be empty",
		},
	}

	It("creates prerequisite objects", func() {
		utils.Kubectl("apply", "-f", case19PrereqYaml)
		// Delete the last namespace so we can use it to test whether
		// adding a namespace works as the final test
		utils.Kubectl("delete", "namespace", "case19-6-e2e")
		utils.Kubectl("apply", "-f", case19PolicyYaml, "-n", testNamespace)
		plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
			case19PolicyName, testNamespace, true, defaultTimeoutSeconds)
		Expect(plc).NotTo(BeNil())
	})

	It("should properly handle the namespaceSelector", func() {
		for name, patch := range patches {
			By("patching compliant policy " + case19PolicyName + " on the managed cluster")
			utils.Kubectl("patch", "--namespace=managed", "configurationpolicy", case19PolicyName, "--type=json",
				"--patch="+case19PatchPrefix+resetPatch+case19PatchSuffix,
			)
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case19PolicyName, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
			By("patching with " + name)
			utils.Kubectl("patch", "--namespace=managed", "configurationpolicy", case19PolicyName, "--type=json",
				"--patch="+case19PatchPrefix+patch.patch+case19PatchSuffix,
			)
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case19PolicyName, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("NonCompliant"))
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case19PolicyName, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetStatusMessage(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal(patch.message))
		}
	})

	It("should handle when a matching labeled namespace is added", func() {
		utils.Kubectl("apply", "-f", case19PrereqYaml)
		By("patching with a patch for all namespaces")
		utils.Kubectl("patch", "--namespace=managed", "configurationpolicy", case19PolicyName, "--type=json",
			"--patch="+case19PatchPrefix+allPatch+case19PatchSuffix,
		)
		Eventually(func() interface{} {
			managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case19PolicyName, testNamespace, true, defaultTimeoutSeconds)

			return utils.GetStatusMessage(managedPlc)
		}, defaultTimeoutSeconds, 1).Should(ContainSubstring(
			"[configmap-selector-e2e] in namespace case19-6-e2e missing"))
	})

	AfterAll(func() {
		utils.Kubectl("delete", "-f", case19PrereqYaml)
		policies := []string{
			case19PolicyName,
		}
		deleteConfigPolicies(policies)
	})
})
