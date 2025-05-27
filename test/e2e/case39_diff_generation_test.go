// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package e2e

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"open-cluster-management.io/config-policy-controller/test/utils"
)

var _ = Describe("Generate the diff", Ordered, func() {
	const (
		logPath          string = "../../build/_output/controller.log"
		configPolicyName string = "case39-policy-cfgmap-create"
		createYaml       string = "../resources/case39_diff_generation/case39-create-cfgmap-policy.yaml"
		updateYaml       string = "../resources/case39_diff_generation/case39-update-cfgmap-policy.yaml"
		statusYaml       string = "../resources/case39_diff_generation/case39-status-cfgmap-policy.yaml"
	)

	BeforeAll(func() {
		_, err := os.Stat(logPath)
		if err != nil {
			Skip(fmt.Sprintf("Skipping. Failed to find log file %s: %s", logPath, err.Error()))
		}
	})

	It("configmap should be created properly on the managed cluster", func() {
		By("Creating " + configPolicyName + " on managed")
		utils.Kubectl("apply", "-f", createYaml, "-n", testNamespace)
		plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
			configPolicyName, testNamespace, true, defaultTimeoutSeconds)
		Expect(plc).NotTo(BeNil())
		Eventually(func() interface{} {
			managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				configPolicyName, testNamespace, true, defaultTimeoutSeconds)

			return utils.GetStatusMessage(managedPlc)
		}, 120, 1).Should(Equal("configmaps [case39-map] found as specified in namespace default"))
	})

	It("configmap and status should be updated properly on the managed cluster", func() {
		By("Updating " + configPolicyName + " on managed")
		utils.Kubectl("apply", "-f", updateYaml, "-n", testNamespace)

		managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
			configPolicyName, testNamespace, true, defaultTimeoutSeconds)

		// Must check the event instead of the compliance message on the policy since the status updates so quickly
		// to say the object was found as specified.
		Eventually(func(g Gomega) {
			events := utils.GetMatchingEvents(clientManaged, testNamespace,
				configPolicyName,
				"Policy updated",
				regexp.QuoteMeta(
					`Policy status is Compliant: configmaps [case39-map] was updated successfully in namespace default`,
				),
				defaultTimeoutSeconds,
			)

			var foundEvent bool

			for _, event := range events {
				if event.InvolvedObject.UID == managedPlc.GetUID() {
					foundEvent = true

					break
				}
			}

			g.Expect(foundEvent).To(BeTrue(), "Did not find a compliance event indicating the ConfigMap was updated")
		}, 30, 1).Should(Succeed())
	})

	It("diff should be logged by the controller", func() {
		By("Checking the controller logs")
		logFile, err := os.Open(logPath)
		Expect(err).ToNot(HaveOccurred())
		defer logFile.Close()

		diff := ""
		foundDiff := false
		logScanner := bufio.NewScanner(logFile)
		logScanner.Split(bufio.ScanLines)
		for logScanner.Scan() {
			line := logScanner.Text()
			if foundDiff && strings.HasPrefix(line, "\t{") {
				foundDiff = false
			} else if foundDiff || strings.Contains(line, "Logging the diff:") {
				foundDiff = true
			} else {
				continue
			}

			diff += line + "\n"
		}

		Expect(diff).Should(ContainSubstring(`Logging the diff:
--- default/case39-map : existing
+++ default/case39-map : updated
@@ -1,8 +1,8 @@
 apiVersion: v1
 data:
-  fieldToUpdate: "1"
+  fieldToUpdate: "2"
 kind: ConfigMap`))

		Expect(diff).Should(ContainSubstring(
			`{"policy": "case39-policy-cfgmap-create", "name": "case39-map", "namespace": "default", ` +
				`"resource": "configmaps"}`,
		))
	})

	It("configmap and status should be updated properly on the managed cluster", func() {
		By("Updating " + configPolicyName + " on managed")
		utils.Kubectl("apply", "-f", statusYaml, "-n", testNamespace)

		Eventually(func(g Gomega) {
			managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				configPolicyName, testNamespace, true, defaultTimeoutSeconds)

			utils.CheckComplianceStatus(g, managedPlc, "Compliant")

			relatedObjects, _, err := unstructured.NestedSlice(managedPlc.Object, "status", "relatedObjects")
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(relatedObjects).To(HaveLen(1))

			uid, _, _ := unstructured.NestedString(relatedObjects[0].(map[string]interface{}), "properties", "uid")
			g.Expect(uid).ToNot(BeEmpty())

			diff, _, _ := unstructured.NestedString(relatedObjects[0].(map[string]interface{}), "properties", "diff")

			g.Expect(diff).Should(HavePrefix(`--- default/case39-map : existing
+++ default/case39-map : updated
@@ -1,8 +1,8 @@
 apiVersion: v1
 data:
-  fieldToUpdate: "2"
+  fieldToUpdate: "3"
 kind: ConfigMap
 metadata:`))
		}, defaultTimeoutSeconds, 1).Should(Succeed())
	})

	AfterAll(func() {
		deleteConfigPolicies([]string{configPolicyName})
		utils.KubectlDelete("configmap", "case39-map")
	})
})
