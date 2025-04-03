// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package e2e

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"open-cluster-management.io/config-policy-controller/test/utils"
)

var _ = Describe("Test templatization", Ordered, func() {
	const (
		case13RsrcPath               string = "../resources/case13_templatization/"
		case13Secret                 string = "e2esecret"
		case13SecretYaml             string = case13RsrcPath + "case13_secret.yaml"
		case13CfgPolCreateSecret     string = "tmplt-policy-secret-duplicate"
		case13CfgPolCheckSecret      string = "tmplt-policy-secret-duplicate-check"
		case13CfgPolCopySecretYaml   string = case13RsrcPath + "case13_copysecret.yaml"
		case13CfgPolCreateSecretYaml string = case13RsrcPath + "case13_fromsecret.yaml"
		case13CfgPolCheckSecretYaml  string = case13RsrcPath + "case13_verifysecret.yaml"
	)

	const (
		case13ClusterClaim        string = "testclaim.open-cluster-management.io"
		case13ClusterClaimYaml    string = case13RsrcPath + "case13_clusterclaim.yaml"
		case13CfgPolVerifyPod     string = "policy-pod-templatized-name-verify"
		case13CfgPolCreatePod     string = "policy-pod-templatized-name"
		case13CfgPolCreatePodYaml string = case13RsrcPath + "case13_pod_nameFromClusterClaim.yaml"
		case13CfgPolVerifyPodYaml string = case13RsrcPath + "case13_pod_name_verify.yaml"
		case13ConfigMap           string = "e2e13config"
		case13ConfigMapYaml       string = case13RsrcPath + "case13_configmap.yaml"

		case13CfgPolVerifyPodWithConfigMap     string = "policy-pod-configmap-name"
		case13CfgPolVerifyPodWithConfigMapYaml string = case13RsrcPath + "case13_pod_name_verify_configmap.yaml"
	)

	const (
		case13LookupSecret           string = "tmplt-policy-secret-lookup-check"
		case13LookupSecretYaml       string = case13RsrcPath + "case13_lookup_secret.yaml"
		case13LookupClusterClaim     string = "policy-pod-lookup-verify"
		case13LookupClusterClaimYaml string = case13RsrcPath + "case13_lookup_cc.yaml"
	)

	const (
		case13Unterminated     string = "policy-pod-create-unterminated"
		case13UnterminatedYaml string = case13RsrcPath + "case13_unterminated.yaml"
		case13WrongArgs        string = "case13-policy-pod-create-wrong-args"
		case13WrongArgsYaml    string = case13RsrcPath + "case13_wrong_args.yaml"
	)

	const (
		case13UpdateRefObject     = "policy-update-referenced-object"
		case13UpdateRefObjectYaml = case13RsrcPath + "case13_update_referenced_object.yaml"
		case13CopyRefObject       = "policy-copy-referenced-configmap"
		case13CopyRefObjectYaml   = case13RsrcPath + "case13_copy_referenced_configmap.yaml"
	)

	const (
		case13PruneTmpErr     string = "case13-prune-template-error"
		case13PruneTmpErrYaml string = case13RsrcPath + "case13_prune_template_error.yaml"
	)

	AfterAll(func() {
		utils.KubectlDelete("-f", case13ClusterClaimYaml)
	})

	Describe("Create a secret and pull data from it into a configurationPolicy", func() {
		It("should be created properly on the managed cluster", func() {
			By("Creating " + case13CfgPolCreateSecret + " and " + case13CfgPolCheckSecret + " on managed")
			// create secret
			utils.Kubectl("apply", "-f", case13SecretYaml, "-n", "default")
			secret := utils.GetWithTimeout(clientManagedDynamic, gvrSecret,
				case13Secret, "default", true, defaultTimeoutSeconds)
			Expect(secret).NotTo(BeNil())
			// create copy with password from original secret using a templatized policy
			utils.Kubectl("apply", "-f", case13CfgPolCreateSecretYaml, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case13CfgPolCreateSecret, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func(g Gomega) {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case13CfgPolCreateSecret, testNamespace, true, defaultTimeoutSeconds)

				utils.CheckComplianceStatus(g, managedPlc, "Compliant")
			}, defaultTimeoutSeconds, 1).Should(Succeed())
			Eventually(func() interface{} {
				copiedSecret := utils.GetWithTimeout(clientManagedDynamic, gvrSecret,
					case13Secret, "default", true, defaultTimeoutSeconds)

				return utils.GetFieldFromSecret(copiedSecret, "PASSWORD")
			}, defaultTimeoutSeconds, 1).Should(Equal("MWYyZDFlMmU2N2Rm"))
			// check copied secret with a templatized inform policy
			utils.Kubectl("apply", "-f", case13CfgPolCheckSecretYaml, "-n", testNamespace)
			plc = utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case13CfgPolCheckSecret, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func(g Gomega) {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case13CfgPolCheckSecret, testNamespace, true, defaultTimeoutSeconds)

				utils.CheckComplianceStatus(g, managedPlc, "Compliant")
			}, defaultTimeoutSeconds, 1).Should(Succeed())

			By("Clean up")
			utils.KubectlDelete("configurationpolicy", case13CfgPolCreateSecret, "-n", testNamespace)
			utils.KubectlDelete("configurationpolicy", case13CfgPolCheckSecret, "-n", testNamespace)
		})
	})

	Describe("Create a secret and copy all secret data into a configurationPolicy", func() {
		It("should be created properly on the managed cluster", func() {
			By("Creating " + case13CfgPolCreateSecret + " and " + case13CfgPolCheckSecret + " on managed")
			// create secret
			utils.Kubectl("apply", "-f", case13SecretYaml, "-n", "default")
			secret := utils.GetWithTimeout(clientManagedDynamic, gvrSecret,
				case13Secret, "default", true, defaultTimeoutSeconds)
			Expect(secret).NotTo(BeNil())
			// create full data copy from original secret using a templatized policy
			utils.Kubectl("apply", "-f", case13CfgPolCopySecretYaml, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case13CfgPolCreateSecret, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func(g Gomega) {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case13CfgPolCreateSecret, testNamespace, true, defaultTimeoutSeconds)

				utils.CheckComplianceStatus(g, managedPlc, "Compliant")
			}, defaultTimeoutSeconds, 1).Should(Succeed())
			Eventually(func() interface{} {
				copiedSecret := utils.GetWithTimeout(clientManagedDynamic, gvrSecret,
					case13Secret, "default", true, defaultTimeoutSeconds)

				return utils.GetFieldFromSecret(copiedSecret, "PASSWORD")
			}, defaultTimeoutSeconds, 1).Should(Equal("MWYyZDFlMmU2N2Rm"))
			// check copied secret with a templatized inform policy
			utils.Kubectl("apply", "-f", case13CfgPolCheckSecretYaml, "-n", testNamespace)
			plc = utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case13CfgPolCheckSecret, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func(g Gomega) {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case13CfgPolCheckSecret, testNamespace, true, defaultTimeoutSeconds)

				utils.CheckComplianceStatus(g, managedPlc, "Compliant")
			}, defaultTimeoutSeconds, 1).Should(Succeed())

			By("Clean up")
			utils.KubectlDelete("configurationpolicy", case13CfgPolCreateSecret, "-n", testNamespace)
			utils.KubectlDelete("configurationpolicy", case13CfgPolCheckSecret, "-n", testNamespace)
		})
	})

	Describe("Create a clusterclaim and pull data from it into a configurationPolicy", func() {
		It("should be created properly on the managed cluster", func() {
			By("Creating " + case13CfgPolCreatePod + " and " + case13CfgPolVerifyPod + " on managed")
			// create clusterclaim
			utils.Kubectl("apply", "-f", case13ClusterClaimYaml)
			cc := utils.GetClusterLevelWithTimeout(clientManagedDynamic, gvrClusterClaim,
				case13ClusterClaim, true, defaultTimeoutSeconds)
			Expect(cc).NotTo(BeNil())
			// create pod named after value from clusterclaim using a templatized policy
			utils.Kubectl("apply", "-f", case13CfgPolCreatePodYaml, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case13CfgPolCreatePod, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func(g Gomega) {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case13CfgPolCreatePod, testNamespace, true, defaultTimeoutSeconds)

				utils.CheckComplianceStatus(g, managedPlc, "Compliant")
			}, defaultTimeoutSeconds, 1).Should(Succeed())
			// check copied value with an inform policy
			utils.Kubectl("apply", "-f", case13CfgPolVerifyPodYaml, "-n", testNamespace)
			plc = utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case13CfgPolVerifyPod, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func(g Gomega) {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case13CfgPolVerifyPod, testNamespace, true, defaultTimeoutSeconds)

				utils.CheckComplianceStatus(g, managedPlc, "Compliant")
			}, defaultTimeoutSeconds, 1).Should(Succeed())
			// check configmap by creating an inform policy that pulls the pod name from a configmap
			utils.Kubectl("apply", "-f", case13ConfigMapYaml, "-n", "default")
			cm := utils.GetWithTimeout(clientManagedDynamic, gvrConfigMap,
				case13ConfigMap, "default", true, defaultTimeoutSeconds)
			Expect(cm).NotTo(BeNil())
			utils.Kubectl("apply", "-f", case13CfgPolVerifyPodWithConfigMapYaml, "-n", testNamespace)
			plc = utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case13CfgPolVerifyPodWithConfigMap, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func(g Gomega) {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case13CfgPolVerifyPodWithConfigMap, testNamespace, true, defaultTimeoutSeconds)

				utils.CheckComplianceStatus(g, managedPlc, "Compliant")
			}, defaultTimeoutSeconds, 1).Should(Succeed())
		})

		AfterAll(func() {
			utils.KubectlDelete("configurationpolicy", case13CfgPolCreatePod, "-n", testNamespace)
			utils.KubectlDelete("configurationpolicy", case13CfgPolVerifyPod, "-n", testNamespace)
			utils.KubectlDelete("configurationpolicy", case13CfgPolVerifyPodWithConfigMap, "-n", testNamespace)
		})
	})

	Describe("Use the generic lookup template to get the same resources from the previous tests", func() {
		It("should match the values pulled by resource-specific functions", func() {
			By("Creating inform policies on managed")
			// create inform policy to check secret using generic lookup
			utils.Kubectl("apply", "-f", case13LookupSecretYaml, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case13LookupSecret, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func(g Gomega) {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case13LookupSecret, testNamespace, true, defaultTimeoutSeconds)

				utils.CheckComplianceStatus(g, managedPlc, "Compliant")
			}, defaultTimeoutSeconds, 1).Should(Succeed())
			// create inform policy to check clusterclaim using generic lookup
			utils.Kubectl("apply", "-f", case13LookupClusterClaimYaml, "-n", testNamespace)
			plc = utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case13LookupClusterClaim, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case13LookupClusterClaim, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetStatusMessage(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("pods [c13-pod] found as specified in namespace default"))

			By("Clean up")
			deleteConfigPolicies([]string{case13LookupSecret, case13LookupClusterClaim})
			utils.KubectlDelete("pod", "c13-pod", "-n", "default")
		})
	})

	Describe("test invalid templates", Ordered, func() {
		It("should generate noncompliant for invalid template strings", func() {
			By("Creating policies on managed")
			// create policy with unterminated template
			utils.Kubectl("apply", "-f", case13UnterminatedYaml, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case13Unterminated, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func(g Gomega) {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case13Unterminated, testNamespace, true, defaultTimeoutSeconds)

				utils.CheckComplianceStatus(g, managedPlc, "NonCompliant")
			}, defaultTimeoutSeconds, 1).Should(Succeed())
			// create policy with incomplete args in template
			utils.Kubectl("apply", "-f", case13WrongArgsYaml, "-n", testNamespace)
			plc = utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case13WrongArgs, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())

			var managedPlc *unstructured.Unstructured

			Eventually(func(g Gomega) {
				managedPlc = utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case13WrongArgs, testNamespace, true, defaultTimeoutSeconds)

				utils.CheckComplianceStatus(g, managedPlc, "NonCompliant")
			}, defaultTimeoutSeconds, 1).Should(Succeed())

			// Verify that the first object template failing template resolution doesn't prevent the other
			// object templates from being evaluated.
			compliancyDetails, _, _ := unstructured.NestedSlice(managedPlc.Object, "status", "compliancyDetails")
			Expect(compliancyDetails).To(HaveLen(2))

			firstObjTemplate := compliancyDetails[0].(map[string]interface{})
			Expect(firstObjTemplate["Compliant"]).To(Equal("NonCompliant"))

			secondObjTemplate := compliancyDetails[1].(map[string]interface{})
			Expect(secondObjTemplate["Compliant"]).To(Equal("Compliant"))
		})

		AfterAll(func() {
			deleteConfigPolicies([]string{case13Unterminated, case13WrongArgs})
		})
	})

	// Though the Bugzilla bug #2007575 references a different incorrect behavior, it's the same
	// underlying bug and this behavior is easier to test.
	Describe("RHBZ#2007575: Test that the template updates when a referenced resource object is updated",
		Ordered, func() {
			const configMapName = "configmap-update-referenced-object"
			const configMapReplName = configMapName + "-repl"
			It("Should have the expected ConfigMap created", func() {
				By("Creating the ConfigMap to reference")
				configMap := corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name: configMapName,
					},
					Data: map[string]string{"message": "Hello Raleigh!"},
				}
				_, err := clientManaged.CoreV1().ConfigMaps("default").Create(
					context.TODO(), &configMap, metav1.CreateOptions{},
				)
				Expect(err).ToNot(HaveOccurred())
				By("Creating the configuration policy that references the ConfigMap")
				utils.Kubectl("apply", "-f", case13UpdateRefObjectYaml, "-n", testNamespace)

				By("By verifying that the policy is compliant")
				Eventually(
					func() interface{} {
						managedPlc := utils.GetWithTimeout(
							clientManagedDynamic,
							gvrConfigPolicy,
							case13UpdateRefObject,
							testNamespace,
							true,
							defaultTimeoutSeconds,
						)

						return utils.GetComplianceState(managedPlc)
					},
					defaultTimeoutSeconds,
					1,
				).Should(Equal("Compliant"))

				By("By verifying that the replicated ConfigMap has the expected data")
				replConfigMap, err := clientManaged.CoreV1().ConfigMaps("default").Get(
					context.TODO(), configMapReplName, metav1.GetOptions{},
				)
				Expect(err).ToNot(HaveOccurred())
				Expect(replConfigMap.Data["message"]).To(Equal("Hello Raleigh!\n"))

				By("Updating the referenced ConfigMap")
				configMap.Data["message"] = "Hello world!"
				_, err = clientManaged.CoreV1().ConfigMaps("default").Update(
					context.TODO(), &configMap, metav1.UpdateOptions{},
				)
				Expect(err).ToNot(HaveOccurred())

				By("Verifying that the replicated ConfigMap has the updated data")
				Eventually(
					func() interface{} {
						replConfigMap, err := clientManaged.CoreV1().ConfigMaps("default").Get(
							context.TODO(), configMapReplName, metav1.GetOptions{},
						)
						if err != nil {
							return ""
						}

						return replConfigMap.Data["message"]
					},
					defaultTimeoutSeconds,
					1,
				).Should(Equal("Hello world!\n"))
			})

			AfterAll(func() {
				deleteConfigPolicies([]string{case13UpdateRefObject})
				utils.KubectlDelete("configmap", configMapName, "-n", "default")
				utils.KubectlDelete("configmap", configMapReplName, "-n", "default")
			})
		})

	Describe("Test the copy configMap function", Ordered, func() {
		const configMapName = "configmap-copy-configmap-object"
		const configMapReplName = configMapName + "-repl"

		It("Should have the expected ConfigMap created", func() {
			By("Creating the ConfigMap to reference")
			configMap := corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name: configMapName,
				},
				Data: map[string]string{"message": "Hello Raleigh!"},
			}
			_, err := clientManaged.CoreV1().ConfigMaps("default").Create(
				context.TODO(), &configMap, metav1.CreateOptions{},
			)
			Expect(err).ToNot(HaveOccurred())
			By("Creating the configuration policy that references the ConfigMap")
			utils.Kubectl("apply", "-f", case13CopyRefObjectYaml, "-n", testNamespace)

			By("By verifying that the policy is compliant")
			Eventually(
				func() interface{} {
					managedPlc := utils.GetWithTimeout(
						clientManagedDynamic,
						gvrConfigPolicy,
						case13CopyRefObject,
						testNamespace,
						true,
						defaultTimeoutSeconds,
					)

					return utils.GetComplianceState(managedPlc)
				},
				defaultTimeoutSeconds,
				1,
			).Should(Equal("Compliant"))

			By("By verifying that the replicated ConfigMap has the expected data")
			replConfigMap, err := clientManaged.CoreV1().ConfigMaps("default").Get(
				context.TODO(), configMapReplName, metav1.GetOptions{},
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(replConfigMap.Data["message"]).To(Equal("Hello Raleigh!"))
		})

		AfterAll(func() {
			deleteConfigPolicies([]string{case13CopyRefObject})
			utils.KubectlDelete("configmap", configMapName, "-n", "default")
			utils.KubectlDelete("configmap", configMapReplName, "-n", "default")
		})
	})

	Describe("Create a secret and create template error", Ordered, func() {
		It("Should the object created by configpolicy remain", func() {
			By("Creating " + case13CfgPolCreateSecret + " and " + case13CfgPolCheckSecret + " on managed")
			// create secret
			utils.Kubectl("apply", "-f", case13SecretYaml, "-n", "default")
			secret := utils.GetWithTimeout(clientManagedDynamic, gvrSecret,
				case13Secret, "default", true, defaultTimeoutSeconds)
			Expect(secret).NotTo(BeNil())
			// create copy with password from original secret using a templatized policy
			utils.Kubectl("apply", "-f", case13PruneTmpErrYaml, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case13PruneTmpErr, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())

			By("By verifying that the configurationpolicy is working well")
			Eventually(func(g Gomega) {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case13PruneTmpErr, testNamespace, true, defaultTimeoutSeconds)

				utils.CheckComplianceStatus(g, managedPlc, "Compliant")
			}, defaultTimeoutSeconds, 1).Should(Succeed())

			By("By verifying that the configmap exist ")
			Eventually(func() interface{} {
				configmap := utils.GetWithTimeout(clientManagedDynamic, gvrConfigMap,
					case13PruneTmpErr+"-configmap", "default", true, defaultTimeoutSeconds)

				return configmap
			}, defaultTimeoutSeconds, 1).ShouldNot(BeNil())

			By("Patch with invalid managed template")
			utils.Kubectl("patch", "configurationpolicy", case13PruneTmpErr, "--type=json", "-p",
				`[{ "op": "replace", 
			"path": "/spec/object-templates/0/objectDefinition/data/test",
			 "value": '{{ "default" "e2esecret" dddddd vvvvv d }}' }]`,
				"-n", testNamespace)

			By("By verifying that the configurationpolicy is NonCompliant")
			Eventually(func(g Gomega) {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case13PruneTmpErr, testNamespace, true, defaultTimeoutSeconds)

				utils.CheckComplianceStatus(g, managedPlc, "NonCompliant")
			}, defaultTimeoutSeconds, 1).Should(Succeed())

			By("By verifying that the configmap still exist ")
			Consistently(func() interface{} {
				configmap := utils.GetWithTimeout(clientManagedDynamic, gvrConfigMap,
					case13PruneTmpErr+"-configmap", "default", true, defaultTimeoutSeconds)

				return configmap
			}, defaultConsistentlyDuration, 1).ShouldNot(BeNil())
		})

		AfterAll(func() {
			utils.KubectlDelete("configurationpolicy", case13PruneTmpErr, "-n", testNamespace)
			utils.KubectlDelete("configmap", case13PruneTmpErr+"-configmap", "-n", "default")
			utils.KubectlDelete("secret", case13Secret, "-n", "default")
		})
	})

	Describe("Policy with the ObjectNamespace variables", Ordered, func() {
		const (
			preReqs           = case13RsrcPath + "case13_objectns_var_prereqs.yaml"
			policyYAML        = case13RsrcPath + "case13_objectns_var.yaml"
			policyName        = "case13-object-variables"
			invalidPolicyYAML = case13RsrcPath + "case13_objectns_var_invalid_ns.yaml"
			invalidPolicyName = "case13-invalid-ns"
		)

		It("Should enforce the labels on the e2e-object-variable ConfigMaps", func(ctx SpecContext) {
			By("Creating the prerequisites")
			utils.Kubectl("apply", "-f", preReqs)

			By("Applying the " + policyName + " ConfigurationPolicy")
			utils.Kubectl("apply", "-n", testNamespace, "-f", policyYAML)

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
					Equal("case13-e2e-object-variables"), "Related object namespace should match",
				)

				relatedObject2, ok := relatedObjects[1].(map[string]interface{})
				g.Expect(ok).To(BeTrue(), "Related object is not a map")

				relatedObject2NS, _, _ := unstructured.NestedString(relatedObject2, "object", "metadata", "namespace")
				g.Expect(relatedObject2NS).To(Equal("default"), "Related object namespace should match")
			}, defaultTimeoutSeconds, 1).Should(Succeed())

			By("By verifying the ConfigMaps")
			configMap1, err := clientManaged.CoreV1().ConfigMaps("case13-e2e-object-variables").Get(
				ctx, "case13-e2e-object-variables", metav1.GetOptions{},
			)
			Expect(err).ToNot(HaveOccurred())

			Expect(configMap1.ObjectMeta.Labels).To(HaveKeyWithValue("case13", "passed"))
			Expect(configMap1.ObjectMeta.Labels).To(HaveKeyWithValue("namespace", "case13-e2e-object-variables"))

			configMap2, err := clientManaged.CoreV1().ConfigMaps("default").Get(
				ctx, "case13-e2e-object-variables", metav1.GetOptions{},
			)
			Expect(err).ToNot(HaveOccurred())

			Expect(configMap2.ObjectMeta.Labels).To(HaveKeyWithValue("case13", "passed"))
			Expect(configMap2.ObjectMeta.Labels).To(HaveKeyWithValue("namespace", "default"))
		})

		It("Should fail when the namespace doesn't match after template resolution", func() {
			By("Applying the " + policyName + " ConfigurationPolicy")
			utils.Kubectl("apply", "-n", testNamespace, "-f", invalidPolicyYAML)

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

		AfterAll(func() {
			utils.KubectlDelete("configurationpolicy", policyName, "-n", testNamespace)
			utils.KubectlDelete("configurationpolicy", invalidPolicyName, "-n", testNamespace)
			utils.KubectlDelete("-f", preReqs)
		})
	})

	Describe("Policy with the ObjectName variable", Ordered, func() {
		const (
			preReqs                = case13RsrcPath + "case13_objectname_var_prereqs.yaml"
			e2eBaseName            = "case13-e2e-objectname-var"
			policyName             = "case13-objectname-var"
			policyYAML             = case13RsrcPath + "case13_objectname_var.yaml"
			invalidPolicyName      = "case13-invalid-name"
			invalidPolicyYAML      = case13RsrcPath + "case13_objectname_var_invalid_name.yaml"
			noSelectorName         = "case13-no-selector"
			noSelectorPolicyYAML   = case13RsrcPath + "case13_objectname_var_noselector.yaml"
			noSelectorNsName       = "case13-no-selector-ns"
			noSelectorNsPolicyYAML = case13RsrcPath + "case13_objectname_var_noselector_ns.yaml"
			outsidePolicyName      = "case13-outside-name"
			outsidePolicyYAML      = case13RsrcPath + "case13_objectname_var_outside_name.yaml"
			outsideArgPolicyName   = "case13-outside-name-arg"
			outsideArgPolicyYAML   = case13RsrcPath + "case13_objectname_var_outside_name_arg.yaml"
			allSkippedPolicyName   = "case13-objectname-var-all-skipped"
			allSkippedPolicyYAML   = case13RsrcPath + "case13_objectname_var_all_skipped.yaml"
			invalidSkipObjectName  = "case13-objectname-invalid-skipobject"
			invalidSkipObjectYAML  = case13RsrcPath + "case13_objectname_var_invalid_skipobject.yaml"
		)

		BeforeEach(func() {
			By("Creating the prerequisites")
			utils.Kubectl("apply", "-f", preReqs)
		})

		It("Should enforce the labels on the case13-e2e-objectname-var* ConfigMaps", func(ctx SpecContext) {
			By("Applying the " + policyName + " ConfigurationPolicy")
			utils.Kubectl("apply", "-n", testNamespace, "-f", policyYAML)

			By("By verifying that the ConfigurationPolicy is compliant and has the correct related objects")
			Eventually(func(g Gomega) {
				managedPlc := utils.GetWithTimeout(
					clientManagedDynamic, gvrConfigPolicy, policyName, testNamespace, true, defaultTimeoutSeconds,
				)

				utils.CheckComplianceStatus(g, managedPlc, "Compliant")
				g.Expect(utils.GetStatusMessage(managedPlc)).Should(Equal(fmt.Sprintf(
					"configmaps [case13-e2e-objectname-var2, case13-e2e-objectname-var3] "+
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
					LabelSelector: "case13",
				},
			)
			Expect(err).ToNot(HaveOccurred())

			for _, cm := range configMaps.Items {
				if cm.Name == "case13-e2e-objectname-var1" {
					continue
				}

				Expect(cm.ObjectMeta.Labels).To(HaveKeyWithValue("case13", "passed"))
				Expect(cm.ObjectMeta.Labels).To(HaveKeyWithValue("name", cm.GetName()))
				Expect(cm.ObjectMeta.Labels).To(HaveKeyWithValue("namespace", cm.GetNamespace()))
			}
		})

		DescribeTable("Should succeed when context vars are in use but name/namespace are left empty",
			func(ctx SpecContext, policyName string, policyYAML string) {
				By("Applying the " + policyName + " ConfigurationPolicy")
				utils.Kubectl("apply", "-n", testNamespace, "-f", policyYAML)

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
						Equal("configmaps [case13-e2e-objectname-var3] found as specified in namespace " + e2eBaseName))

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
					ctx, "case13-e2e-objectname-var3", metav1.GetOptions{})
				Expect(err).ToNot(HaveOccurred())
				Expect(cm.ObjectMeta.Labels).To(HaveKeyWithValue("case13", "passed"))
				Expect(cm.ObjectMeta.Labels).To(HaveKeyWithValue("object-name", cm.GetName()))
				Expect(cm.ObjectMeta.Labels).To(HaveKeyWithValue("object-namespace", cm.GetNamespace()))
			},
			Entry("inside an if conditional", outsidePolicyName, outsidePolicyYAML),
			Entry("with a boolean argument", outsideArgPolicyName, outsideArgPolicyYAML),
		)

		It("Should fail when the name doesn't match after template resolution", func() {
			By("Applying the " + invalidPolicyName + " ConfigurationPolicy")
			utils.Kubectl("apply", "-n", testNamespace, "-f", invalidPolicyYAML)

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

		It("Should fail when skipObject is called with multiple arguments", func() {
			By("Applying the " + invalidSkipObjectName + " ConfigurationPolicy")
			utils.Kubectl("apply", "-n", testNamespace, "-f", invalidSkipObjectYAML)

			By("By verifying that the ConfigurationPolicy is noncompliant")
			Eventually(func(g Gomega) {
				managedPlc := utils.GetWithTimeout(
					clientManagedDynamic,
					gvrConfigPolicy,
					invalidSkipObjectName,
					testNamespace,
					true,
					defaultTimeoutSeconds,
				)

				utils.CheckComplianceStatus(g, managedPlc, "NonCompliant")
				g.Expect(utils.GetStatusMessage(managedPlc)).To(ContainSubstring(
					`template: tmpl:8:12: executing "tmpl" at <skipObject true false true>: ` +
						`error calling skipObject: ` +
						`skipObject only accepts one optional boolean argument but received 3`,
				))
			}, defaultTimeoutSeconds, 1).Should(Succeed())
		})

		It("Should fail when context variables are used without a selector", func() {
			for name, test := range map[string]struct {
				file, expectedStruct string
			}{
				noSelectorName:   {file: noSelectorPolicyYAML, expectedStruct: " ObjectNamespace string "},
				noSelectorNsName: {file: noSelectorNsPolicyYAML},
			} {
				By("Applying the " + name + " ConfigurationPolicy")
				utils.Kubectl("apply", "-n", testNamespace, "-f", test.file)

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
			By("Applying the " + allSkippedPolicyName + " ConfigurationPolicy")
			utils.Kubectl("apply", "-n", testNamespace, "-f", allSkippedPolicyYAML)

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

		AfterEach(func() {
			utils.KubectlDelete("configurationpolicy", policyName, "-n", testNamespace)
			utils.KubectlDelete("configurationpolicy", invalidPolicyName, "-n", testNamespace)
			utils.KubectlDelete("configurationpolicy", noSelectorName, "-n", testNamespace)
			utils.KubectlDelete("configurationpolicy", noSelectorNsName, "-n", testNamespace)
			utils.KubectlDelete("configurationpolicy", outsidePolicyName, "-n", testNamespace)
			utils.KubectlDelete("configurationpolicy", outsideArgPolicyName, "-n", testNamespace)
			utils.KubectlDelete("configurationpolicy", allSkippedPolicyName, "-n", testNamespace)
			utils.KubectlDelete("configurationpolicy", invalidSkipObjectName, "-n", testNamespace)
			utils.KubectlDelete("configmaps", "-n", e2eBaseName, "--all")
		})

		AfterAll(func() {
			utils.KubectlDelete("-f", preReqs)
		})
	})
})
