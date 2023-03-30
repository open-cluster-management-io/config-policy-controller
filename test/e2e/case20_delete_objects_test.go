// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package e2e

import (
	"context"
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"open-cluster-management.io/config-policy-controller/test/utils"
)

const (
	case20PodName                   string = "nginx-pod-e2e20"
	case20PodWithFinalizer          string = "nginx-pod-cannot-delete"
	case20ConfigPolicyNameCreate    string = "policy-pod-create"
	case20ConfigPolicyNameEdit      string = "policy-pod-edit"
	case20ConfigPolicyNameExisting  string = "policy-pod-already-created"
	case20ConfigPolicyNameInform    string = "policy-pod-inform"
	case20ConfigPolicyNameFinalizer string = "policy-pod-create-withfinalizer"
	case20ConfigPolicyNameChange    string = "policy-pod-change-remediation"
	case20ConfigPolicyNameMHPDA     string = "policy-pod-mhpda"
	case20PodMHPDAName              string = "nginx-pod-e2e20-mhpda"
	case20PodYaml                   string = "../resources/case20_delete_objects/case20_pod.yaml"
	case20PolicyYamlCreate          string = "../resources/case20_delete_objects/case20_create_pod.yaml"
	case20PolicyYamlEdit            string = "../resources/case20_delete_objects/case20_edit_pod.yaml"
	case20PolicyYamlExisting        string = "../resources/case20_delete_objects/case20_enforce_noncreated_pod.yaml"
	case20PolicyYamlInform          string = "../resources/case20_delete_objects/case20_inform_pod.yaml"
	case20PolicyYamlFinalizer       string = "../resources/case20_delete_objects/case20_createpod_finalizer.yaml"
	case20PolicyYamlChangeInform    string = "../resources/case20_delete_objects/case20_change_inform.yaml"
	case20PolicyYamlChangeEnforce   string = "../resources/case20_delete_objects/case20_change_enforce.yaml"
	case20PolicyYamlMHPDA           string = "../resources/case20_delete_objects/case20_musthave_pod_deleteall.yaml"

	// For the CRD deletion test
	case20ConfigPolicyCRDPath string = "../../deploy/crds/policy.open-cluster-management.io_configurationpolicies.yaml"
)

var _ = Describe("Test status fields being set for object deletion", func() {
	Describe("Create a policy on managed cluster in ns:"+testNamespace, func() {
		It("should update status fields properly for created objects", func() {
			By("Creating " + case20ConfigPolicyNameCreate + " on managed")
			utils.Kubectl("apply", "-f", case20PolicyYamlCreate, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case20ConfigPolicyNameCreate, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case20ConfigPolicyNameCreate, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case20ConfigPolicyNameCreate, testNamespace, true, defaultTimeoutSeconds)
				relatedObj := managedPlc.Object["status"].(map[string]interface{})["relatedObjects"].([]interface{})[0]
				properties := relatedObj.(map[string]interface{})["properties"].(map[string]interface{})

				return properties["createdByPolicy"].(bool)
			}, defaultTimeoutSeconds, 1).Should(Equal(true))
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case20ConfigPolicyNameCreate, testNamespace, true, defaultTimeoutSeconds)
				relatedObj := managedPlc.Object["status"].(map[string]interface{})["relatedObjects"].([]interface{})[0]
				properties := relatedObj.(map[string]interface{})["properties"].(map[string]interface{})

				return properties["uid"].(string)
			}, defaultTimeoutSeconds, 1).Should(Not(Equal("")))
		})
		It("should update status fields properly for non-created objects", func() {
			By("Creating " + case20ConfigPolicyNameExisting + " on managed")
			utils.Kubectl("apply", "-f", case20PolicyYamlExisting, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case20ConfigPolicyNameExisting, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case20ConfigPolicyNameExisting, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case20ConfigPolicyNameExisting, testNamespace, true, defaultTimeoutSeconds)
				relatedObj := managedPlc.Object["status"].(map[string]interface{})["relatedObjects"].([]interface{})[0]
				properties := relatedObj.(map[string]interface{})["properties"].(map[string]interface{})

				return properties["createdByPolicy"].(bool)
			}, defaultTimeoutSeconds, 1).Should(Equal(false))
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case20ConfigPolicyNameExisting, testNamespace, true, defaultTimeoutSeconds)
				relatedObj := managedPlc.Object["status"].(map[string]interface{})["relatedObjects"].([]interface{})[0]
				properties := relatedObj.(map[string]interface{})["properties"].(map[string]interface{})

				return properties["uid"]
			}, defaultTimeoutSeconds, 1).Should(BeNil())
		})
		It("should update status fields properly for edited objects", func() {
			By("Creating " + case20ConfigPolicyNameEdit + " on managed")
			utils.Kubectl("apply", "-f", case20PolicyYamlEdit, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case20ConfigPolicyNameEdit, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case20ConfigPolicyNameEdit, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case20ConfigPolicyNameEdit, testNamespace, true, defaultTimeoutSeconds)
				relatedObj := managedPlc.Object["status"].(map[string]interface{})["relatedObjects"].([]interface{})[0]
				properties := relatedObj.(map[string]interface{})["properties"].(map[string]interface{})

				return properties["createdByPolicy"].(bool)
			}, defaultTimeoutSeconds, 1).Should(Equal(false))
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case20ConfigPolicyNameEdit, testNamespace, true, defaultTimeoutSeconds)
				relatedObj := managedPlc.Object["status"].(map[string]interface{})["relatedObjects"].([]interface{})[0]
				properties := relatedObj.(map[string]interface{})["properties"].(map[string]interface{})

				return properties["uid"]
			}, defaultTimeoutSeconds, 1).Should(BeNil())
		})
		It("should not update status field for inform policies", func() {
			By("Creating " + case20ConfigPolicyNameInform + " on managed")
			utils.Kubectl("apply", "-f", case20PolicyYamlInform, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case20ConfigPolicyNameInform, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case20ConfigPolicyNameInform, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case20ConfigPolicyNameInform, testNamespace, true, defaultTimeoutSeconds)
				relatedObj := managedPlc.Object["status"].(map[string]interface{})["relatedObjects"].([]interface{})[0]
				properties := relatedObj.(map[string]interface{})["properties"]

				return properties
			}, defaultTimeoutSeconds, 1).Should(BeNil())
		})
		It("Cleans up", func() {
			utils.Kubectl("delete", "pod", case20PodName, "-n", "default")
			policies := []string{
				case20ConfigPolicyNameCreate,
				case20ConfigPolicyNameExisting,
				case20ConfigPolicyNameEdit,
				case20ConfigPolicyNameInform,
			}

			deleteConfigPolicies(policies)

			Consistently(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case20ConfigPolicyNameCreate, testNamespace, false, defaultTimeoutSeconds)

				return managedPlc
			}, defaultTimeoutSeconds, 1).Should(BeNil())

			Consistently(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case20ConfigPolicyNameExisting, testNamespace, false, defaultTimeoutSeconds)

				return managedPlc
			}, defaultTimeoutSeconds, 1).Should(BeNil())

			Consistently(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case20ConfigPolicyNameEdit, testNamespace, false, defaultTimeoutSeconds)

				return managedPlc
			}, defaultTimeoutSeconds, 1).Should(BeNil())
		})
	})
})

var _ = Describe("Test objects that should be deleted are actually being deleted", func() {
	Describe("Create a policy on managed cluster in ns:"+testNamespace, func() {
		It("Should create pod", func() {
			// create pod
			By("Creating " + case20PodName + " on default")
			utils.Kubectl("apply", "-f", case20PodYaml)
			Eventually(func() interface{} {
				pod := utils.GetWithTimeout(clientManagedDynamic, gvrPod,
					case20PodName, "default", true, defaultTimeoutSeconds)

				return pod
			}, defaultTimeoutSeconds, 1).Should(Not(BeNil()))
			// check policy
			By("Creating " + case20ConfigPolicyNameInform + " on managed")
			utils.Kubectl("apply", "-f", case20PolicyYamlInform, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case20ConfigPolicyNameInform, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case20ConfigPolicyNameInform, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
		})
		It("should not delete pod", func() {
			deleteConfigPolicies([]string{case20ConfigPolicyNameInform})
			pod := utils.GetWithTimeout(clientManagedDynamic, gvrPod,
				case20PodName, "default", true, defaultTimeoutSeconds)
			Expect(pod).Should(Not(BeNil()))
			Consistently(func() interface{} {
				pod := utils.GetWithTimeout(clientManagedDynamic, gvrPod,
					case20PodName, "default", true, defaultTimeoutSeconds)

				return pod
			}, defaultTimeoutSeconds, 1).Should(Not(BeNil()))
		})
		It("Should create DeleteIfCreated policy", func() {
			// delete pod to reset
			utils.Kubectl("delete", "pod", "nginx-pod-e2e20", "-n", "default")
			// create policy to create pod
			By("Creating " + case20ConfigPolicyNameCreate + " on managed")
			utils.Kubectl("apply", "-f", case20PolicyYamlCreate, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case20ConfigPolicyNameCreate, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case20ConfigPolicyNameCreate, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
			Eventually(func() interface{} {
				pod := utils.GetWithTimeout(clientManagedDynamic, gvrPod,
					case20PodName, "default", true, defaultTimeoutSeconds)

				return pod
			}, defaultTimeoutSeconds, 1).Should(Not(BeNil()))
		})
		It("should delete child object properly", func() {
			// delete policy, should delete pod
			deleteConfigPolicies([]string{case20ConfigPolicyNameCreate})
			Eventually(func() interface{} {
				pod := utils.GetWithTimeout(clientManagedDynamic, gvrPod,
					case20PodName, "default", false, defaultTimeoutSeconds)

				return pod
			}, defaultTimeoutSeconds, 1).Should(BeNil())
		})
		It("should create deleteifcreated policy for non created obj", func() {
			// policy that did not create pod
			By("Creating " + case20PodName + " on default")
			utils.Kubectl("apply", "-f", case20PodYaml)
			Eventually(func() interface{} {
				pod := utils.GetWithTimeout(clientManagedDynamic, gvrPod,
					case20PodName, "default", true, defaultTimeoutSeconds)

				return pod
			}, defaultTimeoutSeconds, 1).Should(Not(BeNil()))

			By("Creating " + case20ConfigPolicyNameEdit + " on managed")
			utils.Kubectl("apply", "-f", case20PolicyYamlEdit, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case20ConfigPolicyNameEdit, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case20ConfigPolicyNameEdit, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
		})
		It("should not delete the child object", func() {
			// delete policy, should delete pod
			deleteConfigPolicies([]string{case20ConfigPolicyNameEdit})
			Consistently(func() interface{} {
				pod := utils.GetWithTimeout(clientManagedDynamic, gvrPod,
					case20PodName, "default", true, defaultTimeoutSeconds)

				return pod
			}, defaultTimeoutSeconds, 1).Should(Not(BeNil()))
		})
		It("should handle deleteAll properly for created obj", func() {
			By("Creating " + case20ConfigPolicyNameExisting + " on managed")
			utils.Kubectl("apply", "-f", case20PolicyYamlExisting, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case20ConfigPolicyNameExisting, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case20ConfigPolicyNameExisting, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
		})
		It("should delete the child object properly", func() {
			// delete policy, should delete pod
			deleteConfigPolicies([]string{case20ConfigPolicyNameExisting})
			Eventually(func() interface{} {
				pod := utils.GetWithTimeout(clientManagedDynamic, gvrPod,
					case20PodName, "default", false, defaultTimeoutSeconds)

				return pod
			}, defaultTimeoutSeconds, 1).Should(BeNil())
			Consistently(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case20ConfigPolicyNameExisting, testNamespace, false, defaultTimeoutSeconds)

				return managedPlc
			}, defaultTimeoutSeconds, 1).Should(BeNil())
		})
		It("should handle deleteAll properly for non created obj", func() {
			By("Creating " + case20PodName + " on default")
			utils.Kubectl("apply", "-f", case20PodYaml)
			Eventually(func() interface{} {
				pod := utils.GetWithTimeout(clientManagedDynamic, gvrPod,
					case20PodName, "default", true, defaultTimeoutSeconds)

				return pod
			}, defaultTimeoutSeconds, 1).Should(Not(BeNil()))
			By("Creating " + case20ConfigPolicyNameExisting + " on managed")
			utils.Kubectl("apply", "-f", case20PolicyYamlExisting, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case20ConfigPolicyNameExisting, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case20ConfigPolicyNameExisting, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
		})
		It("should delete the child object properly", func() {
			// delete policy, should delete pod
			deleteConfigPolicies([]string{case20ConfigPolicyNameExisting})
			Eventually(func() interface{} {
				pod := utils.GetWithTimeout(clientManagedDynamic, gvrPod,
					case20PodName, "default", false, defaultTimeoutSeconds)

				return pod
			}, defaultTimeoutSeconds, 1).Should(BeNil())
		})
		It("Should create pod with finalizer", func() {
			// create policy to create pod
			By("Creating " + case20ConfigPolicyNameFinalizer + " on managed")
			utils.Kubectl("apply", "-f", case20PolicyYamlFinalizer, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case20ConfigPolicyNameFinalizer, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case20ConfigPolicyNameFinalizer, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
			Eventually(func() interface{} {
				pod := utils.GetWithTimeout(clientManagedDynamic, gvrPod,
					case20PodWithFinalizer, "default", true, defaultTimeoutSeconds)

				return pod
			}, defaultTimeoutSeconds, 1).Should(Not(BeNil()))
		})
		It("should hang on unfinished child object delete", func() {
			// delete policy, should delete pod
			err := clientManagedDynamic.Resource(gvrConfigPolicy).Namespace(testNamespace).Delete(
				context.TODO(), case20ConfigPolicyNameFinalizer, metav1.DeleteOptions{},
			)
			Expect(err).To(BeNil())

			Consistently(func() interface{} {
				pod := utils.GetWithTimeout(clientManagedDynamic, gvrPod,
					case20PodWithFinalizer, "default", true, defaultTimeoutSeconds)

				return pod
			}, defaultTimeoutSeconds, 1).Should(Not(BeNil()))
			Consistently(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case20ConfigPolicyNameFinalizer, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Terminating"))
		})
		It("should finish delete when pod finalizer is removed", func() {
			utils.Kubectl(
				"patch",
				"pods/nginx-pod-cannot-delete",
				"--type",
				"json",
				`-p=[{"op":"remove","path":"/metadata/finalizers"}]`,
			)
			Eventually(func() interface{} {
				pod := utils.GetWithTimeout(clientManagedDynamic, gvrPod,
					case20PodWithFinalizer, "default", false, defaultTimeoutSeconds)

				return pod
			}, defaultTimeoutSeconds, 1).Should(BeNil())
			Consistently(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case20ConfigPolicyNameFinalizer, testNamespace, false, defaultTimeoutSeconds)

				return managedPlc
			}, defaultTimeoutSeconds, 1).Should(BeNil())
		})
		It("should handle changing policy from inform to enforce", func() {
			By("Creating " + case20ConfigPolicyNameChange + " on managed")
			utils.Kubectl("apply", "-f", case20PolicyYamlChangeInform, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case20ConfigPolicyNameChange, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case20ConfigPolicyNameChange, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("NonCompliant"))
			By("Patching " + case20ConfigPolicyNameChange + " to enforce")
			utils.Kubectl("apply", "-f", case20PolicyYamlChangeEnforce, "-n", testNamespace)
			plc = utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case20ConfigPolicyNameChange, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case20ConfigPolicyNameChange, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
		})
		It("should delete the child object properly", func() {
			// delete policy, should delete pod
			deleteConfigPolicies([]string{case20ConfigPolicyNameChange})
			Eventually(func() interface{} {
				pod := utils.GetWithTimeout(clientManagedDynamic, gvrPod,
					case20PodName, "default", false, defaultTimeoutSeconds)

				return pod
			}, defaultTimeoutSeconds, 1).Should(BeNil())
			Consistently(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case20ConfigPolicyNameChange, testNamespace, false, defaultTimeoutSeconds)

				return managedPlc
			}, defaultTimeoutSeconds, 1).Should(BeNil())
		})
		It("should handle changing policy from enforce to inform", func() {
			By("Creating " + case20ConfigPolicyNameChange + " on managed")
			utils.Kubectl("apply", "-f", case20PolicyYamlChangeEnforce, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case20ConfigPolicyNameChange, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case20ConfigPolicyNameChange, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
			By("Patching " + case20ConfigPolicyNameChange + " to inform")
			utils.Kubectl("apply", "-f", case20PolicyYamlChangeInform, "-n", testNamespace)
			plc = utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case20ConfigPolicyNameChange, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case20ConfigPolicyNameChange, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
		})
		It("should not delete the child object properly", func() {
			// delete policy, should not delete pod
			deleteConfigPolicies([]string{case20ConfigPolicyNameChange})
			Eventually(func() interface{} {
				pod := utils.GetWithTimeout(clientManagedDynamic, gvrPod,
					case20PodName, "default", true, defaultTimeoutSeconds)

				return pod
			}, defaultTimeoutSeconds, 1).Should(Not(BeNil()))
		})
		It("Cleans up", func() {
			utils.Kubectl("delete", "pod", case20PodName, "-n", "default")
		})
	})
	Describe("Test behavior after manually deleting object", Ordered, func() {
		It("creates a policy to create a pod", func() {
			By("Creating " + case20ConfigPolicyNameCreate + " on managed")
			utils.Kubectl("apply", "-f", case20PolicyYamlCreate, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case20ConfigPolicyNameCreate, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case20ConfigPolicyNameCreate, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
			By("Verifying the pod is present")
			Eventually(func() interface{} {
				pod := utils.GetWithTimeout(clientManagedDynamic, gvrPod,
					case20PodName, "default", true, defaultTimeoutSeconds)

				return pod
			}, defaultTimeoutSeconds, 1).Should(Not(BeNil()))
		})
		It("automatically recreates the pod after it's deleted", func() {
			By("Deleting the pod with kubectl")
			utils.Kubectl("delete", "pod/"+case20PodName, "-n", "default")

			By("Verifying the pod was recreated and isn't still being deleted")
			Eventually(func() interface{} {
				pod := utils.GetWithTimeout(clientManagedDynamic, gvrPod,
					case20PodName, "default", true, defaultTimeoutSeconds)

				_, found, err := unstructured.NestedString(pod.Object, "metadata", "deletionTimestamp")
				if err != nil {
					return err
				}
				if found {
					return errors.New("Pod is being deleted")
				}

				return nil
			}, defaultTimeoutSeconds, 1).Should(BeNil())
		})
		AfterAll(func() {
			By("deletes the pod after the policy is deleted")
			deleteConfigPolicies([]string{case20ConfigPolicyNameCreate})
			Eventually(func() interface{} {
				pod := utils.GetWithTimeout(clientManagedDynamic, gvrPod,
					case20PodName, "default", false, defaultTimeoutSeconds)

				return pod
			}, defaultTimeoutSeconds, 1).Should(BeNil())
		})
	})
})

var _ = Describe("Test objects are not deleted when the CRD is removed", Ordered, func() {
	AfterAll(func() {
		deleteConfigPolicies([]string{case20ConfigPolicyNameMHPDA})
		utils.Kubectl("apply", "-f", case20ConfigPolicyCRDPath)
	})

	It("creates the policy to manage a pod", func() {
		By("Creating " + case20ConfigPolicyNameMHPDA + " on managed")
		utils.Kubectl("apply", "-f", case20PolicyYamlMHPDA, "-n", testNamespace)
		plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
			case20ConfigPolicyNameMHPDA, testNamespace, true, defaultTimeoutSeconds)
		Expect(plc).NotTo(BeNil())
		Eventually(func() interface{} {
			managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case20ConfigPolicyNameMHPDA, testNamespace, true, defaultTimeoutSeconds)

			return utils.GetComplianceState(managedPlc)
		}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
	})

	It("deletes the ConfigurationPolicy CRD and compares the pod UID before and after", func() {
		By("Getting the pod UID")
		oldPodUID := ""
		Eventually(func() interface{} {
			pod := utils.GetWithTimeout(clientManagedDynamic, gvrPod,
				case20PodMHPDAName, "default", true, defaultTimeoutSeconds)

			oldPodUID = string(pod.GetUID())

			return pod
		}, defaultTimeoutSeconds, 1).Should(Not(BeNil()))

		By("Deleting the ConfigurationPolicy CRD")
		utils.Kubectl("delete", "-f", case20ConfigPolicyCRDPath)

		By("Checking that the ConfigurationPolicy is gone")
		Eventually(func(g Gomega) {
			namespace := clientManagedDynamic.Resource(gvrConfigPolicy).Namespace(testNamespace)
			_, err := namespace.Get(context.TODO(), case20ConfigPolicyNameMHPDA, metav1.GetOptions{})
			g.Expect(err).NotTo(BeNil())
			g.Expect(err.Error()).To(ContainSubstring("the server could not find the requested resource"))
		}, defaultTimeoutSeconds, 1).Should(Succeed())

		By("Recreating the CRD")
		utils.Kubectl("apply", "-f", case20ConfigPolicyCRDPath)

		By("Recreating the ConfigurationPolicy")
		utils.Kubectl("apply", "-f", case20PolicyYamlMHPDA, "-n", testNamespace)
		plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
			case20ConfigPolicyNameMHPDA, testNamespace, true, defaultTimeoutSeconds)
		Expect(plc).NotTo(BeNil())
		Eventually(func() interface{} {
			managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case20ConfigPolicyNameMHPDA, testNamespace, true, defaultTimeoutSeconds)

			return utils.GetComplianceState(managedPlc)
		}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))

		By("Checking the pod UID")
		Eventually(func() interface{} {
			pod := utils.GetWithTimeout(clientManagedDynamic, gvrPod,
				case20PodMHPDAName, "default", true, defaultTimeoutSeconds)

			return string(pod.GetUID())
		}, defaultTimeoutSeconds, 1).Should(Equal(oldPodUID))
	})
})
