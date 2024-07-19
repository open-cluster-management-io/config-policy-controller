// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package e2e

import (
	"context"
	"fmt"
	"strconv"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"open-cluster-management.io/config-policy-controller/test/utils"
)

const (
	case13Secret                 string = "e2esecret"
	case13SecretYaml             string = "../resources/case13_templatization/case13_secret.yaml"
	case13CfgPolCreateSecret     string = "tmplt-policy-secret-duplicate"
	case13CfgPolCheckSecret      string = "tmplt-policy-secret-duplicate-check"
	case13CfgPolCopySecretYaml   string = "../resources/case13_templatization/case13_copysecret.yaml"
	case13CfgPolCreateSecretYaml string = "../resources/case13_templatization/case13_fromsecret.yaml"
	case13CfgPolCheckSecretYaml  string = "../resources/case13_templatization/case13_verifysecret.yaml"
)

const (
	case13ClusterClaim        string = "testclaim.open-cluster-management.io"
	case13ClusterClaimYaml    string = "../resources/case13_templatization/case13_clusterclaim.yaml"
	case13CfgPolVerifyPod     string = "policy-pod-templatized-name-verify"
	case13CfgPolCreatePod     string = "policy-pod-templatized-name"
	case13CfgPolCreatePodYaml string = "../resources/case13_templatization/case13_pod_nameFromClusterClaim.yaml"
	case13CfgPolVerifyPodYaml string = "../resources/case13_templatization/case13_pod_name_verify.yaml"
	case13ConfigMap           string = "e2e13config"
	case13ConfigMapYaml       string = "../resources/case13_templatization/case13_configmap.yaml"

	case13CfgPolVerifyPodWithConfigMap string = "policy-pod-configmap-name"
	//nolint:lll
	case13CfgPolVerifyPodWithConfigMapYaml string = "../resources/case13_templatization/case13_pod_name_verify_configmap.yaml"
)

const (
	case13LookupSecret           string = "tmplt-policy-secret-lookup-check"
	case13LookupSecretYaml       string = "../resources/case13_templatization/case13_lookup_secret.yaml"
	case13LookupClusterClaim     string = "policy-pod-lookup-verify"
	case13LookupClusterClaimYaml string = "../resources/case13_templatization/case13_lookup_cc.yaml"
)

const (
	case13Unterminated     string = "policy-pod-create-unterminated"
	case13UnterminatedYaml string = "../resources/case13_templatization/case13_unterminated.yaml"
	case13WrongArgs        string = "case13-policy-pod-create-wrong-args"
	case13WrongArgsYaml    string = "../resources/case13_templatization/case13_wrong_args.yaml"
)

const (
	case13UpdateRefObject     = "policy-update-referenced-object"
	case13UpdateRefObjectYaml = "../resources/case13_templatization/case13_update_referenced_object.yaml"
	case13CopyRefObject       = "policy-copy-referenced-configmap"
	case13CopyRefObjectYaml   = "../resources/case13_templatization/case13_copy_referenced_configmap.yaml"
)

const (
	case13PruneTmpErr     string = "case13-prune-template-error"
	case13PruneTmpErrYaml string = "../resources/case13_templatization/case13_prune_template_error.yaml"
)

var _ = Describe("Test templatization", Ordered, func() {
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
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case13CfgPolCreateSecret, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
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
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case13CfgPolCheckSecret, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))

			By("Clean up")
			utils.Kubectl("delete", "configurationpolicy", case13CfgPolCreateSecret, "-n", testNamespace)
			utils.Kubectl("delete", "configurationpolicy", case13CfgPolCheckSecret, "-n", testNamespace)
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
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case13CfgPolCreateSecret, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
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
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case13CfgPolCheckSecret, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))

			By("Clean up")
			utils.Kubectl("delete", "configurationpolicy", case13CfgPolCreateSecret, "-n", testNamespace)
			utils.Kubectl("delete", "configurationpolicy", case13CfgPolCheckSecret, "-n", testNamespace)
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
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case13CfgPolCreatePod, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
			// check copied value with an inform policy
			utils.Kubectl("apply", "-f", case13CfgPolVerifyPodYaml, "-n", testNamespace)
			plc = utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case13CfgPolVerifyPod, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case13CfgPolVerifyPod, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
			// check configmap by creating an inform policy that pulls the pod name from a configmap
			utils.Kubectl("apply", "-f", case13ConfigMapYaml, "-n", "default")
			cm := utils.GetWithTimeout(clientManagedDynamic, gvrConfigMap,
				case13ConfigMap, "default", true, defaultTimeoutSeconds)
			Expect(cm).NotTo(BeNil())
			utils.Kubectl("apply", "-f", case13CfgPolVerifyPodWithConfigMapYaml, "-n", testNamespace)
			plc = utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case13CfgPolVerifyPodWithConfigMap, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case13CfgPolVerifyPodWithConfigMap, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
		})
		AfterAll(func() {
			utils.Kubectl("delete", "configurationpolicy", case13CfgPolCreatePod,
				"-n", testNamespace, "--ignore-not-found")
			utils.Kubectl("delete", "configurationpolicy", case13CfgPolVerifyPod,
				"-n", testNamespace, "--ignore-not-found")
			utils.Kubectl("delete", "configurationpolicy",
				case13CfgPolVerifyPodWithConfigMap, "-n", testNamespace, "--ignore-not-found")
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
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case13LookupSecret, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
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
			utils.Kubectl("delete", "pod", "c13-pod", "-n", "default")
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
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case13Unterminated, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("NonCompliant"))
			// create policy with incomplete args in template
			utils.Kubectl("apply", "-f", case13WrongArgsYaml, "-n", testNamespace)
			plc = utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case13WrongArgs, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case13WrongArgs, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("NonCompliant"))
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
					ObjectMeta: v1.ObjectMeta{
						Name: configMapName,
					},
					Data: map[string]string{"message": "Hello Raleigh!"},
				}
				_, err := clientManaged.CoreV1().ConfigMaps("default").Create(
					context.TODO(), &configMap, v1.CreateOptions{},
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
					context.TODO(), configMapReplName, v1.GetOptions{},
				)
				Expect(err).ToNot(HaveOccurred())
				Expect(replConfigMap.Data["message"]).To(Equal("Hello Raleigh!\n"))

				By("Checking metric endpoint for policy template counter for policy " + case13UpdateRefObject)
				Eventually(func() interface{} {
					return utils.GetMetrics(
						"config_policy_templates_process_total",
						fmt.Sprintf(`name=\"%s\"`, case13UpdateRefObject),
					)
				}, defaultTimeoutSeconds, 1).Should(Not(BeNil()))
				templatesTotalCounter := utils.GetMetrics(
					"config_policy_templates_process_total",
					fmt.Sprintf(`name=\"%s\"`, case13UpdateRefObject),
				)
				totalCounter, err := strconv.Atoi(templatesTotalCounter[0])
				Expect(err).ToNot(HaveOccurred())
				if err == nil {
					Expect(totalCounter).To(BeNumerically(">", 0))
				}
				By("Policy " + case13UpdateRefObject + " total template process counter : " + templatesTotalCounter[0])

				Eventually(func() interface{} {
					return utils.GetMetrics(
						"config_policy_templates_process_seconds_total",
						fmt.Sprintf(`name=\"%s\"`, case13UpdateRefObject),
					)
				}, defaultTimeoutSeconds, 1).Should(Not(BeNil()))
				templatesTotalSeconds := utils.GetMetrics(
					"config_policy_templates_process_seconds_total",
					fmt.Sprintf(`name=\"%s\"`, case13UpdateRefObject),
				)
				templatesSeconds, err := strconv.ParseFloat(templatesTotalSeconds[0], 32)
				Expect(err).ToNot(HaveOccurred())
				if err == nil {
					Expect(templatesSeconds).To(BeNumerically(">", 0))
				}
				By("Policy " + case13UpdateRefObject + " total template process seconds : " + templatesTotalSeconds[0])

				By("Updating the referenced ConfigMap")
				configMap.Data["message"] = "Hello world!"
				_, err = clientManaged.CoreV1().ConfigMaps("default").Update(
					context.TODO(), &configMap, v1.UpdateOptions{},
				)
				Expect(err).ToNot(HaveOccurred())

				By("Verifying that the replicated ConfigMap has the updated data")
				Eventually(
					func() interface{} {
						replConfigMap, err := clientManaged.CoreV1().ConfigMaps("default").Get(
							context.TODO(), configMapReplName, v1.GetOptions{},
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
				utils.Kubectl("delete", "configmap", configMapName, "-n", "default")
				utils.Kubectl("delete", "configmap", configMapReplName, "-n", "default")
			})
		})
	Describe("Test the copy configMap function", Ordered, func() {
		const configMapName = "configmap-copy-configmap-object"
		const configMapReplName = configMapName + "-repl"
		It("Should have the expected ConfigMap created", func() {
			By("Creating the ConfigMap to reference")
			configMap := corev1.ConfigMap{
				ObjectMeta: v1.ObjectMeta{
					Name: configMapName,
				},
				Data: map[string]string{"message": "Hello Raleigh!"},
			}
			_, err := clientManaged.CoreV1().ConfigMaps("default").Create(
				context.TODO(), &configMap, v1.CreateOptions{},
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
				context.TODO(), configMapReplName, v1.GetOptions{},
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(replConfigMap.Data["message"]).To(Equal("Hello Raleigh!"))
		})
		AfterAll(func() {
			deleteConfigPolicies([]string{case13CopyRefObject})
			utils.Kubectl("delete", "configmap", configMapName, "-n", "default", "--ignore-not-found")
			utils.Kubectl("delete", "configmap", configMapReplName, "-n", "default", "--ignore-not-found")
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
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case13PruneTmpErr, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))

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
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case13PruneTmpErr, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("NonCompliant"))

			By("By verifying that the configmap still exist ")
			Consistently(func() interface{} {
				configmap := utils.GetWithTimeout(clientManagedDynamic, gvrConfigMap,
					case13PruneTmpErr+"-configmap", "default", true, defaultTimeoutSeconds)

				return configmap
			}, defaultConsistentlyDuration, 1).ShouldNot(BeNil())
		})
		AfterAll(func() {
			utils.Kubectl("delete", "configurationpolicy", case13PruneTmpErr,
				"-n", testNamespace, "--ignore-not-found")
			utils.Kubectl("delete", "configmap", case13PruneTmpErr+"-configmap",
				"-n", "default", "--ignore-not-found")
			utils.Kubectl("delete", "secret", case13Secret,
				"-n", "default", "--ignore-not-found")
		})
	})
})
