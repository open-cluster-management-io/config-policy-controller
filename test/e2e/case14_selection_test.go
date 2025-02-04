// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package e2e

import (
	"context"

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
			utils.KubectlDelete("-f", policy.yamlFile, "-n", testNamespace)
		}
		for _, ns := range testNamespaces {
			By("Deleting " + case14LimitRangeName + " on " + ns)
			utils.KubectlDelete("-f", case14LimitRangeFile, "-n", ns)
		}
		for _, ns := range append(testNamespaces, newNs) {
			By("Deleting namespace " + ns)
			utils.KubectlDelete("namespace", ns)
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
			Eventually(func(g Gomega) {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					policy.name, testNamespace, true, defaultTimeoutSeconds)

				utils.CheckComplianceStatus(g, managedPlc, "NonCompliant")
			}, defaultTimeoutSeconds, 1).Should(Succeed())
		}
	})

	It("should stay noncompliant when limitrange is in one matching namespace", func() {
		By("Creating limitrange " + case14LimitRangeName + " on range1")
		utils.Kubectl("apply", "-f", case14LimitRangeFile, "-n", "range1")
		for _, policy := range policyTests {
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				policy.name, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func(g Gomega) {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					policy.name, testNamespace, true, defaultTimeoutSeconds)

				utils.CheckComplianceStatus(g, managedPlc, "NonCompliant")
			}, defaultTimeoutSeconds, 1).Should(Succeed())
			Consistently(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					policy.name, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultConsistentlyDuration, 1).Should(Equal("NonCompliant"))
		}
	})

	It("should be compliant with limitrange in all matching namespaces", func() {
		By("Creating " + case14LimitRangeName + " on range2")
		utils.Kubectl("apply", "-f", case14LimitRangeFile, "-n", "range2")
		for _, policy := range policyTests {
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				policy.name, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func(g Gomega) {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					policy.name, testNamespace, true, defaultTimeoutSeconds)

				utils.CheckComplianceStatus(g, managedPlc, "Compliant")
			}, defaultTimeoutSeconds, 1).Should(Succeed())
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
			Eventually(func(g Gomega) {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					policy.name, testNamespace, true, defaultTimeoutSeconds)

				utils.CheckComplianceStatus(g, managedPlc, "NonCompliant")
			}, defaultTimeoutSeconds, 1).Should(Succeed())
			Consistently(func() interface{} {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					policy.name, testNamespace, true, defaultTimeoutSeconds)

				return utils.GetComplianceState(managedPlc)
			}, defaultConsistentlyDuration, 1).Should(Equal("NonCompliant"))
			By("Checking that " + policy.name + " has the correct relatedObjects")
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				policy.name, testNamespace, true, defaultTimeoutSeconds)
			// If an object name is specified in the policy, related objects match those in the template.
			// If an object name is not specified in the policy, related objects match those in the
			//   cluster as this is not enforceable.
			// When hasObjName = false
			// compliant: NonCompliant
			//  metadata:
			// 	  name: '-'
			//    namespace: range3
			// reason: Resource not found but should exist
			// is attached for range3.
			Expect(checkRelated(plc)).Should(HaveLen(len(testNamespaces) + 1))
		}
	})

	It("should update relatedObjects after enforcing the policy", func() {
		for _, policy := range policyTests {
			By("Patching " + policy.name + " to enforce")
			utils.EnforceConfigurationPolicy(policy.name, testNamespace)
			By("Checking that " + policy.name + " is Compliant")
			Eventually(func(g Gomega) {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					policy.name, testNamespace, true, defaultTimeoutSeconds)

				utils.CheckComplianceStatus(g, managedPlc, "Compliant")
			}, defaultTimeoutSeconds, 1).Should(Succeed())
			By("Checking that " + policy.name + " has the correct relatedObjects")
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				policy.name, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Expect(checkRelated(plc)).Should(HaveLen(len(testNamespaces) + 1))
		}
	})

	It("should update relatedObjects after updating the namespaceSelector to fewer namespaces", func() {
		for _, policy := range policyTests {
			By("Patching the " + policy.name + " namespaceSelector to reduce the namespaces")
			utils.Kubectl("patch", "configurationpolicy", policy.name, `--type=json`,
				`-p=[{"op":"replace","path":"/spec/namespaceSelector/include","value":["range[2-3]"]}]`,
				"-n", testNamespace)
			By("Checking that " + policy.name + " is Compliant")
			Eventually(func(g Gomega) {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					policy.name, testNamespace, true, defaultTimeoutSeconds)

				utils.CheckComplianceStatus(g, managedPlc, "Compliant")
			}, defaultTimeoutSeconds, 1).Should(Succeed())
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
			Eventually(func(g Gomega) {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					policy.name, testNamespace, true, defaultTimeoutSeconds)

				utils.CheckComplianceStatus(g, managedPlc, "Compliant")
			}, defaultTimeoutSeconds, 1).Should(Succeed())
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
		Expect(namespaces.Delete(context.TODO(), newNs, metav1.DeleteOptions{})).To(Succeed())
		Eventually(func() bool {
			_, err := namespaces.Get(context.TODO(), newNs, metav1.GetOptions{})

			return errors.IsNotFound(err)
		}, defaultTimeoutSeconds, 1).Should(BeTrue())
		for _, policy := range policyTests {
			By("Checking that " + policy.name + " is Compliant")
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				policy.name, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).NotTo(BeNil())
			Eventually(func(g Gomega) {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					policy.name, testNamespace, true, defaultTimeoutSeconds)

				utils.CheckComplianceStatus(g, managedPlc, "Compliant")
			}, defaultTimeoutSeconds, 1).Should(Succeed())
			By("Checking that " + policy.name + " has the correct relatedObjects")
			Eventually(func() int {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					policy.name, testNamespace, true, defaultTimeoutSeconds)

				return len(checkRelated(managedPlc))
			}, defaultTimeoutSeconds, 1).Should(Equal(len(testNamespaces)))
		}
	})
})
