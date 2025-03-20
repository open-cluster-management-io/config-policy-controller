// Copyright Contributors to the Open Cluster Management project

package e2e

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"open-cluster-management.io/config-policy-controller/test/utils"
)

var _ = Describe("Custom compliance messages", Ordered, func() {
	const (
		parentYAML    = "../resources/case41_custom_message/case41-parent.yaml"
		parentName    = "case41-parent"
		cfgPolicyYAML = "../resources/case41_custom_message/case41.yaml"
		policyName    = "case41"
	)

	AfterAll(func() {
		By("Deleting case 41's resources")
		deleteConfigPolicies([]string{"case41"})
		utils.Kubectl("-n", "default", "delete", "pod", "nginx-pod-e2e-41", "--ignore-not-found")
		utils.Kubectl("delete", "namespace", "test-case-41", "--ignore-not-found")
		utils.Kubectl("-n", testNamespace, "delete", "policy", parentName, "--ignore-not-found")
	})

	It("Should have the right initial event for an inform policy with an invalid message template", func() {
		By("Creating the case41 ConfigurationPolicy")
		createObjWithParent(parentYAML, parentName, cfgPolicyYAML, testNamespace, gvrPolicy, gvrConfigPolicy)

		By("Verifying the ConfigurationPolicy starts NonCompliant")
		Eventually(func() interface{} {
			managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy, "case41", testNamespace, true, 5)

			return utils.GetComplianceState(managedPlc)
		}, 10, 1).Should(Equal("NonCompliant"))

		By("Verifying the event has the default message, and mentions the error")
		Eventually(func(g Gomega) string {
			events := utils.GetMatchingEvents(clientManaged, testNamespace, parentName, "policy:", "NonCompliant;", 5)
			g.Expect(events).ToNot(BeEmpty())

			return events[len(events)-1].Message
		}, 10, 1).Should(And(
			ContainSubstring("NonCompliant; violation - namespaces [test-case-41] not found; "),
			MatchRegexp("(failure processing the custom message: failed to parse custom template: .* unexpected EOF)"),
		))
	})

	It("Should still become compliant when enforced despite the invalid message template", func() {
		By("Patching the remediationAction")
		utils.EnforceConfigurationPolicy(policyName, testNamespace)

		By("Verifying the ConfigurationPolicy becomes Compliant")
		Eventually(func() interface{} {
			managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy, "case41", testNamespace, true, 5)

			return utils.GetComplianceState(managedPlc)
		}, 10, 1).Should(Equal("Compliant"))

		By("Verifying the event has the default message, and mentions the error")
		Eventually(func(g Gomega) string {
			events := utils.GetMatchingEvents(clientManaged, testNamespace, parentName, "policy:", "^Compliant;", 5)
			g.Expect(events).ToNot(BeEmpty())

			return events[len(events)-1].Message
		}, 10, 1).Should(And(
			ContainSubstring("Compliant; notification - namespaces [test-case-41] found as specified; "),
			MatchRegexp("(failure processing the custom message: failed to parse custom template: .* unexpected EOF)"),
		))
	})

	It("Should work with a range over the related objects", func() {
		By("Patching the custom compliance message")
		template := `{{ range .Policy.status.relatedObjects -}} the {{.object.kind}} ` +
			`{{.object.metadata.name}} is {{.compliant}} in namespace {{.object.metadata.namespace}} ` +
			`because '{{.reason}}'; {{ end }}`
		utils.Kubectl("patch", "configurationpolicy", policyName, "-n", testNamespace, "--type=json", "-p",
			`[{"op": "replace", "path": "/spec/customMessage/compliant", "value": "`+template+`"}]`)

		By("Verifying the event has the customized message")
		Eventually(func(g Gomega) string {
			events := utils.GetMatchingEvents(clientManaged, testNamespace, parentName, "policy:", "^Compliant;", 5)
			g.Expect(events).ToNot(BeEmpty())

			return events[len(events)-1].Message
		}, 10, 1).Should(And(
			ContainSubstring("the Namespace test-case-41 is Compliant in namespace <no value>"),
			ContainSubstring(`Compliant in namespace test-case-41 because 'Resource found as expected'`),
		))
	})

	It("Should be able to access specific fields inside the related objects in event-driven mode", func() {
		By("Patching the custom compliance message")
		template := ` {{ range .Policy.status.relatedObjects }}{{ if eq .object.kind \"Pod\" -}}` +
			`Pod {{.object.metadata.name}} is in phase '{{.object.status.phase}}'; {{ end }}{{ end }}`
		utils.Kubectl("patch", "configurationpolicy", policyName, "-n", testNamespace, "--type=json", "-p",
			`[{"op": "replace", "path": "/spec/customMessage/compliant", "value": "`+template+`"}]`)

		By("Verifying the event has the customized message")
		Eventually(func(g Gomega) string {
			events := utils.GetMatchingEvents(clientManaged, testNamespace, parentName, "policy:", "^Compliant;", 5)
			g.Expect(events).ToNot(BeEmpty())

			return events[len(events)-1].Message
		}, 10, 1).Should(And(
			ContainSubstring("Pod nginx-pod-e2e-41 is in phase 'Pending'"),
		))
	})

	It("Should not access extra fields inside the related objects in interval-based mode", func() {
		By("Patching the evaluationInterval")
		utils.Kubectl("patch", "configurationpolicy", policyName, "-n", testNamespace, "--type=json", "-p",
			`[{"op": "replace", "path": "/spec/evaluationInterval", "value": {"compliant": "2s"}}]`)

		By("Verifying the event has the customized message")
		Eventually(func(g Gomega) string {
			events := utils.GetMatchingEvents(clientManaged, testNamespace, parentName, "policy:", "^Compliant;", 5)
			g.Expect(events).ToNot(BeEmpty())

			return events[len(events)-1].Message
		}, 10, 1).Should(And(
			ContainSubstring("Pod nginx-pod-e2e-41 is in phase '<no value>'"),
		))
	})

	It("Should be able to access a diff when one is available", func() {
		By("Patching the policy")
		template := ` {{ range .Policy.status.relatedObjects }}{{ if eq .compliant \"NonCompliant\" -}}` +
			`{{.object.kind}} {{.object.metadata.name}} is NonCompliant, ` +
			`with diff '{{.properties.diff}}'; {{ end }}{{ end }}`
		utils.Kubectl("patch", "configurationpolicy", policyName, "-n", testNamespace, "--type=json", "-p",
			`[{"op": "replace", "path": "/spec/remediationAction", "value": "inform"},
			{"op": "add", "path": "/spec/object-templates/1/objectDefinition/status", "value": {"phase": "Blue"}},
			{"op": "replace", "path": "/spec/customMessage/noncompliant", "value": "`+template+`"}]`)

		By("Verifying the event has the customized message")
		Eventually(func(g Gomega) string {
			events := utils.GetMatchingEvents(clientManaged, testNamespace, parentName, "policy:", "NonCompliant;", 5)
			g.Expect(events).ToNot(BeEmpty())

			return events[len(events)-1].Message
		}, 10, 1).Should(And(
			ContainSubstring("nginx-pod-e2e-41 is NonCompliant, with diff '--- default/nginx-pod-e2e-41 : existing"),
			ContainSubstring("-  phase: Pending"),
			ContainSubstring("+  phase: Blue"),
		))
	})

	It("Should be able to access the default message", func() {
		By("Patching the policy")
		template := `Customized! But the default is good too: {{.DefaultMessage}}`
		utils.Kubectl("patch", "configurationpolicy", policyName, "-n", testNamespace, "--type=json", "-p",
			`[{"op": "replace", "path": "/spec/remediationAction", "value": "enforce"},
			{"op": "remove", "path": "/spec/object-templates/1/objectDefinition/status"},
			{"op": "replace", "path": "/spec/customMessage/compliant", "value": "`+template+`"}]`)

		By("Verifying the event has the customized message")
		Eventually(func(g Gomega) string {
			events := utils.GetMatchingEvents(clientManaged, testNamespace, parentName, "policy:", "^Compliant;", 5)
			g.Expect(events).ToNot(BeEmpty())

			return events[len(events)-1].Message
		}, 10, 1).Should(And(
			ContainSubstring("Customized!"),
			ContainSubstring("Compliant; notification - namespaces [test-case-41] found as specified; "),
		))
	})

	It("Should be able to use sprig functions", func() {
		By("Patching the policy")
		template := `{{upper .DefaultMessage}}`
		utils.Kubectl("patch", "configurationpolicy", policyName, "-n", testNamespace, "--type=json", "-p",
			`[{"op": "replace", "path": "/spec/customMessage/compliant", "value": "`+template+`"}]`)

		By("Verifying the event has the customized message")
		Eventually(func(g Gomega) string {
			events := utils.GetMatchingEvents(clientManaged, testNamespace, parentName, "policy:", "^Compliant;", 5)
			g.Expect(events).ToNot(BeEmpty())

			return events[len(events)-1].Message
		}, 10, 1).Should(And(
			ContainSubstring("TEST-CASE-41"),
			ContainSubstring("FOUND AS SPECIFIED"),
		))
	})
})
