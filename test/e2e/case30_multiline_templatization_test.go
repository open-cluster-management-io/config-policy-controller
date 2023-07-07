// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package e2e

import (
	"context"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"open-cluster-management.io/config-policy-controller/test/utils"
)

const (
	case30RangePolicyName      string = "case30-configpolicy"
	case30NoTemplatePolicyName string = "case30-configpolicy-notemplate"
	case30RangePolicyYaml      string = "../resources/case30_multiline_templatization/case30_policy.yaml"
	case30NoTemplatePolicyYaml string = "../resources/case30_multiline_templatization/case30_policy_notemplate.yaml"
	case30ConfigMapsYaml       string = "../resources/case30_multiline_templatization/case30_configmaps.yaml"
	case30ConfigmapName1       string = "30config1"
	case30ConfigmapName2       string = "30config2"
)

const (
	case30Unterminated       string = "case30-configpolicy"
	case30UnterminatedYaml   string = "../resources/case30_multiline_templatization/case30_unterminated.yaml"
	case30WrongArgs          string = "case30-policy-pod-create-wrong-args"
	case30WrongArgsYaml      string = "../resources/case30_multiline_templatization/case30_wrong_args.yaml"
	case30NoObject           string = "case30-configpolicy-no-object"
	case30NoObjectPolicyYaml string = "../resources/case30_multiline_templatization/case30_no_object.yaml"
)

var _ = Describe("Test multiline templatization", Ordered, func() {
	Describe("Verify multiline template with range keyword", Ordered, func() {
		It("configmap should be created properly on the managed cluster", func() {
			By("Creating config maps on managed")
			utils.Kubectl("apply", "-f", case30ConfigMapsYaml, "-n", "default")
			for _, cfgMapName := range []string{"30config1", "30config2"} {
				cfgmap := utils.GetWithTimeout(clientManagedDynamic, gvrConfigMap,
					cfgMapName, "default", true, defaultTimeoutSeconds)
				Expect(cfgmap).NotTo(BeNil())
			}
		})
		It("both configmaps should be updated properly on the managed cluster", func() {
			By("Creating policy with range template on managed")
			utils.Kubectl("apply", "-f", case30RangePolicyYaml, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case30RangePolicyName, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())

			By("Verifying that the " + case30RangePolicyName + " policy is compliant")
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(
					clientManagedDynamic,
					gvrConfigPolicy,
					case30RangePolicyName,
					testNamespace,
					true,
					defaultTimeoutSeconds,
				)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))

			By("Verifying that both configmaps have the updated data")
			for _, cfgMapName := range []string{"30config1", "30config2"} {
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

		It("processes policies with no template correctly", func() {
			By("Creating policy with no template in object-templates-raw")
			utils.Kubectl("apply", "-f", case30NoTemplatePolicyYaml, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case30NoTemplatePolicyName, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())

			By("Verifying that the " + case30NoTemplatePolicyName + " policy is compliant")
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(
					clientManagedDynamic,
					gvrConfigPolicy,
					case30NoTemplatePolicyName,
					testNamespace,
					true,
					defaultTimeoutSeconds,
				)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
		})

		AfterAll(func() {
			deleteConfigPolicies([]string{case30RangePolicyName, case30NoTemplatePolicyName})
			utils.Kubectl("delete", "-f", case30ConfigMapsYaml)
			utils.Kubectl("delete", "configmap", case30ConfigmapName1,
				"-n", "default", "--ignore-not-found")
			utils.Kubectl("delete", "configmap", case30ConfigmapName2,
				"-n", "default", "--ignore-not-found")
		})
	})
	Describe("Test invalid multiline templates", func() {
		It("configmap should be created properly on the managed cluster", func() {
			By("Creating config maps on managed")
			utils.Kubectl("apply", "-f", case30ConfigMapsYaml, "-n", "default")
			for _, cfgMapName := range []string{"30config1", "30config2"} {
				cfgmap := utils.GetWithTimeout(clientManagedDynamic, gvrConfigMap,
					cfgMapName, "default", true, defaultTimeoutSeconds)
				Expect(cfgmap).NotTo(BeNil())
			}
		})
		It("should create compliant policy", func() {
			By("Creating policy with range template on managed")
			utils.Kubectl("apply", "-f", case30RangePolicyYaml, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case30RangePolicyName, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())

			By("Verifying that the " + case30RangePolicyName + " policy is compliant")
			Eventually(func(g Gomega) interface{} {
				managedPlc := utils.GetWithTimeout(
					clientManagedDynamic,
					gvrConfigPolicy,
					case30RangePolicyName,
					testNamespace,
					true,
					defaultTimeoutSeconds,
				)

				details, _, err := unstructured.NestedSlice(managedPlc.Object, "status", "compliancyDetails")
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(details).To(HaveLen(2))

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
		})

		It("should generate noncompliant for invalid template strings", func() {
			By("Creating policies on managed")
			// create policy with unterminated template
			utils.Kubectl("apply", "-f", case30UnterminatedYaml, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case30Unterminated, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func(g Gomega) interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case30Unterminated, testNamespace, true, defaultTimeoutSeconds)

				details, _, err := unstructured.NestedSlice(managedPlc.Object, "status", "compliancyDetails")
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(details).To(HaveLen(1))

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("NonCompliant"))
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case30Unterminated, testNamespace, true, defaultTimeoutSeconds)

				return strings.Contains(
					utils.GetStatusMessage(managedPlc).(string),
					"unterminated character constant",
				)
			}, 10, 1).Should(BeTrue())
			// create policy with incomplete args in template
			utils.Kubectl("apply", "-f", case30WrongArgsYaml, "-n", testNamespace)
			plc = utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case30WrongArgs, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case30WrongArgs, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("NonCompliant"))
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case30WrongArgs, testNamespace, true, defaultTimeoutSeconds)

				return strings.Contains(
					utils.GetStatusMessage(managedPlc).(string),
					"wrong number of args for lookup: want at least 4 got 1",
				)
			}, 10, 1).Should(BeTrue())
		})
		It("should be compliant when no templates are specified", func() {
			By("Creating policies on managed")
			// create policy with unterminated template
			utils.Kubectl("apply", "-f", case30NoObjectPolicyYaml, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case30NoObject, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case30NoObject, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case30NoObject, testNamespace, true, defaultTimeoutSeconds)

				return strings.Contains(
					utils.GetStatusMessage(managedPlc).(string),
					"contains no object templates to check",
				)
			}, 10, 1).Should(BeTrue())
		})
		AfterAll(func() {
			deleteConfigPolicies([]string{case30Unterminated, case30WrongArgs, case30NoObject})
		})
	})
})
