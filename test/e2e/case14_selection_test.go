// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package e2e

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"open-cluster-management.io/config-policy-controller/test/utils"
)

const (
	case14LimitRangeFile string = "../resources/case14_namespaces/case14_limitrange.yaml"
	case14LimitRangeName string = "container-mem-limit-range"
)

var _ = Describe("Test policy compliance with namespace selection", Ordered, func() {
	checkRelated := func(policy *unstructured.Unstructured) []interface{} {
		related, _, err := unstructured.NestedSlice(policy.Object, "status", "relatedObjects")
		if err != nil {
			panic(err)
		}

		return related
	}

	testNamespaces := []string{"range1", "range2"}
	newNs := "range3"
	policyTests := []struct {
		name       string
		yamlFile   string
		hasObjName bool
	}{
		{
			"policy-named-limitrange",
			"../resources/case14_namespaces/case14_limitrange_named.yaml",
			true,
		},
		{
			"policy-unnamed-limitrange",
			"../resources/case14_namespaces/case14_limitrange_unnamed.yaml",
			false,
		},
	}

	BeforeAll(func() {
		By("Create Namespaces if needed")
		namespaces := clientManaged.CoreV1().Namespaces()
		for _, ns := range testNamespaces {
			if _, err := namespaces.Get(context.TODO(), ns, metav1.GetOptions{}); err != nil && errors.IsNotFound(err) {
				Expect(namespaces.Create(context.TODO(), &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: ns,
					},
				}, metav1.CreateOptions{})).NotTo(BeNil())
			}
			Expect(namespaces.Get(context.TODO(), ns, metav1.GetOptions{})).NotTo(BeNil())
		}
	})

	AfterAll(func() {
		for _, policy := range policyTests {
			By("Deleting " + policy.name + " on managed")
			utils.Kubectl("delete", "-f", policy.yamlFile, "-n", testNamespace)
		}
		for _, ns := range testNamespaces {
			By("Deleting " + case14LimitRangeName + " on " + ns)
			utils.Kubectl("delete", "-f", case14LimitRangeFile, "-n", ns)
		}
		for _, ns := range append(testNamespaces, newNs) {
			By("Deleting namespace " + ns)
			utils.Kubectl("delete", "namespace", ns, "--ignore-not-found")
		}
	})

	It("should create the policy on managed cluster in ns "+testNamespace, func() {
		for _, policy := range policyTests {
			By("Creating " + policy.name + " on managed")
			utils.Kubectl("apply", "-f", policy.yamlFile, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				policy.name, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			By("Checking that " + policy.name + " is NonCompliant")
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					policy.name, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("NonCompliant"))
		}
	})

	It("should stay noncompliant when limitrange is in one matching namespace", func() {
		By("Creating limitrange " + case14LimitRangeName + " on range1")
		utils.Kubectl("apply", "-f", case14LimitRangeFile, "-n", "range1")
		for _, policy := range policyTests {
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				policy.name, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					policy.name, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("NonCompliant"))
			Consistently(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					policy.name, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, time.Second*20, 1).Should(Equal("NonCompliant"))
		}
	})

	It("should be compliant with limitrange in all matching namespaces", func() {
		By("Creating " + case14LimitRangeName + " on range2")
		utils.Kubectl("apply", "-f", case14LimitRangeFile, "-n", "range2")
		for _, policy := range policyTests {
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				policy.name, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					policy.name, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
		}
	})

	It("should be noncompliant after adding new matching namespace", func() {
		By("Creating namespace " + newNs)
		namespaces := clientManaged.CoreV1().Namespaces()
		Expect(namespaces.Create(context.TODO(), &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: newNs,
			},
		}, metav1.CreateOptions{})).NotTo(BeNil())
		Expect(namespaces.Get(context.TODO(), newNs, metav1.GetOptions{})).NotTo(BeNil())
		for _, policy := range policyTests {
			By("Checking that " + policy.name + " is NonCompliant")
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					policy.name, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("NonCompliant"))
			Consistently(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					policy.name, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, time.Second*20, 1).Should(Equal("NonCompliant"))
			By("Checking that " + policy.name + " has the correct relatedObjects")
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				policy.name, testNamespace, true, defaultTimeoutSeconds)
			// If an object name is specified in the policy, related objects match those in the template.
			// If an object name is not specified in the policy, related objects match those in the
			//   cluster as this is not enforceable.
			if policy.hasObjName {
				Expect(len(checkRelated(plc))).Should(Equal(len(testNamespaces) + 1))
			} else {
				Expect(len(checkRelated(plc))).Should(Equal(len(testNamespaces)))
			}
		}
	})

	It("should update relatedObjects after enforcing the policy", func() {
		for _, policy := range policyTests {
			By("Patching " + policy.name + " to enforce")
			utils.Kubectl("patch", "configurationpolicy", policy.name, `--type=json`,
				`-p=[{"op":"replace","path":"/spec/remediationAction","value":"enforce"}]`, "-n", testNamespace)
			By("Checking that " + policy.name + " is Compliant")
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					policy.name, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
			By("Checking that " + policy.name + " has the correct relatedObjects")
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				policy.name, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Expect(len(checkRelated(plc))).Should(Equal(len(testNamespaces) + 1))
		}
	})

	It("should update relatedObjects after updating the namespaceSelector to fewer namespaces", func() {
		for _, policy := range policyTests {
			By("Patching the " + policy.name + " namespaceSelector to reduce the namespaces")
			utils.Kubectl("patch", "configurationpolicy", policy.name, `--type=json`,
				`-p=[{"op":"replace","path":"/spec/namespaceSelector/include","value":["range[2-3]"]}]`,
				"-n", testNamespace)
			By("Checking that " + policy.name + " is Compliant")
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					policy.name, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
			By("Checking that " + policy.name + " has the correct relatedObjects")
			Eventually(func() int {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					policy.name, testNamespace, true, defaultTimeoutSeconds)

				return len(checkRelated(managedPlc))
			}, defaultTimeoutSeconds, 1).Should(Equal(len(testNamespaces)))
		}
	})

	It("should update relatedObjects after restoring the namespaceSelector", func() {
		for _, policy := range policyTests {
			By("Restoring the " + policy.name + " namespaceSelector")
			utils.Kubectl("apply", "-f", policy.yamlFile, "-n", testNamespace)
			By("Checking that " + policy.name + " is Compliant")
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					policy.name, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
			By("Checking that " + policy.name + " has the correct relatedObjects")
			Eventually(func() int {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					policy.name, testNamespace, true, defaultTimeoutSeconds)

				return len(checkRelated(managedPlc))
			}, defaultTimeoutSeconds, 1).Should(Equal(len(testNamespaces) + 1))
		}
	})

	It("should update relatedObjects after deleting a namespace", func() {
		By("Deleting namespace " + newNs)
		namespaces := clientManaged.CoreV1().Namespaces()
		Expect(namespaces.Delete(context.TODO(), newNs, metav1.DeleteOptions{})).To(BeNil())
		Eventually(func() bool {
			_, err := namespaces.Get(context.TODO(), newNs, metav1.GetOptions{})

			return errors.IsNotFound(err)
		}, defaultTimeoutSeconds, 1).Should(BeTrue())
		for _, policy := range policyTests {
			By("Checking that " + policy.name + " is Compliant")
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				policy.name, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					policy.name, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))
			By("Checking that " + policy.name + " has the correct relatedObjects")
			Eventually(func() int {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					policy.name, testNamespace, true, defaultTimeoutSeconds)

				return len(checkRelated(managedPlc))
			}, defaultTimeoutSeconds, 1).Should(Equal(len(testNamespaces)))
		}
	})
})
