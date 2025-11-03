// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package e2e

import (
	"fmt"
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"open-cluster-management.io/config-policy-controller/test/utils"
)

var _ = Describe("Test template context variables", func() {
	const (
		rsrcPath string = "../resources/case44_template_vars/"
	)

	Describe("Policy with the ObjectNamespace variables", Ordered, func() {
		const (
			preReqs   = rsrcPath + "case44_objectns_var_prereqs.yaml"
			testLabel = "case44-objectns-var"
		)

		BeforeEach(func() {
			By("Creating the prerequisites")
			utils.KubectlApplyAndLabel(testLabel, "-f", preReqs)
		})

		AfterEach(func() {
			utils.KubectlDelete("configurationpolicy", "-l=e2e-test="+testLabel, "-n", testNamespace, "--wait")
		})

		AfterAll(func() {
			utils.KubectlDelete("-f", preReqs)
		})

		It("Should enforce the labels on the e2e-objectns-variable ConfigMaps", func(ctx SpecContext) {
			const (
				policyName = "case44-objectns-variable"
			)

			By("Applying the " + policyName + " ConfigurationPolicy")
			utils.KubectlApplyAndLabel(testLabel, "-n", testNamespace, "-f", rsrcPath+"case44_objectns_var.yaml")

			By("By verifying that the ConfigurationPolicy is compliant and has the correct related objects")
			Eventually(func(g Gomega) {
				managedPlc := utils.GetWithTimeout(
					clientManagedDynamic, gvrConfigPolicy, policyName, testNamespace, true, defaultTimeoutSeconds,
				)

				utils.CheckComplianceStatus(g, managedPlc, "Compliant")

				relatedObjects, _, _ := unstructured.NestedSlice(managedPlc.Object, "status", "relatedObjects")
				g.Expect(relatedObjects).To(HaveLen(2))

				relatedObject1, ok := relatedObjects[0].(map[string]interface{})
				g.Expect(ok).To(BeTrue(), "Related object is not a map")

				relatedObject1NS, _, _ := unstructured.NestedString(relatedObject1, "object", "metadata", "namespace")
				g.Expect(relatedObject1NS).To(
					Equal("case44-e2e-objectns-variables"), "Related object namespace should match",
				)

				relatedObject2, ok := relatedObjects[1].(map[string]interface{})
				g.Expect(ok).To(BeTrue(), "Related object is not a map")

				relatedObject2NS, _, _ := unstructured.NestedString(relatedObject2, "object", "metadata", "namespace")
				g.Expect(relatedObject2NS).To(Equal("default"), "Related object namespace should match")
			}, defaultTimeoutSeconds, 1).Should(Succeed())

			By("By verifying the ConfigMaps")
			configMap1, err := clientManaged.CoreV1().ConfigMaps("case44-e2e-objectns-variables").Get(
				ctx, "case44-e2e-objectns-variables", metav1.GetOptions{},
			)
			Expect(err).ToNot(HaveOccurred())

			Expect(configMap1.ObjectMeta.Labels).To(HaveKeyWithValue("case44", "passed"))
			Expect(configMap1.ObjectMeta.Labels).To(HaveKeyWithValue("namespace", "case44-e2e-objectns-variables"))

			configMap2, err := clientManaged.CoreV1().ConfigMaps("default").Get(
				ctx, "case44-e2e-objectns-variables", metav1.GetOptions{},
			)
			Expect(err).ToNot(HaveOccurred())

			Expect(configMap2.ObjectMeta.Labels).To(HaveKeyWithValue("case44", "passed"))
			Expect(configMap2.ObjectMeta.Labels).To(HaveKeyWithValue("namespace", "default"))
		})

		It("Should fail when the namespace doesn't match after template resolution", func() {
			const (
				invalidPolicyName = "case44-invalid-ns"
			)

			By("Applying the " + invalidPolicyName + " ConfigurationPolicy")
			utils.KubectlApplyAndLabel(
				testLabel, "-n", testNamespace, "-f", rsrcPath+"case44_objectns_var_invalid_ns.yaml")

			By("By verifying that the ConfigurationPolicy is noncompliant")
			Eventually(func(g Gomega) {
				managedPlc := utils.GetWithTimeout(
					clientManagedDynamic,
					gvrConfigPolicy,
					invalidPolicyName,
					testNamespace,
					true,
					defaultTimeoutSeconds,
				)

				utils.CheckComplianceStatus(g, managedPlc, "NonCompliant")
				g.Expect(utils.GetStatusMessage(managedPlc)).To(Equal(
					"The object definition's namespace must match the result " +
						"from the namespace selector after template resolution",
				))
			}, defaultTimeoutSeconds, 1).Should(Succeed())
		})
	})

	Describe("Policy with the ObjectName variable", Ordered, func() {
		const (
			preReqs     = rsrcPath + "case44_objectname_var_prereqs.yaml"
			e2eBaseName = "case44-e2e-objectname-var"
			testLabel   = "case44-objectname-var"
		)

		BeforeEach(func() {
			By("Creating the prerequisites")
			utils.KubectlApplyAndLabel(testLabel, "-f", preReqs)
		})

		AfterEach(func() {
			utils.KubectlDelete("configurationpolicy", "-l=e2e-test="+testLabel, "-n", testNamespace, "--wait")
			utils.KubectlDelete("configmaps", "-n", e2eBaseName, "-l=e2e-test="+testLabel, "--wait")
		})

		AfterAll(func() {
			utils.KubectlDelete("-f", preReqs)
		})

		It("Should enforce the labels on the case44-e2e-objectname-var* ConfigMaps", func(ctx SpecContext) {
			const (
				policyName = "case44-objectname-var"
			)
			By("Applying the " + policyName + " ConfigurationPolicy")
			utils.KubectlApplyAndLabel(testLabel, "-n", testNamespace, "-f", rsrcPath+"case44_objectname_var.yaml")

			By("By verifying that the ConfigurationPolicy is compliant and has the correct related objects")
			Eventually(func(g Gomega) {
				managedPlc := utils.GetWithTimeout(
					clientManagedDynamic, gvrConfigPolicy, policyName, testNamespace, true, defaultTimeoutSeconds,
				)

				utils.CheckComplianceStatus(g, managedPlc, "Compliant")
				g.Expect(utils.GetStatusMessage(managedPlc)).Should(Equal(fmt.Sprintf(
					"configmaps [case44-e2e-objectname-var2, case44-e2e-objectname-var3] "+
						"found as specified in namespace %[1]s",
					e2eBaseName)))

				relatedObjects, _, _ := unstructured.NestedSlice(managedPlc.Object, "status", "relatedObjects")
				g.Expect(relatedObjects).To(HaveLen(2))

				for idx := range relatedObjects {
					relatedObject, ok := relatedObjects[idx].(map[string]interface{})
					g.Expect(ok).To(BeTrue(), "Related object is not a map")
					relatedObject1NS, _, _ := unstructured.NestedString(relatedObject, "object", "metadata", "name")
					// The first object is skipped.
					g.Expect(relatedObject1NS).To(
						Equal(fmt.Sprintf("%s%d", e2eBaseName, idx+2)),
						"Related object name should match")
				}
			}, defaultTimeoutSeconds, 1).Should(Succeed())

			By("By verifying the ConfigMaps")
			configMaps, err := clientManaged.CoreV1().ConfigMaps(e2eBaseName).List(
				ctx, metav1.ListOptions{
					LabelSelector: "case44",
				},
			)
			Expect(err).ToNot(HaveOccurred())

			for _, cm := range configMaps.Items {
				if cm.Name == "case44-e2e-objectname-var1" {
					continue
				}

				Expect(cm.ObjectMeta.Labels).To(HaveKeyWithValue("case44", "passed"))
				Expect(cm.ObjectMeta.Labels).To(HaveKeyWithValue("name", cm.GetName()))
				Expect(cm.ObjectMeta.Labels).To(HaveKeyWithValue("namespace", cm.GetNamespace()))
			}
		})

		DescribeTable("Should succeed when context vars are in use but name/namespace are left empty",
			func(ctx SpecContext, policyName string, policyYAML string) {
				By("Applying the " + policyName + " ConfigurationPolicy")
				utils.KubectlApplyAndLabel(testLabel, "-n", testNamespace, "-f", rsrcPath+policyYAML)

				By("By verifying that the ConfigurationPolicy is compliant and has the correct related objects")
				Eventually(func(g Gomega) {
					managedPlc := utils.GetWithTimeout(
						clientManagedDynamic,
						gvrConfigPolicy,
						policyName,
						testNamespace,
						true,
						defaultTimeoutSeconds,
					)

					utils.CheckComplianceStatus(g, managedPlc, "Compliant")
					g.Expect(utils.GetStatusMessage(managedPlc)).Should(
						Equal("configmaps [case44-e2e-objectname-var3] found as specified in namespace " + e2eBaseName))

					relatedObjects, _, _ := unstructured.NestedSlice(managedPlc.Object, "status", "relatedObjects")
					g.Expect(relatedObjects).To(HaveLen(1))
					relatedObject, ok := relatedObjects[0].(map[string]interface{})
					g.Expect(ok).To(BeTrue(), "Related object is not a map")
					relatedObject1NS, _, _ := unstructured.NestedString(relatedObject, "object", "metadata", "name")
					g.Expect(relatedObject1NS).To(
						Equal(fmt.Sprintf("%s%d", e2eBaseName, 3)),
						"Related object name should match")
				}, defaultTimeoutSeconds, 1).Should(Succeed())

				By("By verifying the ConfigMaps")
				cm, err := clientManaged.CoreV1().ConfigMaps(e2eBaseName).Get(
					ctx, "case44-e2e-objectname-var3", metav1.GetOptions{})
				Expect(err).ToNot(HaveOccurred())
				Expect(cm.ObjectMeta.Labels).To(HaveKeyWithValue("case44", "passed"))
				Expect(cm.ObjectMeta.Labels).To(HaveKeyWithValue("object-name", cm.GetName()))
				Expect(cm.ObjectMeta.Labels).To(HaveKeyWithValue("object-namespace", cm.GetNamespace()))
			},
			Entry("inside an if conditional", "case44-outside-name", "case44_objectname_var_outside_name.yaml"),
			Entry("with a boolean argument", "case44-outside-name-arg", "case44_objectname_var_outside_name_arg.yaml"),
		)

		It("Should fail when the name doesn't match after template resolution", func() {
			const (
				invalidPolicyName = "case44-invalid-name"
			)

			By("Applying the " + invalidPolicyName + " ConfigurationPolicy")
			utils.KubectlApplyAndLabel(
				testLabel, "-n", testNamespace, "-f", rsrcPath+"case44_objectname_var_invalid_name.yaml")

			By("By verifying that the ConfigurationPolicy is noncompliant")
			Eventually(func(g Gomega) {
				managedPlc := utils.GetWithTimeout(
					clientManagedDynamic,
					gvrConfigPolicy,
					invalidPolicyName,
					testNamespace,
					true,
					defaultTimeoutSeconds,
				)

				utils.CheckComplianceStatus(g, managedPlc, "NonCompliant")
				g.Expect(utils.GetStatusMessage(managedPlc)).To(Equal(
					"The object definition's name must match the result " +
						"from the object selector after template resolution",
				))
			}, defaultTimeoutSeconds, 1).Should(Succeed())
		})

		DescribeTable("Should fail when skipObject is called",
			func(policyName string, policyYAML string, errString string) {
				By("Applying the " + policyName + " ConfigurationPolicy")
				utils.KubectlApplyAndLabel(testLabel, "-n", testNamespace, "-f", rsrcPath+policyYAML)

				By("By verifying that the ConfigurationPolicy is noncompliant")
				Eventually(func(g Gomega) {
					managedPlc := utils.GetWithTimeout(
						clientManagedDynamic,
						gvrConfigPolicy,
						policyName,
						testNamespace,
						true,
						defaultTimeoutSeconds,
					)

					utils.CheckComplianceStatus(g, managedPlc, "NonCompliant")
					g.Expect(utils.GetStatusMessage(managedPlc)).To(HaveSuffix(
						"error calling skipObject: " + errString))
				}, defaultTimeoutSeconds, 1).Should(Succeed())
			},
			Entry("with multiple arguments", "case44-skipobject-multi-arg", "case44_skipobject_multi_arg.yaml",
				`expected one optional boolean argument but received 3 arguments`),
			Entry("with a non-boolean argument", "case44-skipobject-non-bool", "case44_skipobject_non_bool.yaml",
				`expected boolean but received 'not a boolean'`),
		)

		It("Should fail when context variables are used without a selector", func() {
			for name, test := range map[string]struct {
				file, expectedStruct string
			}{
				"case44-no-selector": {
					file:           "case44_objectname_var_noselector.yaml",
					expectedStruct: " ObjectNamespace string ",
				},
				"case44-no-selector-ns": {
					file: "case44_objectname_var_noselector_ns.yaml",
				},
			} {
				By("Applying the " + name + " ConfigurationPolicy")
				utils.KubectlApplyAndLabel(testLabel, "-n", testNamespace, "-f", rsrcPath+test.file)

				By("By verifying that the ConfigurationPolicy is noncompliant")
				Eventually(func(g Gomega) {
					managedPlc := utils.GetWithTimeout(
						clientManagedDynamic,
						gvrConfigPolicy,
						name,
						testNamespace,
						true,
						defaultTimeoutSeconds,
					)

					utils.CheckComplianceStatus(g, managedPlc, "NonCompliant")
					g.Expect(utils.GetStatusMessage(managedPlc)).To(ContainSubstring(
						`template: tmpl:6:14: executing "tmpl" at <.ObjectName>: ` +
							`can't evaluate field ObjectName in type struct {` + test.expectedStruct + `}`,
					))
				}, defaultTimeoutSeconds, 1).Should(Succeed())
			}
		})

		It("Should be compliant when all objects are skipped with skipObject", func() {
			const (
				allSkippedPolicyName = "case44-objectname-var-all-skipped"
			)

			By("Applying the " + allSkippedPolicyName + " ConfigurationPolicy")
			utils.KubectlApplyAndLabel(
				testLabel, "-n", testNamespace, "-f", rsrcPath+"case44_objectname_var_all_skipped.yaml")

			By("By verifying that the ConfigurationPolicy is compliant")
			Eventually(func(g Gomega) {
				managedPlc := utils.GetWithTimeout(
					clientManagedDynamic,
					gvrConfigPolicy,
					allSkippedPolicyName,
					testNamespace,
					true,
					defaultTimeoutSeconds,
				)

				utils.CheckComplianceStatus(g, managedPlc, "Compliant")
				g.Expect(utils.GetStatusMessage(managedPlc)).To(Equal(
					"All objects of kind ConfigMap were skipped by the `skipObject` template function",
				))
			}, defaultTimeoutSeconds, 1).Should(Succeed())
		})

		It("Should be compliant when skipObject is used with a non-existent CRD (ACM-23563)", func() {
			const (
				policyName = "case44-skipobject-missing-crd"
			)

			By("Applying the " + policyName + " ConfigurationPolicy with a non-existent kind")
			utils.KubectlApplyAndLabel(
				testLabel, "-n", testNamespace, "-f", rsrcPath+"case44_skipobject_missing_crd.yaml")

			By("By verifying that the ConfigurationPolicy is compliant without CRD errors")
			Eventually(func(g Gomega) {
				managedPlc := utils.GetWithTimeout(
					clientManagedDynamic,
					gvrConfigPolicy,
					policyName,
					testNamespace,
					true,
					defaultTimeoutSeconds,
				)

				utils.CheckComplianceStatus(g, managedPlc, "Compliant")
				g.Expect(utils.GetStatusMessage(managedPlc)).To(Equal(
					"All objects of kind FakeKind were skipped by the `skipObject` template function",
				))

				// Verify there are no related objects since everything was skipped
				relatedObjects, _, _ := unstructured.NestedSlice(managedPlc.Object, "status", "relatedObjects")
				g.Expect(relatedObjects).To(BeEmpty())
			}, defaultTimeoutSeconds, 1).Should(Succeed())
		})
	})

	Describe("Policy with the Object variable", Ordered, func() {
		const (
			preReqs    = rsrcPath + "case44_object_var_prereqs.yaml"
			baseName   = "case44-object-var"
			policyName = "case44-object-variables"
		)

		BeforeEach(func() {
			By("Creating the prerequisites")
			utils.KubectlApplyAndLabel(baseName, "-f", preReqs)
		})

		AfterEach(func() {
			utils.KubectlDelete("configurationpolicy", "-l=e2e-test="+baseName, "-n", testNamespace, "--wait")
			utils.KubectlDelete("configmaps", "-l=e2e-test="+baseName, "-n", baseName, "--wait")
		})

		AfterAll(func() {
			utils.KubectlDelete("-f", preReqs)
		})

		DescribeTable("Should enforce the labels on the e2e-object-var ConfigMaps",
			func(ctx SpecContext, patch string, objectCount int) {
				By("Applying the " + policyName + " ConfigurationPolicy")
				patchFilepath := rsrcPath + "case44_object_var.yaml"
				if patch != "" {
					patchFilepath = utils.KubectlJSONPatchToFile(patch, "-n", testNamespace, "-f", patchFilepath)
					defer os.Remove(patchFilepath)
				}
				utils.KubectlApplyAndLabel(baseName, "-n", testNamespace, "-f", patchFilepath)

				By("By verifying that the ConfigurationPolicy is noncompliant")
				Eventually(func(g Gomega) {
					managedPlc := utils.GetWithTimeout(
						clientManagedDynamic, gvrConfigPolicy, policyName, testNamespace, true, defaultTimeoutSeconds,
					)

					utils.CheckComplianceStatus(g, managedPlc, "NonCompliant")
				}, defaultTimeoutSeconds, 1).Should(Succeed())

				By("Enforcing the policy")
				utils.EnforceConfigurationPolicy(policyName, testNamespace)

				By("By verifying that the ConfigurationPolicy is compliant and has the correct related objects")
				Eventually(func(g Gomega) {
					managedPlc := utils.GetWithTimeout(
						clientManagedDynamic, gvrConfigPolicy, policyName, testNamespace, true, defaultTimeoutSeconds,
					)

					utils.CheckComplianceStatus(g, managedPlc, "Compliant")

					relatedObjects, _, _ := unstructured.NestedSlice(managedPlc.Object, "status", "relatedObjects")
					g.Expect(relatedObjects).To(HaveLen(objectCount))

					for i := range objectCount {
						relatedObject, ok := relatedObjects[i].(map[string]interface{})
						g.Expect(ok).To(BeTrue(), "Related object is not a map")

						relatedObjName, _, _ := unstructured.NestedString(relatedObject, "object", "metadata", "name")
						g.Expect(relatedObjName).To(
							Equal(fmt.Sprintf("%s%d", baseName, i+1)), "Related object name should match",
						)
					}
				}, defaultTimeoutSeconds, 1).Should(Succeed())

				By("By verifying the ConfigMaps")
				for i := 1; i <= 4; i++ {
					configMap, err := clientManaged.CoreV1().ConfigMaps(baseName).Get(
						ctx, fmt.Sprintf("%s%d", baseName, i), metav1.GetOptions{},
					)
					Expect(err).ToNot(HaveOccurred())

					if i <= objectCount {
						Expect(configMap.ObjectMeta.Labels).To(HaveKeyWithValue("selected", "true"))
						Expect(configMap.ObjectMeta.Labels).To(HaveKeyWithValue("name", configMap.GetName()))
					} else {
						Expect(configMap.ObjectMeta.Labels).NotTo(HaveKeyWithValue("selected", "true"))
						Expect(configMap.ObjectMeta.Labels).NotTo(HaveKeyWithValue("name", configMap.GetName()))
					}
				}
			},
			Entry("with an evaluationInterval", "", 1),
			Entry("with watch enabled",
				`[{ "op": "remove", "path": "/spec/evaluationInterval" }]`, 1),
			Entry("with an evaluationInterval and objectSelector",
				`[{ "op": "remove",
						"path": "/spec/object-templates/0/objectDefinition/metadata/name"}]`, 3),
			Entry("with watch enabled and objectSelector",
				`[{ "op": "remove", "path": "/spec/evaluationInterval" },
					{ "op": "remove",
						"path": "/spec/object-templates/0/objectDefinition/metadata/name"}]`, 3),
		)
	})
})
