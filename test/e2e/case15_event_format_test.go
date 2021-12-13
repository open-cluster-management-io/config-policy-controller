// Copyright (c) 2021 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package e2e

import (
	"context"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/open-cluster-management/config-policy-controller/test/utils"
)

const (
	case15AlwaysCompliantParentYaml     = "../resources/case15_event_format/case15_parent_alwayscompliant.yaml"
	case15AlwaysCompliantParentName     = "parent-alwayscompliant"
	case15AlwaysCompliantYaml           = "../resources/case15_event_format/case15_mnh_pod_alwayscompliant.yaml"
	case15AlwaysCompliantName           = "mnh-pod-alwayscompliant"
	case15NeverCompliantYaml            = "../resources/case15_event_format/case15_mh_pod_nevercompliant.yaml"
	case15NeverCompliantName            = "mh-pod-nevercompliant"
	case15NeverCompliantParentYaml      = "../resources/case15_event_format/case15_parent_nevercompliant.yaml"
	case15NeverCompliantParentName      = "parent-nevercompliant"
	case15BecomesCompliantYaml          = "../resources/case15_event_format/case15_mh_pod_becomescompliant.yaml"
	case15BecomesCompliantName          = "mh-pod-becomescompliant"
	case15BecomesCompliantParentYaml    = "../resources/case15_event_format/case15_parent_becomescompliant.yaml"
	case15BecomesCompliantParentName    = "parent-becomescompliant"
	case15BecomesNonCompliantYaml       = "../resources/case15_event_format/case15_mnh_pod_becomesnoncompliant.yaml"
	case15BecomesNonCompliantName       = "mnh-pod-becomesnoncompliant"
	case15BecomesNonCompliantParentYaml = "../resources/case15_event_format/case15_parent_becomesnoncompliant.yaml"
	case15BecomesNonCompliantParentName = "parent-becomesnoncompliant"
	case15PodForNonComplianceYaml       = "../resources/case15_event_format/case15_becomesnoncompliant_pod.yaml"
)

var _ = Describe("Testing compliance event formatting", func() {
	It("Records the right events for a policy that is always compliant", func() {
		By("Creating parent policy " + case15AlwaysCompliantParentName + " on " + testNamespace)
		utils.Kubectl("apply", "-f", case15AlwaysCompliantParentYaml, "-n", testNamespace)
		parent := utils.GetWithTimeout(clientManagedDynamic, gvrPolicy,
			case15AlwaysCompliantParentName, testNamespace, true, defaultTimeoutSeconds)
		Expect(parent).NotTo(BeNil())

		By("Creating compliant policy " + case15AlwaysCompliantName + " on " + testNamespace + " with parent " +
			case15AlwaysCompliantParentName)
		plcDef := utils.ParseYaml(case15AlwaysCompliantYaml)
		ownerRefs := plcDef.GetOwnerReferences()
		ownerRefs[0].UID = parent.GetUID()
		plcDef.SetOwnerReferences(ownerRefs)
		_, err := clientManagedDynamic.Resource(gvrConfigPolicy).Namespace(testNamespace).
			Create(context.TODO(), plcDef, v1.CreateOptions{})
		Expect(err).To(BeNil())

		plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
			case15AlwaysCompliantName, testNamespace, true, defaultTimeoutSeconds)
		Expect(plc).NotTo(BeNil())
		Eventually(func() interface{} {
			managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case15AlwaysCompliantName, testNamespace, true, defaultTimeoutSeconds)

			return utils.GetComplianceState(managedPlc)
		}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))

		By("Checking events on the configurationpolicy")
		compPlcEvents := utils.GetMatchingEvents(clientManaged, testNamespace,
			case15AlwaysCompliantName, "", "Policy status is: Compliant", defaultTimeoutSeconds)
		Expect(compPlcEvents).NotTo(BeEmpty())
		nonCompPlcEvents := utils.GetMatchingEvents(clientManaged, testNamespace,
			case15AlwaysCompliantName, "", "Policy status is: NonCompliant", defaultTimeoutSeconds)
		Expect(nonCompPlcEvents).To(BeEmpty())

		By("Checking events on the parent policy")
		compParentEvents := utils.GetMatchingEvents(clientManaged, testNamespace,
			case15AlwaysCompliantParentName, "policy: "+testNamespace+"/"+
				case15AlwaysCompliantName, "^Compliant;", defaultTimeoutSeconds)
		Expect(compParentEvents).NotTo(BeEmpty())
		nonCompParentEvents := utils.GetMatchingEvents(clientManaged, testNamespace,
			case15AlwaysCompliantParentName, "policy: "+testNamespace+"/"+
				case15AlwaysCompliantName, "^NonCompliant;", defaultTimeoutSeconds)
		Expect(nonCompParentEvents).To(BeEmpty())
	})
	It("Records the right events for a policy that is never compliant", func() {
		By("Creating parent policy " + case15NeverCompliantParentName + " on " + testNamespace)
		utils.Kubectl("apply", "-f", case15NeverCompliantParentYaml, "-n", testNamespace)
		parent := utils.GetWithTimeout(clientManagedDynamic, gvrPolicy,
			case15NeverCompliantParentName, testNamespace, true, defaultTimeoutSeconds)
		Expect(parent).NotTo(BeNil())

		By("Creating noncompliant policy " + case15NeverCompliantName + " on " + testNamespace + " with parent " +
			case15NeverCompliantParentName)
		plcDef := utils.ParseYaml(case15NeverCompliantYaml)
		ownerRefs := plcDef.GetOwnerReferences()
		ownerRefs[0].UID = parent.GetUID()
		plcDef.SetOwnerReferences(ownerRefs)
		_, err := clientManagedDynamic.Resource(gvrConfigPolicy).Namespace(testNamespace).
			Create(context.TODO(), plcDef, v1.CreateOptions{})
		Expect(err).To(BeNil())

		plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
			case15NeverCompliantName, testNamespace, true, defaultTimeoutSeconds)
		Expect(plc).NotTo(BeNil())
		Eventually(func() interface{} {
			managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case15NeverCompliantName, testNamespace, true, defaultTimeoutSeconds)

			return utils.GetComplianceState(managedPlc)
		}, defaultTimeoutSeconds, 1).Should(Equal("NonCompliant"))

		By("Checking events on the configurationpolicy")
		compPlcEvents := utils.GetMatchingEvents(clientManaged, testNamespace,
			case15NeverCompliantName, "", "Policy status is: Compliant", defaultTimeoutSeconds)
		Expect(compPlcEvents).To(BeEmpty())
		nonCompPlcEvents := utils.GetMatchingEvents(clientManaged, testNamespace,
			case15NeverCompliantName, "", "Policy status is: NonCompliant", defaultTimeoutSeconds)
		Expect(nonCompPlcEvents).NotTo(BeEmpty())

		By("Checking events on the parent policy")
		compParentEvents := utils.GetMatchingEvents(clientManaged, testNamespace,
			case15NeverCompliantParentName, "policy: "+testNamespace+"/"+case15NeverCompliantName,
			"^Compliant;", defaultTimeoutSeconds)
		Expect(compParentEvents).To(BeEmpty())
		nonCompParentEvents := utils.GetMatchingEvents(clientManaged, testNamespace,
			case15NeverCompliantParentName, "policy: "+testNamespace+"/"+case15NeverCompliantName,
			"^NonCompliant;", defaultTimeoutSeconds)
		Expect(nonCompParentEvents).NotTo(BeEmpty())
	})
	It("Records events for a policy that becomes compliant", func() {
		By("Creating parent policy " + case15BecomesCompliantParentName + " on " + testNamespace)
		utils.Kubectl("apply", "-f", case15BecomesCompliantParentYaml, "-n", testNamespace)
		parent := utils.GetWithTimeout(clientManagedDynamic, gvrPolicy,
			case15BecomesCompliantParentName, testNamespace, true, defaultTimeoutSeconds)
		Expect(parent).NotTo(BeNil())

		By("Creating noncompliant policy " + case15BecomesCompliantName + " on " + testNamespace + " with parent " +
			case15BecomesCompliantParentName)
		plcDef := utils.ParseYaml(case15BecomesCompliantYaml)
		ownerRefs := plcDef.GetOwnerReferences()
		ownerRefs[0].UID = parent.GetUID()
		plcDef.SetOwnerReferences(ownerRefs)
		_, err := clientManagedDynamic.Resource(gvrConfigPolicy).Namespace(testNamespace).
			Create(context.TODO(), plcDef, v1.CreateOptions{})
		Expect(err).To(BeNil())

		plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
			case15BecomesCompliantName, testNamespace, true, defaultTimeoutSeconds)
		Expect(plc).NotTo(BeNil())
		Eventually(func() interface{} {
			managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case15BecomesCompliantName, testNamespace, true, defaultTimeoutSeconds)

			return utils.GetComplianceState(managedPlc)
		}, defaultTimeoutSeconds, 1).Should(Equal("NonCompliant"))

		By("Enforcing the policy to make it compliant")
		utils.Kubectl("patch", "configurationpolicy", case15BecomesCompliantName, `--type=json`,
			`-p=[{"op":"replace","path":"/spec/remediationAction","value":"enforce"}]`, "-n", testNamespace)
		Eventually(func() interface{} {
			managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case15BecomesCompliantName, testNamespace, true, defaultTimeoutSeconds)

			return utils.GetComplianceState(managedPlc)
		}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))

		By("Checking for compliant events on the configurationpolicy and the parent policy")
		compPlcEvents := utils.GetMatchingEvents(clientManaged, testNamespace,
			case15BecomesCompliantName, "", "Policy status is: Compliant", defaultTimeoutSeconds)
		Expect(compPlcEvents).NotTo(BeEmpty())
		compParentEvents := utils.GetMatchingEvents(clientManaged, testNamespace, case15BecomesCompliantParentName,
			"policy: "+testNamespace+"/"+case15BecomesCompliantName, "^Compliant;", defaultTimeoutSeconds)
		Expect(compParentEvents).NotTo(BeEmpty())
	})
	It("Records events for a policy that becomes noncompliant", func() {
		By("Creating parent policy " + case15BecomesNonCompliantParentName + " on " + testNamespace)
		utils.Kubectl("apply", "-f", case15BecomesNonCompliantParentYaml, "-n", testNamespace)
		parent := utils.GetWithTimeout(clientManagedDynamic, gvrPolicy,
			case15BecomesNonCompliantParentName, testNamespace, true, defaultTimeoutSeconds)
		Expect(parent).NotTo(BeNil())

		By("Creating compliant policy " + case15BecomesNonCompliantName + " on " + testNamespace + " with parent " +
			case15BecomesNonCompliantParentName)
		plcDef := utils.ParseYaml(case15BecomesNonCompliantYaml)
		ownerRefs := plcDef.GetOwnerReferences()
		ownerRefs[0].UID = parent.GetUID()
		plcDef.SetOwnerReferences(ownerRefs)
		_, err := clientManagedDynamic.Resource(gvrConfigPolicy).Namespace(testNamespace).
			Create(context.TODO(), plcDef, v1.CreateOptions{})
		Expect(err).To(BeNil())

		plc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
			case15BecomesNonCompliantName, testNamespace, true, defaultTimeoutSeconds)
		Expect(plc).NotTo(BeNil())
		Eventually(func() interface{} {
			managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case15BecomesNonCompliantName, testNamespace, true, defaultTimeoutSeconds)

			return utils.GetComplianceState(managedPlc)
		}, defaultTimeoutSeconds, 1).Should(Equal("Compliant"))

		By("Creating a pod to make it noncompliant")
		utils.Kubectl("apply", "-f", case15PodForNonComplianceYaml)
		Eventually(func() interface{} {
			managedPlc := utils.GetWithTimeout(clientManagedDynamic, gvrConfigPolicy,
				case15BecomesNonCompliantName, testNamespace, true, defaultTimeoutSeconds)

			return utils.GetComplianceState(managedPlc)
		}, defaultTimeoutSeconds, 1).Should(Equal("NonCompliant"))

		By("Checking for noncompliant events on the configurationpolicy and the parent policy")
		nonCompPlcEvents := utils.GetMatchingEvents(clientManaged, testNamespace,
			case15BecomesNonCompliantName, "", "Policy status is: NonCompliant", defaultTimeoutSeconds)
		Expect(nonCompPlcEvents).NotTo(BeEmpty())
		nonCompParentEvents := utils.GetMatchingEvents(clientManaged, testNamespace,
			case15BecomesNonCompliantParentName, "policy: "+testNamespace+"/"+case15BecomesNonCompliantName,
			"^NonCompliant;", defaultTimeoutSeconds)
		Expect(nonCompParentEvents).NotTo(BeEmpty())
	})
})
