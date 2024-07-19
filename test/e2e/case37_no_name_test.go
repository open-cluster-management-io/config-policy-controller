package e2e

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"open-cluster-management.io/config-policy-controller/test/utils"
)

var _ = Describe("Test a namespace-scope policy that is missing name", Ordered, func() {
	const (
		case37GoodIngressPath       string = "../resources/case37_no_name/case37_good_ingress.yaml"
		case37BadIngressPath        string = "../resources/case37_no_name/case37_bad_ingresses.yaml"
		case37PolicyNSPath          string = "../resources/case37_no_name/case37_no_name_policy.yaml"
		case37PolicyNSName          string = "case37-test-policy-1"
		case37PolicyNotHavePath     string = "../resources/case37_no_name/case37_no_name_policy_nothave.yaml"
		case37PolicyMustnothaveName string = "case37-test-policy-mustnothave"
	)
	Describe("Test a musthave", Ordered, func() {
		BeforeAll(func() {
			By("Apply bad ingresses")
			utils.Kubectl("apply", "-f", case37BadIngressPath)

			By("Creating a policy on managed")
			utils.Kubectl("apply", "-f", case37PolicyNSPath, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case37PolicyNSName, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).ShouldNot(BeNil())
		})
		It("should have 1 NonCompliant relatedObject under the policy's status", func() {
			var managedPlc *unstructured.Unstructured

			Eventually(func(g Gomega) {
				managedPlc = utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case37PolicyNSName, testNamespace, true, defaultTimeoutSeconds)

				utils.CheckComplianceStatus(g, managedPlc, "NonCompliant")
			}, defaultTimeoutSeconds, 1).Should(Succeed())

			var relatedObjects []interface{}

			By("relatedObjects should exist")
			Eventually(func(g Gomega) interface{} {
				managedPlc = utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case37PolicyNSName, testNamespace, true, defaultTimeoutSeconds)

				var err error

				relatedObjects, _, err = unstructured.NestedSlice(managedPlc.Object, "status", "relatedObjects")

				g.Expect(err).ShouldNot(HaveOccurred())
				g.Expect(relatedObjects).ShouldNot(BeNil())

				return relatedObjects
				// Getting relatedObjects takes longer time
			}, defaultTimeoutSeconds*2, 1).Should(HaveLen(1))

			By("Check the kind of related object")
			kind := relatedObjects[0].(map[string]interface{})["object"].(map[string]interface{})["kind"].(string)
			Expect(kind).Should(Equal("Ingress"))

			By("Check the reason of related object")
			relatedObjectsOne := relatedObjects[0].(map[string]interface{})
			Expect(relatedObjectsOne["reason"].(string)).Should(Equal("Resource found but does not match"))

			By("Check the name of related object is -")
			name, _, err := unstructured.NestedString(relatedObjects[0].(map[string]interface{}),
				"object", "metadata", "name")
			Expect(err).ShouldNot(HaveOccurred())
			Expect(name).Should(Equal("-"))
		})
		It("should have 1 Compliant relatedObject under the policy's status", func() {
			By("Apply good ingress")
			utils.Kubectl("apply", "-f", case37GoodIngressPath)

			var managedPlc *unstructured.Unstructured

			Eventually(func(g Gomega) {
				managedPlc = utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case37PolicyNSName, testNamespace, true, defaultTimeoutSeconds)

				utils.CheckComplianceStatus(g, managedPlc, "Compliant")
			}, defaultTimeoutSeconds, 1).Should(Succeed())

			var relatedObjects []interface{}

			By("relatedObjects should exist")
			Eventually(func(g Gomega) interface{} {
				managedPlc = utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case37PolicyNSName, testNamespace, true, defaultTimeoutSeconds)

				var err error

				relatedObjects, _, err = unstructured.NestedSlice(managedPlc.Object, "status", "relatedObjects")

				g.Expect(err).ShouldNot(HaveOccurred())
				g.Expect(relatedObjects).ShouldNot(BeNil())

				return relatedObjects
			}, defaultTimeoutSeconds*2, 1).Should(HaveLen(1))

			//  NonCompliant relatedObjects Should be deleted and only the Compliant relatedObject remain
			By("Check the kind of related object")
			kind := relatedObjects[0].(map[string]interface{})["object"].(map[string]interface{})["kind"].(string)
			Expect(kind).Should(Equal("Ingress"))

			By("Check the reason of related objects")
			reason := relatedObjects[0].(map[string]interface{})["reason"].(string)
			Expect(reason).Should(Equal("Resource found as expected"))

			By("Check the name of related object is not -")
			name, _, err := unstructured.NestedString(relatedObjects[0].(map[string]interface{}),
				"object", "metadata", "name")
			Expect(err).ShouldNot(HaveOccurred())
			Expect(name).ShouldNot(Equal("-"))
		})
		AfterAll(func() {
			utils.KubectlDelete("-f", case37PolicyNSPath, "-n", testNamespace)
			utils.KubectlDelete("-f", case37GoodIngressPath)
			utils.KubectlDelete("-f", case37BadIngressPath)
			utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case37PolicyNSName, testNamespace, false, defaultTimeoutSeconds)
		})
	})
	Describe("Test a mustnothave", Ordered, func() {
		BeforeAll(func() {
			By("Creating a policy on managed")
			utils.Kubectl("apply", "-f", case37PolicyNotHavePath, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case37PolicyMustnothaveName, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).ShouldNot(BeNil())
		})
		It("should have 1 Compliant relatedObject under the policy's status", func() {
			var managedPlc *unstructured.Unstructured

			Eventually(func(g Gomega) {
				managedPlc = utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case37PolicyMustnothaveName, testNamespace, true, defaultTimeoutSeconds)

				utils.CheckComplianceStatus(g, managedPlc, "Compliant")
			}, defaultTimeoutSeconds, 1).Should(Succeed())

			By("relatedObjects should exist")
			var relatedObjects []interface{}
			Eventually(func(g Gomega) interface{} {
				managedPlc = utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case37PolicyMustnothaveName, testNamespace, true, defaultTimeoutSeconds)

				var err error
				relatedObjects, _, err = unstructured.NestedSlice(managedPlc.Object, "status", "relatedObjects")
				g.Expect(err).ShouldNot(HaveOccurred())

				return relatedObjects
				// Getting relatedObjects takes longer time
			}, defaultTimeoutSeconds*2, 1).Should(HaveLen(1))

			By("Check the reasons of related object")
			relatedObjectsOne := relatedObjects[0].(map[string]interface{})
			Expect(relatedObjectsOne["reason"].(string)).Should(Equal("Resource not found as expected"))

			By("Check the name of related object is -")
			name, _, err := unstructured.NestedString(relatedObjects[0].(map[string]interface{}),
				"object", "metadata", "name")
			Expect(err).ShouldNot(HaveOccurred())
			Expect(name).Should(Equal("-"))
		})
		It("should have 1 Compliant relatedObject under the policy's status", func() {
			By("Apply a ingress that is not related to policy")
			utils.Kubectl("apply", "-f", case37BadIngressPath)

			var managedPlc *unstructured.Unstructured

			Eventually(func(g Gomega) {
				managedPlc = utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case37PolicyMustnothaveName, testNamespace, true, defaultTimeoutSeconds)

				utils.CheckComplianceStatus(g, managedPlc, "Compliant")
			}, defaultTimeoutSeconds, 1).Should(Succeed())

			var relatedObjects []interface{}

			By("relatedObjects should exist")
			Eventually(func(g Gomega) interface{} {
				managedPlc = utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case37PolicyMustnothaveName, testNamespace, true, defaultTimeoutSeconds)

				var err error

				relatedObjects, _, err = unstructured.NestedSlice(managedPlc.Object, "status", "relatedObjects")

				g.Expect(err).ShouldNot(HaveOccurred())
				g.Expect(relatedObjects).ShouldNot(BeNil())

				return relatedObjects
				// Getting relatedObjects takes longer time
			}, defaultTimeoutSeconds*2, 1).Should(HaveLen(1))

			By("Check the kind of related objects")
			kind := relatedObjects[0].(map[string]interface{})["object"].(map[string]interface{})["kind"].(string)
			Expect(kind).Should(Equal("Ingress"))

			By("Check the reasons of related objects")
			relatedObjectsOne := relatedObjects[0].(map[string]interface{})
			Expect(relatedObjectsOne["reason"].(string)).Should(Equal("Resource not found as expected"))

			By("Check the name of related object is -")
			name, _, err := unstructured.NestedString(relatedObjects[0].(map[string]interface{}),
				"object", "metadata", "name")
			Expect(err).ShouldNot(HaveOccurred())
			Expect(name).Should(Equal("-"))
		})
		It("should have 1 NonCompliant relatedObject under the policy's status", func() {
			By("Apply a ingress")
			utils.Kubectl("apply", "-f", case37GoodIngressPath)

			var managedPlc *unstructured.Unstructured

			Eventually(func(g Gomega) {
				managedPlc = utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case37PolicyMustnothaveName, testNamespace, true, defaultTimeoutSeconds)

				utils.CheckComplianceStatus(g, managedPlc, "NonCompliant")
			}, defaultTimeoutSeconds, 1).Should(Succeed())

			var relatedObjects []interface{}

			By("relatedObjects should exist")
			Eventually(func(g Gomega) interface{} {
				managedPlc = utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case37PolicyMustnothaveName, testNamespace, true, defaultTimeoutSeconds)

				var err error

				relatedObjects, _, err = unstructured.NestedSlice(managedPlc.Object, "status", "relatedObjects")

				g.Expect(err).ShouldNot(HaveOccurred())
				g.Expect(relatedObjects).ShouldNot(BeNil())

				return relatedObjects
			}, defaultTimeoutSeconds*2, 1).Should(HaveLen(1))

			//  NonCompliant relatedObjects Should be deleted and only the Compliant relatedObject remain
			By("Check the kind of related objects")
			kind := relatedObjects[0].(map[string]interface{})["object"].(map[string]interface{})["kind"].(string)
			Expect(kind).Should(Equal("Ingress"))

			By("Check the reason of related object")
			reason := relatedObjects[0].(map[string]interface{})["reason"].(string)
			Expect(reason).Should(Equal("Resource found but should not exist"))

			By("Check the name of related object is not -")
			name, _, err := unstructured.NestedString(relatedObjects[0].(map[string]interface{}),
				"object", "metadata", "name")
			Expect(err).ShouldNot(HaveOccurred())
			Expect(name).ShouldNot(Equal("-"))
		})
		AfterAll(func() {
			utils.KubectlDelete("-f", case37PolicyNotHavePath, "-n", testNamespace)
			utils.KubectlDelete("-f", case37GoodIngressPath)
			utils.KubectlDelete("-f", case37BadIngressPath)
			utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case37PolicyMustnothaveName, testNamespace, false, defaultTimeoutSeconds)
		})
	})
})

var _ = Describe("Test a cluster-scope policy that is missing name ", Ordered, func() {
	const (
		case37BadIngressClassPath     string = "../resources/case37_no_name/case37_bad_ingressclass.yaml"
		case37PolicyCSPath            string = "../resources/case37_no_name/case37_no_name_clusterscope_policy.yaml"
		case37PolicyCSName            string = "case37-test-policy-clusterscope"
		case37PolicyCSMustnothavePath string = "../resources/case37_no_name/" +
			"case37_no_name_clusterscope_policy_nothave.yaml"
		case37PolicyCSMustnothaveName string = "case37-test-policy-clusterscope-mustnothave"
	)

	Describe("Test a musthave", func() {
		BeforeEach(func() {
			By("Creating an IngressClass")
			utils.Kubectl("apply", "-f", case37BadIngressClassPath)

			By("Creating a policy on managed")
			utils.Kubectl("apply", "-f", case37PolicyCSPath, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case37PolicyCSName, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).ShouldNot(BeNil())
		})
		It("should have 1 NonCompliant relatedObject under the policy's status", func() {
			var managedPlc *unstructured.Unstructured

			Eventually(func(g Gomega) {
				managedPlc = utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case37PolicyCSName, testNamespace, true, defaultTimeoutSeconds)

				utils.CheckComplianceStatus(g, managedPlc, "NonCompliant")
			}, defaultTimeoutSeconds, 1).Should(Succeed())

			var relatedObjects []interface{}

			By("relatedObjects should exist")
			Eventually(func(g Gomega) interface{} {
				managedPlc = utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case37PolicyCSName, testNamespace, true, defaultTimeoutSeconds)

				var err error

				relatedObjects, _, err = unstructured.NestedSlice(managedPlc.Object, "status", "relatedObjects")

				g.Expect(err).ShouldNot(HaveOccurred())
				g.Expect(relatedObjects).ShouldNot(BeNil())

				return relatedObjects
			}, defaultTimeoutSeconds*2, 1).Should(HaveLen(1))

			By("Check the kind of related object")
			kind := relatedObjects[0].(map[string]interface{})["object"].(map[string]interface{})["kind"].(string)
			Expect(kind).Should(Equal("IngressClass"))

			By("Check the reason of related object")
			reason := relatedObjects[0].(map[string]interface{})["reason"].(string)
			Expect(reason).Should(Equal("Resource found but does not match"))

			By("Check the name of relatedObject is -")
			name, _, err := unstructured.NestedString(relatedObjects[0].(map[string]interface{}),
				"object", "metadata", "name")
			Expect(err).ShouldNot(HaveOccurred())
			Expect(name).Should(Equal("-"))
		})
		AfterEach(func() {
			utils.KubectlDelete("-f", case37PolicyCSPath, "-n", testNamespace)
			utils.KubectlDelete("-f", case37BadIngressClassPath)
			utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case37PolicyCSName, testNamespace, false, defaultTimeoutSeconds)
		})
	})
	Describe("Test a mustnothave", func() {
		BeforeEach(func() {
			By("Creating a IngressClass that is not related to the policy")
			utils.Kubectl("apply", "-f", case37BadIngressClassPath)

			By("Creating a policy on managed")
			utils.Kubectl("apply", "-f", case37PolicyCSMustnothavePath, "-n", testNamespace)
			plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case37PolicyCSMustnothaveName, testNamespace, true, defaultTimeoutSeconds)
			Expect(plc).ShouldNot(BeNil())
		})
		It("should have 1 Compliant relatedObject under the policy's status", func() {
			var managedPlc *unstructured.Unstructured

			Eventually(func(g Gomega) {
				managedPlc = utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case37PolicyCSMustnothaveName, testNamespace, true, defaultTimeoutSeconds)

				utils.CheckComplianceStatus(g, managedPlc, "Compliant")
			}, defaultTimeoutSeconds, 1).Should(Succeed())

			var relatedObjects []interface{}

			By("relatedObjects should exist")
			Eventually(func(g Gomega) interface{} {
				managedPlc = utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					case37PolicyCSMustnothaveName, testNamespace, true, defaultTimeoutSeconds)

				var err error

				relatedObjects, _, err = unstructured.NestedSlice(managedPlc.Object, "status", "relatedObjects")

				g.Expect(err).ShouldNot(HaveOccurred())
				g.Expect(relatedObjects).ShouldNot(BeNil())

				return relatedObjects
			}, defaultTimeoutSeconds*2, 1).Should(HaveLen(1))

			By("Check the kind of relatedObject")
			kind := relatedObjects[0].(map[string]interface{})["object"].(map[string]interface{})["kind"].(string)
			Expect(kind).Should(Equal("IngressClass"))

			By("Check the reason of relatedObject")
			reason := relatedObjects[0].(map[string]interface{})["reason"].(string)
			Expect(reason).Should(Equal("Resource not found as expected"))

			By("Check the name of relatedObject is -")
			name, _, err := unstructured.NestedString(relatedObjects[0].(map[string]interface{}),
				"object", "metadata", "name")
			Expect(err).ShouldNot(HaveOccurred())
			Expect(name).Should(Equal("-"))
		})
		AfterEach(func() {
			utils.KubectlDelete("-f", case37PolicyCSMustnothavePath, "-n", testNamespace)
			utils.KubectlDelete("-f", case37BadIngressClassPath)
			utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case37PolicyCSMustnothaveName, testNamespace, false, defaultTimeoutSeconds)
		})
	})
})
