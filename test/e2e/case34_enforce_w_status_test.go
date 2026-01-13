// Copyright (c) 2023 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package e2e

import (
	"strconv"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	policyv1 "open-cluster-management.io/config-policy-controller/api/v1"
	"open-cluster-management.io/config-policy-controller/test/utils"
)

var _ = Describe("Test compliance events of enforced policies that define a status", Serial, func() {
	const rsrcPath = "../resources/case34_enforce_w_status/"

	Describe("Test compliance events on a namespace with a stuck deletion", func() {
		const (
			policyYAML    = rsrcPath + "policy.yaml"
			policyName    = "case34-parent"
			cfgPlcYAML    = rsrcPath + "config-policy-for-ns.yaml"
			updatedCfgPlc = rsrcPath + "config-policy-for-ns-updated.yaml"
			cfgPlcName    = "case34-for-ns"
			nsName        = "case34-ns"
			finalizerName = "policy.open-cluster-management.io/stuck-test"
		)

		It("Should have the expected events", func() {
			By("Setting up the policy")
			createObjWithParent(policyYAML, policyName, cfgPlcYAML, testNamespace, gvrPolicy, gvrConfigPolicy)

			By("Checking there is a NonCompliant event on the policy")
			Eventually(func() []policyv1.HistoryEvent {
				return utils.GetHistoryEvents(clientManagedDynamic, gvrConfigPolicy,
					cfgPlcName, testNamespace, "^NonCompliant;")
			}, defaultTimeoutSeconds, 1).ShouldNot(BeEmpty())
			Eventually(func() interface{} {
				return utils.GetMatchingEvents(clientManaged, testNamespace,
					policyName, cfgPlcName, "^NonCompliant;", defaultTimeoutSeconds)
			}, defaultTimeoutSeconds, 5).ShouldNot(BeEmpty())

			By("Verifying that the diff is correct")
			Eventually(func(g Gomega) {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					cfgPlcName, testNamespace, true, defaultTimeoutSeconds)

				relObjs, found, err := unstructured.NestedSlice(managedPlc.Object, "status", "relatedObjects")
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(found).To(BeTrue())
				g.Expect(relObjs).To(HaveLen(1))

				relNS, ok := relObjs[0].(map[string]any)
				g.Expect(ok).To(BeTrue())

				diff, found, err := unstructured.NestedString(relNS, "properties", "diff")
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(found).To(BeTrue())
				g.Expect(diff).To(ContainSubstring("-  phase: Active"))
				g.Expect(diff).To(ContainSubstring("+  phase: Terminating"))
			}, defaultTimeoutSeconds, 5).Should(Succeed())

			By("Checking there are no Compliant events on the policy")
			Consistently(func() []policyv1.HistoryEvent {
				return utils.GetHistoryEvents(clientManagedDynamic, gvrConfigPolicy,
					cfgPlcName, testNamespace, "^Compliant;")
			}, defaultConsistentlyDuration, 5).Should(BeEmpty())
			Eventually(func() interface{} {
				return utils.GetMatchingEvents(clientManaged, testNamespace,
					policyName, cfgPlcName, "^Compliant;", defaultTimeoutSeconds)
			}, defaultTimeoutSeconds, 1).Should(BeEmpty())

			By("Updating the policy")
			utils.Kubectl("apply", "-f", updatedCfgPlc, "-n", testNamespace)

			By("Checking there are no Compliant events created during the update flow")
			Consistently(func() []policyv1.HistoryEvent {
				return utils.GetHistoryEvents(clientManagedDynamic, gvrConfigPolicy,
					cfgPlcName, testNamespace, "^Compliant;")
			}, defaultConsistentlyDuration, 5).Should(BeEmpty())
			Eventually(func() interface{} {
				return utils.GetMatchingEvents(clientManaged, testNamespace,
					policyName, cfgPlcName, "^Compliant;", defaultTimeoutSeconds)
			}, defaultTimeoutSeconds, 1).Should(BeEmpty())

			By("Setting a finalizer on the namespace")
			utils.Kubectl("patch", "ns", nsName, "--type=merge",
				`-p={"metadata":{"finalizers":["`+finalizerName+`"]}}`)
			Eventually(func(g Gomega) []string {
				ns := utils.GetClusterLevelWithTimeout(clientManagedDynamic, gvrNS,
					nsName, true, defaultTimeoutSeconds)
				g.Expect(ns).ShouldNot(BeNil())

				return ns.GetFinalizers()
			}, defaultTimeoutSeconds, 2).Should(ContainElement(finalizerName))

			By("Marking the namespace for deletion")
			utils.Kubectl("delete", "ns", nsName, "--wait=false")

			By("Checking there is now a Compliant event on the policy")
			Eventually(func() []policyv1.HistoryEvent {
				return utils.GetHistoryEvents(clientManagedDynamic, gvrConfigPolicy,
					cfgPlcName, testNamespace, "^Compliant;")
			}, defaultTimeoutSeconds, 1).ShouldNot(BeEmpty())
			Eventually(func() interface{} {
				return utils.GetMatchingEvents(clientManaged, testNamespace,
					policyName, cfgPlcName, "^Compliant;", defaultTimeoutSeconds)
			}, defaultTimeoutSeconds, 5).ShouldNot(BeEmpty())
		})

		AfterEach(func() {
			if CurrentSpecReport().Failed() {
				events := utils.GetMatchingEvents(clientManaged, testNamespace,
					policyName, ".*", ".*", defaultTimeoutSeconds)

				By("Test failed, printing compliance events for debugging, event count = " + strconv.Itoa(len(events)))
				for _, ev := range events {
					GinkgoWriter.Println("---")
					GinkgoWriter.Println("Name:", ev.Name)
					GinkgoWriter.Println("Reason:", ev.Reason)
					GinkgoWriter.Println("Message:", ev.Message)
					GinkgoWriter.Println("FirstTimestamp:", ev.FirstTimestamp)
					GinkgoWriter.Println("LastTimestamp:", ev.LastTimestamp)
					GinkgoWriter.Println("Count:", ev.Count)
					GinkgoWriter.Println("Type:", ev.Type)
					GinkgoWriter.Println("---")
				}
			}

			utils.KubectlDelete("policy", policyName, "-n", "managed")
			configPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				cfgPlcName, "managed", false, defaultTimeoutSeconds,
			)
			Expect(configPlc).To(BeNil())

			utils.Kubectl("patch", "ns", nsName, "--type=merge", `-p={"metadata":{"finalizers":[]}}`)
			utils.KubectlDelete("ns", nsName)
			utils.KubectlDelete("event", "--field-selector=involvedObject.name="+policyName, "-n", "managed")
			utils.KubectlDelete("event", "--field-selector=involvedObject.name="+cfgPlcName, "-n", "managed")
		})
	})

	Describe("Test compliance events of an object without a status initially", func() {
		const (
			policyYAML = rsrcPath + "policy.yaml"
			policyName = "case34-parent"
			cfgPlcYAML = rsrcPath + "config-policy-for-nested.yaml"
			cfgPlcName = "case34-for-nested"
			nestedName = "case34-nested"
			nestedNS   = "default"
		)

		It("Should have the expected events", func() {
			By("Setting up the policy")
			createObjWithParent(policyYAML, policyName, cfgPlcYAML, testNamespace, gvrPolicy, gvrConfigPolicy)

			By("Checking there is a NonCompliant event on the policy")
			Eventually(func() []policyv1.HistoryEvent {
				return utils.GetHistoryEvents(clientManagedDynamic, gvrConfigPolicy,
					cfgPlcName, testNamespace, "^NonCompliant;")
			}, defaultTimeoutSeconds, 1).ShouldNot(BeEmpty())
			Eventually(func() interface{} {
				return utils.GetMatchingEvents(clientManaged, testNamespace,
					policyName, cfgPlcName, "^NonCompliant;", defaultTimeoutSeconds)
			}, defaultTimeoutSeconds, 5).ShouldNot(BeEmpty())

			By("Verifying that the nested configuration policy has no compliance status")
			Eventually(func(g Gomega) bool {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					nestedName, nestedNS, true, defaultTimeoutSeconds)

				_, found, err := unstructured.NestedString(managedPlc.Object, "status", "compliant")
				g.Expect(err).NotTo(HaveOccurred())

				return found
			}, defaultTimeoutSeconds, 5).Should(BeFalseBecause("The ConfigurationPolicy should have no status"))
			Consistently(func(g Gomega) bool {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					nestedName, nestedNS, true, defaultTimeoutSeconds)

				_, found, err := unstructured.NestedString(managedPlc.Object, "status", "compliant")
				g.Expect(err).NotTo(HaveOccurred())

				return found
			}, defaultTimeoutSeconds, 5).Should(BeFalseBecause("The ConfigurationPolicy should have no status"))

			By("Checking there are no Compliant events on the policy")
			Eventually(func() []policyv1.HistoryEvent {
				return utils.GetHistoryEvents(clientManagedDynamic, gvrConfigPolicy,
					cfgPlcName, testNamespace, "^Compliant;")
			}, defaultConsistentlyDuration, 5).Should(BeEmpty())
			Eventually(func() interface{} {
				return utils.GetMatchingEvents(clientManaged, testNamespace,
					policyName, cfgPlcName, "^Compliant;", defaultTimeoutSeconds)
			}, defaultTimeoutSeconds, 1).Should(BeEmpty())

			By("Setting a different field in the status of the nested configuration policy")
			utils.Kubectl("patch", "configurationpolicy", "-n="+nestedNS, nestedName, "--subresource=status",
				"--type=merge", `-p={"status":{"lastEvaluated":"recently"}}`)

			By("Verifying that the nested configuration policy has only the expected status field")
			Eventually(func(g Gomega) {
				managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
					nestedName, nestedNS, true, defaultTimeoutSeconds)

				_, found, err := unstructured.NestedString(managedPlc.Object, "status", "lastEvaluated")
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(found).To(BeTrueBecause("The lastEvaluated should be set in the status"))

				_, found, err = unstructured.NestedString(managedPlc.Object, "status", "compliant")
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(found).To(BeFalseBecause("The compliant status should not be set"))
			}, defaultTimeoutSeconds, 5).Should(Succeed())

			By("Verifying there are still no Compliant events on the policy")
			Consistently(func() []policyv1.HistoryEvent {
				return utils.GetHistoryEvents(clientManagedDynamic, gvrConfigPolicy,
					cfgPlcName, testNamespace, "^Compliant;")
			}, defaultConsistentlyDuration, 5).Should(BeEmpty())
			Eventually(func() interface{} {
				return utils.GetMatchingEvents(clientManaged, testNamespace,
					policyName, cfgPlcName, "^Compliant;", defaultTimeoutSeconds)
			}, defaultTimeoutSeconds, 1).Should(BeEmpty())

			By("Setting the desired field in the status of the nested configuration policy")
			utils.Kubectl("patch", "configurationpolicy", "-n="+nestedNS, nestedName, "--subresource=status",
				"--type=merge", `-p={"status":{"compliant":"NonCompliant"}}`)

			By("Checking that there is now a Compliant event on the policy")
			Eventually(func() []policyv1.HistoryEvent {
				return utils.GetHistoryEvents(clientManagedDynamic, gvrConfigPolicy,
					cfgPlcName, testNamespace, "^Compliant;")
			}, defaultTimeoutSeconds, 5).ShouldNot(BeEmpty())
			Eventually(func() interface{} {
				return utils.GetMatchingEvents(clientManaged, testNamespace,
					policyName, cfgPlcName, "^Compliant;", defaultTimeoutSeconds)
			}, defaultTimeoutSeconds, 1).ShouldNot(BeEmpty())
		})

		AfterEach(func() {
			utils.KubectlDelete("policy", policyName, "-n", "managed")
			configPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				cfgPlcName, "managed", false, defaultTimeoutSeconds,
			)
			Expect(configPlc).To(BeNil())

			utils.KubectlDelete("configurationpolicy", nestedName, "-n", nestedNS)
			utils.KubectlDelete("event", "--field-selector=involvedObject.name="+policyName, "-n", "managed")
			utils.KubectlDelete("event", "--field-selector=involvedObject.name="+cfgPlcName, "-n", "managed")
		})
	})
})
