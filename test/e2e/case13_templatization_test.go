// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package e2e

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/open-cluster-management/config-policy-controller/test/utils"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const case13Secret string = "e2esecret"
const case13SecretCopy string = "e2esecret2"
const case13SecretYaml string = "../resources/case13_templatization/case13_secret.yaml"
const case13CfgPolCreateSecret string = "tmplt-policy-secret-duplicate"
const case13CfgPolCheckSecret string = "tmplt-policy-secret-duplicate-check"
const case13CfgPolCreateSecretYaml string = "../resources/case13_templatization/case13_copysecret.yaml"
const case13CfgPolCheckSecretYaml string = "../resources/case13_templatization/case13_verifysecret.yaml"

const case13ClusterClaim string = "testclaim.open-cluster-management.io"
const case13ClusterClaimYaml string = "../resources/case13_templatization/case13_clusterclaim.yaml"
const case13CfgPolVerifyPod string = "policy-pod-templatized-name-verify"
const case13CfgPolCreatePod string = "policy-pod-templatized-name"
const case13CfgPolCreatePodYaml string = "../resources/case13_templatization/case13_pod_nameFromClusterClaim.yaml"
const case13CfgPolVerifyPodYaml string = "../resources/case13_templatization/case13_pod_name_verify.yaml"
const case13ConfigMap string = "e2e13config"
const case13ConfigMapYaml string = "../resources/case13_templatization/case13_configmap.yaml"
const case13CfgPolVerifyPodWithConfigMap string = "policy-pod-configmap-name"
const case13CfgPolVerifyPodWithConfigMapYaml string = "../resources/case13_templatization/case13_pod_name_verify_configmap.yaml"

const case13LookupSecret string = "tmplt-policy-secret-lookup-check"
const case13LookupSecretYaml string = "../resources/case13_templatization/case13_lookup_secret.yaml"
const case13LookupClusterClaim string = "policy-pod-lookup-verify"
const case13LookupClusterClaimYaml string = "../resources/case13_templatization/case13_lookup_cc.yaml"

const case13Unterminated string = "policy-pod-create-unterminated"
const case13UnterminatedYaml string = "../resources/case13_templatization/case13_unterminated.yaml"
const case13WrongArgs string = "policy-pod-create-wrong-args"
const case13WrongArgsYaml string = "../resources/case13_templatization/case13_wrong_args.yaml"

const case13UpdateRefObject = "policy-update-referenced-object"
const case13UpdateRefObjectYaml = "../resources/case13_templatization/case13_update_referenced_object.yaml"

var _ = Describe("Test templatization", func() {
	Describe("Create a secret and pull data from it into a configurationPolicy", func() {
		It("should be created properly on the managed cluster", func() {
			By("Creating " + case13CfgPolCreateSecret + " and " + case13CfgPolCheckSecret + " on managed")
			//create secret
			utils.Kubectl("apply", "-f", case13SecretYaml, "-n", "default")
			secret := utils.GetWithTimeout(clientManagedDynamic, gvrSecret, case13Secret, "default", true, defaultTimeoutSeconds)
			Expect(secret).NotTo(BeNil())
			//create copy with password from original secret using a templatized policy
			utils.Kubectl("apply", "-f", case13CfgPolCreateSecretYaml, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy, case13CfgPolCreateSecret, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy, case13CfgPolCreateSecret, testNamespace, true, defaultTimeoutSeconds)
				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
			Eventually(func() interface{} {
				copiedSecret := utils.GetWithTimeout(clientManagedDynamic, gvrSecret, case13Secret, "default", true, defaultTimeoutSeconds)
				return utils.GetFieldFromSecret(copiedSecret, "PASSWORD")
			}, defaultTimeoutSeconds, 1).Should(Equal("MWYyZDFlMmU2N2Rm"))
			//check copied secret with a templatized inform policy
			utils.Kubectl("apply", "-f", case13CfgPolCheckSecretYaml, "-n", testNamespace)
			plc = utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy, case13CfgPolCheckSecret, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy, case13CfgPolCheckSecret, testNamespace, true, defaultTimeoutSeconds)
				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
			utils.Kubectl("delete", "configurationpolicy", case13CfgPolCreateSecret, "-n", testNamespace)
			utils.Kubectl("delete", "configurationpolicy", case13CfgPolCheckSecret, "-n", testNamespace)
		})
	})
	Describe("Create a clusterclaim and pull data from it into a configurationPolicy", func() {
		It("should be created properly on the managed cluster", func() {
			By("Creating " + case13CfgPolCreatePod + " and " + case13CfgPolVerifyPod + " on managed")
			//create clusterclaim
			utils.Kubectl("apply", "-f", case13ClusterClaimYaml)
			cc := utils.GetClusterLevelWithTimeout(clientManagedDynamic, gvrClusterClaim, case13ClusterClaim, true, defaultTimeoutSeconds)
			Expect(cc).NotTo(BeNil())
			//create pod named after value from clusterclaim using a templatized policy
			utils.Kubectl("apply", "-f", case13CfgPolCreatePodYaml, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy, case13CfgPolCreatePod, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy, case13CfgPolCreatePod, testNamespace, true, defaultTimeoutSeconds)
				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
			//check copied value with an inform policy
			utils.Kubectl("apply", "-f", case13CfgPolVerifyPodYaml, "-n", testNamespace)
			plc = utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy, case13CfgPolVerifyPod, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy, case13CfgPolVerifyPod, testNamespace, true, defaultTimeoutSeconds)
				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
			//check configmap by creating an inform policy that pulls the pod name from a configmap
			utils.Kubectl("apply", "-f", case13ConfigMapYaml, "-n", "default")
			cm := utils.GetWithTimeout(clientManagedDynamic, gvrConfigMap, case13ConfigMap, "default", true, defaultTimeoutSeconds)
			Expect(cm).NotTo(BeNil())
			utils.Kubectl("apply", "-f", case13CfgPolVerifyPodWithConfigMapYaml, "-n", testNamespace)
			plc = utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy, case13CfgPolVerifyPodWithConfigMap, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy, case13CfgPolVerifyPodWithConfigMap, testNamespace, true, defaultTimeoutSeconds)
				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
			utils.Kubectl("delete", "configurationpolicy", case13CfgPolCreatePod, "-n", testNamespace)
			utils.Kubectl("delete", "configurationpolicy", case13CfgPolVerifyPod, "-n", testNamespace)
		})
	})
	Describe("Use the generic lookup template to get the same resources from the previous tests", func() {
		It("should match the values pulled by resource-specific functions", func() {
			By("Creating inform policies on managed")
			//create inform policy to check secret using generic lookup
			utils.Kubectl("apply", "-f", case13LookupSecretYaml, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy, case13LookupSecret, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy, case13LookupSecret, testNamespace, true, defaultTimeoutSeconds)
				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
			//create inform policy to check clusterclaim using generic lookup
			utils.Kubectl("apply", "-f", case13LookupClusterClaimYaml, "-n", testNamespace)
			plc = utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy, case13LookupClusterClaim, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy, case13LookupClusterClaim, testNamespace, true, defaultTimeoutSeconds)
				return utils.GetStatusMessage(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("pods [testvalue] in namespace default found as specified, therefore this Object template is compliant"))
			utils.Kubectl("delete", "configurationpolicy", case13LookupSecret, "-n", testNamespace)
			utils.Kubectl("delete", "configurationpolicy", case13LookupClusterClaim, "-n", testNamespace)
		})
	})
	Describe("test invalid templates", func() {
		It("should generate noncompliant for invalid template strings", func() {
			By("Creating policies on managed")
			//create policy with unterminated template
			utils.Kubectl("apply", "-f", case13UnterminatedYaml, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy, case13Unterminated, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy, case13Unterminated, testNamespace, true, defaultTimeoutSeconds)
				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("NonCompliant"))
			//create policy with incomplete args in template
			utils.Kubectl("apply", "-f", case13WrongArgsYaml, "-n", testNamespace)
			plc = utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy, case13WrongArgs, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy, case13WrongArgs, testNamespace, true, defaultTimeoutSeconds)
				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("NonCompliant"))
		})
	})
	// Though the Bugzilla bug #2007575 references a different incorrect behavior, it's the same
	// underlying bug and this behavior is easier to test.
	Describe("RHBZ#2007575: Test that the template updates when a referenced resource object is updated", func() {
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
			Expect(err).To(BeNil())
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
			Expect(err).To(BeNil())
			Expect(replConfigMap.Data["message"]).To(Equal("Hello Raleigh!\n"))

			By("Sleeping 30 seconds to ensure PeriodicallyExecConfigPolicies has rerun twice")
			time.Sleep(30 * time.Second)

			By("Updating the referenced ConfigMap")
			configMap.Data["message"] = "Hello world!"
			_, err = clientManaged.CoreV1().ConfigMaps("default").Update(
				context.TODO(), &configMap, v1.UpdateOptions{},
			)
			Expect(err).To(BeNil())

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
		It("Should clean up", func() {
			utils.Kubectl("delete", "configurationpolicy", case13UpdateRefObject, "-n", testNamespace)
			utils.Kubectl("delete", "configmap", configMapName, "-n", "default")
			utils.Kubectl("delete", "configmap", configMapReplName, "-n", "default")
		})
	})
})
