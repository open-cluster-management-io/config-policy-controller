// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package e2e

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"open-cluster-management.io/config-policy-controller/test/utils"
)

const (
	case9ConfigPolicyNamePod           string = "policy-pod-c9-create"
	case9ConfigPolicyNameAnno          string = "policy-pod-anno"
	case9ConfigPolicyNameNoAnno        string = "policy-pod-no-anno"
	case9ConfigPolicyNameLabelPatch    string = "policy-label-patch"
	case9ConfigPolicyNameLabelCheck    string = "policy-label-check"
	case9ConfigPolicyNameLabelAuto     string = "policy-label-check-auto"
	case9ConfigPolicyNameNSCreate      string = "policy-c9-create-ns"
	case9ConfigPolicyNameIgnoreLabels  string = "policy-ignore-labels"
	case9MultiAnnoNSCreate             string = "policy-create-ns-multiple-annotations"
	case9CheckNSMusthave               string = "policy-check-ns-mdcomptype-mh"
	case9CheckNSMustonlyhave           string = "policy-check-ns-mdcomptype-moh"
	case9PolicyYamlPod                 string = "../resources/case9_md_check/case9_pod_create.yaml"
	case9PolicyYamlAnno                string = "../resources/case9_md_check/case9_annos.yaml"
	case9PolicyYamlNoAnno              string = "../resources/case9_md_check/case9_no_annos.yaml"
	case9PolicyYamlLabelPatch          string = "../resources/case9_md_check/case9_label_patch.yaml"
	case9PolicyYamlLabelCheck          string = "../resources/case9_md_check/case9_label_check.yaml"
	case9PolicyYamlLabelAuto           string = "../resources/case9_md_check/case9_label_check_auto.yaml"
	case9PolicyYamlIgnoreLabels        string = "../resources/case9_md_check/case9_mustonlyhave_nolabels.yaml"
	case9PolicyYamlNSCreate            string = "../resources/case9_md_check/case9_ns_create.yaml"
	case9PolicyYamlMultiAnnoNSCreate   string = "../resources/case9_md_check/case9_multianno_ns_create.yaml"
	case9PolicyYamlCheckNSMusthave     string = "../resources/case9_md_check/case9_checkns-md-mh.yaml"
	case9PolicyYamlCheckNSMustonlyhave string = "../resources/case9_md_check/case9_checkns-md-moh.yaml"
)

var _ = Describe("Test pod obj template handling", func() {
	Describe("Create a pod policy on managed cluster in ns:"+testNamespace, func() {
		It("should create a policy properly on the managed cluster", func() {
			By("Creating " + case9ConfigPolicyNamePod + " on managed")
			utils.Kubectl("apply", "-f", case9PolicyYamlPod, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case9ConfigPolicyNamePod, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case9ConfigPolicyNamePod, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
		})
		It("should check annotations of the created policy", func() {
			By("Creating " + case9ConfigPolicyNameAnno + " on managed")
			utils.Kubectl("apply", "-f", case9PolicyYamlAnno, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case9ConfigPolicyNameAnno, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case9ConfigPolicyNameAnno, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("NonCompliant"))
		})
		It("should return compliant if lack of annotations matches", func() {
			By("Creating " + case9ConfigPolicyNameNoAnno + " on managed")
			utils.Kubectl("apply", "-f", case9PolicyYamlNoAnno, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case9ConfigPolicyNameNoAnno, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case9ConfigPolicyNameNoAnno, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
		})
		It("should patch labels/annotations properly if enforce", func() {
			By("Creating " + case9ConfigPolicyNameLabelPatch + " on managed")
			utils.Kubectl("apply", "-f", case9PolicyYamlLabelPatch, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case9ConfigPolicyNameLabelPatch, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case9ConfigPolicyNameLabelPatch, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
		})
		It("should patch verify the patched label exists as expected", func() {
			By("Creating " + case9ConfigPolicyNameLabelCheck + " on managed")
			utils.Kubectl("apply", "-f", case9PolicyYamlLabelCheck, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case9ConfigPolicyNameLabelCheck, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case9ConfigPolicyNameLabelCheck, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
		})
		It("should ignore autogenerated annotations", func() {
			By("Creating " + case9ConfigPolicyNameLabelAuto + " on managed")
			utils.Kubectl("apply", "-f", case9PolicyYamlLabelAuto, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case9ConfigPolicyNameLabelAuto, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case9ConfigPolicyNameLabelAuto, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
		})
		It("should create a namespace with labels and annotations", func() {
			By("Creating " + case9ConfigPolicyNameNSCreate + " on managed")
			utils.Kubectl("apply", "-f", case9PolicyYamlNSCreate, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case9ConfigPolicyNameNSCreate, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case9ConfigPolicyNameNSCreate, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
		})
		It("should ignore labels and annotations if none are specified in the template", func() {
			By("Creating " + case9ConfigPolicyNameIgnoreLabels + " on managed")
			utils.Kubectl("apply", "-f", case9PolicyYamlIgnoreLabels, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case9ConfigPolicyNameIgnoreLabels, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case9ConfigPolicyNameIgnoreLabels, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
		})
		It("Cleans up", func() {
			policies := []string{
				case9ConfigPolicyNamePod,
				case9ConfigPolicyNameAnno,
				case9ConfigPolicyNameNoAnno,
				case9ConfigPolicyNameLabelPatch,
				case9ConfigPolicyNameLabelCheck,
				case9ConfigPolicyNameLabelAuto,
				case9ConfigPolicyNameNSCreate,
				case9ConfigPolicyNameIgnoreLabels,
			}

			deleteConfigPolicies(policies)
		})
	})
	Describe("Create a namespace policy on managed cluster in ns:"+testNamespace, func() {
		It("should create a namespace with multiple annotations on the managed cluster", func() {
			By("Creating " + case9MultiAnnoNSCreate + " on managed")
			utils.Kubectl("apply", "-f", case9PolicyYamlMultiAnnoNSCreate, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case9MultiAnnoNSCreate, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case9MultiAnnoNSCreate, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
			utils.Kubectl("delete", "configurationpolicy", case9MultiAnnoNSCreate, "-n", testNamespace)
		})
		It("should be compliant if metadataComplianceType is musthave", func() {
			By("Creating " + case9CheckNSMusthave + " on managed")
			utils.Kubectl("apply", "-f", case9PolicyYamlCheckNSMusthave, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case9CheckNSMusthave, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case9CheckNSMusthave, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
			utils.Kubectl("delete", "configurationpolicy", case9CheckNSMusthave, "-n", testNamespace)
		})
		It("should return noncompliant if metadataComplianceType is mustonlyhave", func() {
			By("Creating " + case9CheckNSMustonlyhave + " on managed")
			utils.Kubectl("apply", "-f", case9PolicyYamlCheckNSMustonlyhave, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case9CheckNSMustonlyhave, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case9CheckNSMustonlyhave, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("NonCompliant"))
			utils.Kubectl("delete", "configurationpolicy", case9CheckNSMustonlyhave, "-n", testNamespace)
		})
		It("should clean up the created namespace", func() {
			By("Deleting the namespace from " + case9MultiAnnoNSCreate)
			utils.Kubectl("delete", "ns", "case9-test-multi-annotation")
		})
		It("Cleans up", func() {
			policies := []string{
				case9MultiAnnoNSCreate,
				case9CheckNSMusthave,
				case9CheckNSMustonlyhave,
			}

			deleteConfigPolicies(policies)
		})
	})
})
