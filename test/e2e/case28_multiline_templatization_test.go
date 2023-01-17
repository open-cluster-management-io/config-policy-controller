// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package e2e

import (
	"context"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"open-cluster-management.io/config-policy-controller/test/utils"
)

const (
	case28RangePolicyName string = "case28-configpolicy"
	case28RangePolicyYaml string = "../resources/case28_multiline_templatization/case28_policy.yaml"
	case28ConfigMapsYaml  string = "../resources/case28_multiline_templatization/case28_configmaps.yaml"
)

const (
	case28Unterminated     string = "policy-pod-create-unterminated"
	case28UnterminatedYaml string = "../resources/case28_multiline_templatization/case28_unterminated.yaml"
	case28WrongArgs        string = "policy-pod-create-wrong-args"
	case28WrongArgsYaml    string = "../resources/case28_multiline_templatization/case28_wrong_args.yaml"
)

var _ = Describe("Test multiline templatization", Ordered, func() {
	Describe("Verify multiline template with range keyword", Ordered, func() {
		It("configmap should be created properly on the managed cluster", func() {
			By("Creating config maps on managed")
			utils.Kubectl("apply", "-f", case28ConfigMapsYaml, "-n", "default")
			for _, cfgMapName := range []string{"28config1", "28config2"} {
				cfgmap := utils.GetWithTimeout(clientManagedDynamic, gvrConfigMap,
					cfgMapName, "default", true, defaultTimeoutSeconds)
				Expect(cfgmap).NotTo(BeNil())
			}
		})
		It("both configmaps should be updated properly on the managed cluster", func() {
			By("Creating policy with range template on managed")
			utils.Kubectl("apply", "-f", case28RangePolicyYaml, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case28RangePolicyName, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())

			By("Verifying that the " + case28RangePolicyName + " policy is compliant")
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(
					clientManagedDynamic,
					gvrConfigPolicy,
					case28RangePolicyName,
					testNamespace,
					true,
					defaultTimeoutSeconds,
				)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))

			By("Verifying that both configmaps have the updated data")
			for _, cfgMapName := range []string{"28config1", "28config2"} {
				Eventually(
					func() interface{} {
						configMap, err := clientManaged.CoreV1().ConfigMaps("default").Get(
							context.TODO(), cfgMapName, v1.GetOptions{},
						)
						if err != nil {
							return ""
						}

						return configMap.Data["extraData"]
					},
					defaultTimeoutSeconds,
					1,
				).Should(Equal("exists!"))
			}
		})

		AfterAll(func() {
			deleteConfigPolicies([]string{case28RangePolicyName})
			utils.Kubectl("delete", "-f", case28ConfigMapsYaml)
		})
	})
	Describe("Test invalid multiline templates", func() {
		It("should generate noncompliant for invalid template strings", func() {
			By("Creating policies on managed")
			// create policy with unterminated template
			utils.Kubectl("apply", "-f", case28UnterminatedYaml, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case28Unterminated, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case28Unterminated, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("NonCompliant"))
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case28Unterminated, testNamespace, true, defaultTimeoutSeconds)

				return strings.Contains(
					utils.GetStatusMessage(managedPlc).(string),
					"unterminated character constant",
				)
			}, 10, 1).Should(BeTrue())
			// create policy with incomplete args in template
			utils.Kubectl("apply", "-f", case28WrongArgsYaml, "-n", testNamespace)
			plc = utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case28WrongArgs, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case28WrongArgs, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("NonCompliant"))
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case28WrongArgs, testNamespace, true, defaultTimeoutSeconds)

				return strings.Contains(
					utils.GetStatusMessage(managedPlc).(string),
					"wrong number of args for lookup: want 4 got 1",
				)
			}, 10, 1).Should(BeTrue())
		})
		AfterAll(func() {
			deleteConfigPolicies([]string{case28Unterminated, case28WrongArgs})
		})
	})
})
