package e2e

import (
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	policyv1 "open-cluster-management.io/config-policy-controller/api/v1"
	policyv1beta1 "open-cluster-management.io/config-policy-controller/api/v1beta1"
	"open-cluster-management.io/config-policy-controller/test/utils"
)

var _ = Describe("Test installing an operator from OperatorPolicy", Ordered, func() {
	const (
		opPolTestNS      = "operator-policy-testns"
		parentPolicyYAML = "../resources/case38_operator_install/parent-policy.yaml"
		parentPolicyName = "parent-policy"
		opPolTimeout     = 30
	)

	check := func(
		polName string,
		wantNonCompliant bool,
		expectedRelatedObjs []policyv1.RelatedObject,
		expectedCondition metav1.Condition,
		expectedEventMsgSnippet string,
	) {
		checkFunc := func(g Gomega) {
			GinkgoHelper()

			unstructPolicy := utils.GetWithTimeout(clientManagedDynamic, gvrOperatorPolicy, polName,
				opPolTestNS, true, opPolTimeout)

			policyJSON, err := json.MarshalIndent(unstructPolicy.Object, "", "  ")
			g.Expect(err).NotTo(HaveOccurred())

			GinkgoWriter.Printf("Debug info for failure.\npolicy JSON: %s\nwanted related objects: %+v\n"+
				"wanted condition: %+v\n", string(policyJSON), expectedRelatedObjs, expectedCondition)

			policy := policyv1beta1.OperatorPolicy{}
			err = json.Unmarshal(policyJSON, &policy)
			g.Expect(err).NotTo(HaveOccurred())

			if wantNonCompliant {
				g.Expect(policy.Status.ComplianceState).To(Equal(policyv1.NonCompliant))
			}

			matchingRelated := policy.Status.RelatedObjsOfKind(expectedRelatedObjs[0].Object.Kind)
			g.Expect(matchingRelated).To(HaveLen(len(expectedRelatedObjs)))

			for _, expectedRelObj := range expectedRelatedObjs {
				foundMatchingName := false
				unnamed := expectedRelObj.Object.Metadata.Name == ""

				for _, actualRelObj := range matchingRelated {
					if unnamed || actualRelObj.Object.Metadata.Name == expectedRelObj.Object.Metadata.Name {
						foundMatchingName = true
						g.Expect(actualRelObj.Compliant).To(Equal(expectedRelObj.Compliant))
						g.Expect(actualRelObj.Reason).To(Equal(expectedRelObj.Reason))
					}
				}

				g.Expect(foundMatchingName).To(BeTrue())
			}

			idx, actualCondition := policy.Status.GetCondition(expectedCondition.Type)
			g.Expect(idx).NotTo(Equal(-1))
			g.Expect(actualCondition.Status).To(Equal(expectedCondition.Status))
			g.Expect(actualCondition.Reason).To(Equal(expectedCondition.Reason))
			g.Expect(actualCondition.Message).To(Equal(expectedCondition.Message))

			g.Expect(utils.GetMatchingEvents(
				clientManaged, opPolTestNS, parentPolicyName, "", expectedEventMsgSnippet, opPolTimeout,
			)).NotTo(BeEmpty())
		}

		EventuallyWithOffset(1, checkFunc, opPolTimeout, 3).Should(Succeed())
		ConsistentlyWithOffset(1, checkFunc, 3, 1).Should(Succeed())
	}

	Describe("Testing OperatorGroup behavior when it is not specified in the policy", Ordered, func() {
		const (
			opPolYAML        = "../resources/case38_operator_install/operator-policy-no-group.yaml"
			opPolName        = "oppol-no-group"
			extraOpGroupYAML = "../resources/case38_operator_install/extra-operator-group.yaml"
			extraOpGroupName = "extra-operator-group"
		)
		BeforeAll(func() {
			utils.Kubectl("create", "ns", opPolTestNS)
			DeferCleanup(func() {
				utils.Kubectl("delete", "ns", opPolTestNS)
			})

			createObjWithParent(parentPolicyYAML, parentPolicyName,
				opPolYAML, opPolTestNS, gvrPolicy, gvrOperatorPolicy)
		})

		It("Should initially be NonCompliant", func() {
			check(
				opPolName,
				true,
				[]policyv1.RelatedObject{{
					Object: policyv1.ObjectResource{
						Kind:       "OperatorGroup",
						APIVersion: "operators.coreos.com/v1",
						Metadata: policyv1.ObjectMetadata{
							Name:      "*",
							Namespace: opPolTestNS,
						},
					},
					Compliant: "NonCompliant",
					Reason:    "Resource not found but should exist",
				}},
				metav1.Condition{
					Type:    "OperatorGroupCompliant",
					Status:  metav1.ConditionFalse,
					Reason:  "OperatorGroupMissing",
					Message: "the OperatorGroup required by the policy was not found",
				},
				"the OperatorGroup required by the policy was not found",
			)
		})
		It("Should create the OperatorGroup when it is enforced", func() {
			utils.Kubectl("patch", "operatorpolicy", opPolName, "-n", opPolTestNS, "--type=json", "-p",
				`[{"op": "replace", "path": "/spec/remediationAction", "value": "enforce"}]`)
			check(
				opPolName,
				false,
				[]policyv1.RelatedObject{{
					Object: policyv1.ObjectResource{
						Kind:       "OperatorGroup",
						APIVersion: "operators.coreos.com/v1",
					},
					Compliant: "Compliant",
					Reason:    "Resource found as expected",
				}},
				metav1.Condition{
					Type:    "OperatorGroupCompliant",
					Status:  metav1.ConditionTrue,
					Reason:  "OperatorGroupMatches",
					Message: "the OperatorGroup matches what is required by the policy",
				},
				"the OperatorGroup required by the policy was created",
			)
		})
		It("Should become NonCompliant when an extra OperatorGroup is added", func() {
			utils.Kubectl("apply", "-f", extraOpGroupYAML, "-n", opPolTestNS)
			check(
				opPolName,
				true,
				[]policyv1.RelatedObject{{
					Object: policyv1.ObjectResource{
						Kind:       "OperatorGroup",
						APIVersion: "operators.coreos.com/v1",
					},
					Compliant: "NonCompliant",
					Reason:    "There is more than one OperatorGroup in this namespace",
				}, {
					Object: policyv1.ObjectResource{
						Kind:       "OperatorGroup",
						APIVersion: "operators.coreos.com/v1",
						Metadata: policyv1.ObjectMetadata{
							Name:      extraOpGroupName,
							Namespace: opPolTestNS,
						},
					},
					Compliant: "NonCompliant",
					Reason:    "There is more than one OperatorGroup in this namespace",
				}},
				metav1.Condition{
					Type:    "OperatorGroupCompliant",
					Status:  metav1.ConditionFalse,
					Reason:  "TooManyOperatorGroups",
					Message: "there is more than one OperatorGroup in the namespace",
				},
				"there is more than one OperatorGroup in the namespace",
			)
		})
		It("Should warn about the OperatorGroup when it doesn't match the default", func() {
			utils.Kubectl("patch", "operatorpolicy", opPolName, "-n", opPolTestNS, "--type=json", "-p",
				`[{"op": "replace", "path": "/spec/remediationAction", "value": "inform"}]`)
			utils.Kubectl("delete", "operatorgroup", "-n", opPolTestNS, "--all")
			utils.Kubectl("apply", "-f", extraOpGroupYAML, "-n", opPolTestNS)
			check(
				opPolName,
				false,
				[]policyv1.RelatedObject{{
					Object: policyv1.ObjectResource{
						Kind:       "OperatorGroup",
						APIVersion: "operators.coreos.com/v1",
					},
					Compliant: "Compliant",
					Reason:    "Resource found as expected",
				}},
				metav1.Condition{
					Type:   "OperatorGroupCompliant",
					Status: metav1.ConditionTrue,
					Reason: "PreexistingOperatorGroupFound",
					Message: "the policy does not specify an OperatorGroup but one already exists in the namespace" +
						" - assuming that OperatorGroup is correct",
				},
				"assuming that OperatorGroup is correct",
			)
		})
	})
	Describe("Testing OperatorGroup behavior when it is specified in the policy", Ordered, func() {
		const (
			opPolYAML            = "../resources/case38_operator_install/operator-policy-with-group.yaml"
			opPolName            = "oppol-with-group"
			incorrectOpGroupYAML = "../resources/case38_operator_install/incorrect-operator-group.yaml"
			incorrectOpGroupName = "incorrect-operator-group"
			scopedOpGroupYAML    = "../resources/case38_operator_install/scoped-operator-group.yaml"
			scopedOpGroupName    = "scoped-operator-group"
			extraOpGroupYAML     = "../resources/case38_operator_install/extra-operator-group.yaml"
			extraOpGroupName     = "extra-operator-group"
		)

		BeforeAll(func() {
			utils.Kubectl("create", "ns", opPolTestNS)
			DeferCleanup(func() {
				utils.Kubectl("delete", "ns", opPolTestNS)
			})

			utils.Kubectl("apply", "-f", incorrectOpGroupYAML, "-n", opPolTestNS)

			createObjWithParent(parentPolicyYAML, parentPolicyName,
				opPolYAML, opPolTestNS, gvrPolicy, gvrOperatorPolicy)
		})

		It("Should initially be NonCompliant", func() {
			check(
				opPolName,
				true,
				[]policyv1.RelatedObject{{
					Object: policyv1.ObjectResource{
						Kind:       "OperatorGroup",
						APIVersion: "operators.coreos.com/v1",
						Metadata: policyv1.ObjectMetadata{
							Name:      incorrectOpGroupName,
							Namespace: opPolTestNS,
						},
					},
					Compliant: "NonCompliant",
					Reason:    "Resource found but does not match",
				}, {
					Object: policyv1.ObjectResource{
						Kind:       "OperatorGroup",
						APIVersion: "operators.coreos.com/v1",
						Metadata: policyv1.ObjectMetadata{
							Name:      scopedOpGroupName,
							Namespace: opPolTestNS,
						},
					},
					Compliant: "NonCompliant",
					Reason:    "Resource not found but should exist",
				}},
				metav1.Condition{
					Type:    "OperatorGroupCompliant",
					Status:  metav1.ConditionFalse,
					Reason:  "OperatorGroupMismatch",
					Message: "the OperatorGroup found on the cluster does not match the policy",
				},
				"the OperatorGroup found on the cluster does not match the policy",
			)
		})
		It("Should match when the OperatorGroup is manually corrected", func() {
			utils.Kubectl("delete", "operatorgroup", incorrectOpGroupName, "-n", opPolTestNS)
			utils.Kubectl("apply", "-f", scopedOpGroupYAML, "-n", opPolTestNS)
			check(
				opPolName,
				false,
				[]policyv1.RelatedObject{{
					Object: policyv1.ObjectResource{
						Kind:       "OperatorGroup",
						APIVersion: "operators.coreos.com/v1",
						Metadata: policyv1.ObjectMetadata{
							Name:      scopedOpGroupName,
							Namespace: opPolTestNS,
						},
					},
					Compliant: "Compliant",
					Reason:    "Resource found as expected",
				}},
				metav1.Condition{
					Type:    "OperatorGroupCompliant",
					Status:  metav1.ConditionTrue,
					Reason:  "OperatorGroupMatches",
					Message: "the OperatorGroup matches what is required by the policy",
				},
				"the OperatorGroup matches what is required by the policy",
			)
		})
		It("Should report a mismatch when the OperatorGroup is manually edited", func() {
			utils.Kubectl("patch", "operatorgroup", scopedOpGroupName, "-n", opPolTestNS, "--type=json", "-p",
				`[{"op": "replace", "path": "/spec/targetNamespaces", "value": []}]`)
			check(
				opPolName,
				true,
				[]policyv1.RelatedObject{{
					Object: policyv1.ObjectResource{
						Kind:       "OperatorGroup",
						APIVersion: "operators.coreos.com/v1",
						Metadata: policyv1.ObjectMetadata{
							Name:      scopedOpGroupName,
							Namespace: opPolTestNS,
						},
					},
					Compliant: "NonCompliant",
					Reason:    "Resource found but does not match",
				}},
				metav1.Condition{
					Type:    "OperatorGroupCompliant",
					Status:  metav1.ConditionFalse,
					Reason:  "OperatorGroupMismatch",
					Message: "the OperatorGroup found on the cluster does not match the policy",
				},
				"the OperatorGroup found on the cluster does not match the policy",
			)
		})
		It("Should update the OperatorGroup when it is changed to enforce", func() {
			utils.Kubectl("patch", "operatorpolicy", opPolName, "-n", opPolTestNS, "--type=json", "-p",
				`[{"op": "replace", "path": "/spec/remediationAction", "value": "enforce"}]`)
			check(
				opPolName,
				false,
				[]policyv1.RelatedObject{{
					Object: policyv1.ObjectResource{
						Kind:       "OperatorGroup",
						APIVersion: "operators.coreos.com/v1",
						Metadata: policyv1.ObjectMetadata{
							Name:      scopedOpGroupName,
							Namespace: opPolTestNS,
						},
					},
					Compliant: "Compliant",
					Reason:    "Resource found as expected",
				}},
				metav1.Condition{
					Type:    "OperatorGroupCompliant",
					Status:  metav1.ConditionTrue,
					Reason:  "OperatorGroupMatches",
					Message: "the OperatorGroup matches what is required by the policy",
				},
				"the OperatorGroup was updated to match the policy",
			)
		})
		It("Should become NonCompliant when an extra OperatorGroup is added", func() {
			utils.Kubectl("apply", "-f", extraOpGroupYAML, "-n", opPolTestNS)
			check(
				opPolName,
				true,
				[]policyv1.RelatedObject{{
					Object: policyv1.ObjectResource{
						Kind:       "OperatorGroup",
						APIVersion: "operators.coreos.com/v1",
					},
					Compliant: "NonCompliant",
					Reason:    "There is more than one OperatorGroup in this namespace",
				}, {
					Object: policyv1.ObjectResource{
						Kind:       "OperatorGroup",
						APIVersion: "operators.coreos.com/v1",
						Metadata: policyv1.ObjectMetadata{
							Name:      extraOpGroupName,
							Namespace: opPolTestNS,
						},
					},
					Compliant: "NonCompliant",
					Reason:    "There is more than one OperatorGroup in this namespace",
				}},
				metav1.Condition{
					Type:    "OperatorGroupCompliant",
					Status:  metav1.ConditionFalse,
					Reason:  "TooManyOperatorGroups",
					Message: "there is more than one OperatorGroup in the namespace",
				},
				"there is more than one OperatorGroup in the namespace",
			)
		})
	})
	Describe("Testing Subscription behavior for musthave mode while enforcing", Ordered, func() {
		const (
			opPolYAML = "../resources/case38_operator_install/operator-policy-no-group.yaml"
			opPolName = "oppol-no-group"
			subName   = "project-quay"
		)

		BeforeAll(func() {
			utils.Kubectl("create", "ns", opPolTestNS)
			DeferCleanup(func() {
				utils.Kubectl("delete", "ns", opPolTestNS)
			})

			createObjWithParent(parentPolicyYAML, parentPolicyName,
				opPolYAML, opPolTestNS, gvrPolicy, gvrOperatorPolicy)
		})

		It("Should initially be NonCompliant", func() {
			check(
				opPolName,
				true,
				[]policyv1.RelatedObject{{
					Object: policyv1.ObjectResource{
						Kind:       "Subscription",
						APIVersion: "operators.coreos.com/v1alpha1",
						Metadata: policyv1.ObjectMetadata{
							Name:      subName,
							Namespace: opPolTestNS,
						},
					},
					Compliant: "NonCompliant",
					Reason:    "Resource not found but should exist",
				}},
				metav1.Condition{
					Type:    "SubscriptionCompliant",
					Status:  metav1.ConditionFalse,
					Reason:  "SubscriptionMissing",
					Message: "the Subscription required by the policy was not found",
				},
				"the Subscription required by the policy was not found",
			)
		})
		It("Should create the Subscription when enforced", func() {
			utils.Kubectl("patch", "operatorpolicy", opPolName, "-n", opPolTestNS, "--type=json", "-p",
				`[{"op": "replace", "path": "/spec/remediationAction", "value": "enforce"}]`)
			check(
				opPolName,
				false,
				[]policyv1.RelatedObject{{
					Object: policyv1.ObjectResource{
						Kind:       "Subscription",
						APIVersion: "operators.coreos.com/v1alpha1",
						Metadata: policyv1.ObjectMetadata{
							Name:      subName,
							Namespace: opPolTestNS,
						},
					},
					Compliant: "Compliant",
					Reason:    "Resource found as expected",
				}},
				metav1.Condition{
					Type:    "SubscriptionCompliant",
					Status:  metav1.ConditionTrue,
					Reason:  "SubscriptionMatches",
					Message: "the Subscription matches what is required by the policy",
				},
				"the Subscription required by the policy was created",
			)
		})
		It("Should apply an update to the Subscription", func() {
			utils.Kubectl("patch", "operatorpolicy", opPolName, "-n", opPolTestNS, "--type=json", "-p",
				`[{"op": "replace", "path": "/spec/subscription/sourceNamespace", "value": "fake"}]`)
			check(
				opPolName,
				false,
				[]policyv1.RelatedObject{{
					Object: policyv1.ObjectResource{
						Kind:       "Subscription",
						APIVersion: "operators.coreos.com/v1alpha1",
						Metadata: policyv1.ObjectMetadata{
							Name:      subName,
							Namespace: opPolTestNS,
						},
					},
					Compliant: "Compliant",
					Reason:    "Resource found as expected",
				}},
				metav1.Condition{
					Type:    "SubscriptionCompliant",
					Status:  metav1.ConditionTrue,
					Reason:  "SubscriptionMatches",
					Message: "the Subscription matches what is required by the policy",
				},
				"the Subscription was updated to match the policy",
			)
		})
	})
	Describe("Testing Subscription behavior for musthave mode while informing", Ordered, func() {
		const (
			opPolYAML = "../resources/case38_operator_install/operator-policy-no-group.yaml"
			opPolName = "oppol-no-group"
			subName   = "project-quay"
			subYAML   = "../resources/case38_operator_install/subscription.yaml"
		)

		BeforeAll(func() {
			utils.Kubectl("create", "ns", opPolTestNS)
			DeferCleanup(func() {
				utils.Kubectl("delete", "ns", opPolTestNS)
			})

			utils.Kubectl("apply", "-f", subYAML, "-n", opPolTestNS)

			createObjWithParent(parentPolicyYAML, parentPolicyName,
				opPolYAML, opPolTestNS, gvrPolicy, gvrOperatorPolicy)
		})

		It("Should initially notice the matching Subscription", func() {
			check(
				opPolName,
				false,
				[]policyv1.RelatedObject{{
					Object: policyv1.ObjectResource{
						Kind:       "Subscription",
						APIVersion: "operators.coreos.com/v1alpha1",
						Metadata: policyv1.ObjectMetadata{
							Name:      subName,
							Namespace: opPolTestNS,
						},
					},
					Compliant: "Compliant",
					Reason:    "Resource found as expected",
				}},
				metav1.Condition{
					Type:    "SubscriptionCompliant",
					Status:  metav1.ConditionTrue,
					Reason:  "SubscriptionMatches",
					Message: "the Subscription matches what is required by the policy",
				},
				"the Subscription matches what is required by the policy",
			)
		})
		It("Should notice the mismatch when the spec is changed in the policy", func() {
			utils.Kubectl("patch", "operatorpolicy", opPolName, "-n", opPolTestNS, "--type=json", "-p",
				`[{"op": "replace", "path": "/spec/subscription/sourceNamespace", "value": "fake"}]`)
			check(
				opPolName,
				true,
				[]policyv1.RelatedObject{{
					Object: policyv1.ObjectResource{
						Kind:       "Subscription",
						APIVersion: "operators.coreos.com/v1alpha1",
						Metadata: policyv1.ObjectMetadata{
							Name:      subName,
							Namespace: opPolTestNS,
						},
					},
					Compliant: "NonCompliant",
					Reason:    "Resource found but does not match",
				}},
				metav1.Condition{
					Type:    "SubscriptionCompliant",
					Status:  metav1.ConditionFalse,
					Reason:  "SubscriptionMismatch",
					Message: "the Subscription found on the cluster does not match the policy",
				},
				"the Subscription found on the cluster does not match the policy",
			)
		})
	})

	Describe("Test health checks on OLM resources after OperatorPolicy operator installation", Ordered, func() {
		const (
			opPolYAML = "../resources/case38_operator_install/operator-policy-no-group-enforce.yaml"
			opPolName = "oppol-no-group-enforce"
		)
		BeforeAll(func() {
			utils.Kubectl("create", "ns", opPolTestNS)
			DeferCleanup(func() {
				utils.Kubectl("delete", "ns", opPolTestNS)
			})

			createObjWithParent(parentPolicyYAML, parentPolicyName,
				opPolYAML, opPolTestNS, gvrPolicy, gvrOperatorPolicy)
		})

		It("Should generate conditions and relatedobjects of CSV", func() {
			check(
				opPolName,
				false,
				[]policyv1.RelatedObject{{
					Object: policyv1.ObjectResource{
						Kind:       "ClusterServiceVersion",
						APIVersion: "operators.coreos.com/v1alpha1",
					},
					Compliant: "Compliant",
					Reason:    "InstallSucceeded",
				}},
				metav1.Condition{
					Type:    "ClusterServiceVersionCompliant",
					Status:  metav1.ConditionTrue,
					Reason:  "InstallSucceeded",
					Message: "ClusterServiceVersion - install strategy completed with no errors",
				},
				"ClusterServiceVersion - install strategy completed with no errors",
			)
		})

		It("Should generate conditions and relatedobjects of Deployments", func() {
			check(
				opPolName,
				false,
				[]policyv1.RelatedObject{{
					Object: policyv1.ObjectResource{
						Kind:       "Deployment",
						APIVersion: "apps/v1",
					},
					Compliant: "Compliant",
					Reason:    "Deployment Available",
				}},
				metav1.Condition{
					Type:    "DeploymentCompliant",
					Status:  metav1.ConditionTrue,
					Reason:  "DeploymentsAvailable",
					Message: "All operator Deployments have their minimum availability",
				},
				"All operator Deployments have their minimum availability",
			)
		})
	})

	Describe("Test health checks on OLM resources on OperatorPolicy with failed CSV", Ordered, func() {
		const (
			opPolYAML = "../resources/case38_operator_install/operator-policy-no-group-csv-fail.yaml"
			opPolName = "oppol-no-allnamespaces"
		)
		BeforeAll(func() {
			utils.Kubectl("create", "ns", opPolTestNS)
			DeferCleanup(func() {
				utils.Kubectl("delete", "ns", opPolTestNS)
			})

			createObjWithParent(parentPolicyYAML, parentPolicyName,
				opPolYAML, opPolTestNS, gvrPolicy, gvrOperatorPolicy)
		})

		It("Should generate conditions and relatedobjects of CSV", func() {
			check(
				opPolName,
				false,
				[]policyv1.RelatedObject{{
					Object: policyv1.ObjectResource{
						Kind:       "ClusterServiceVersion",
						APIVersion: "operators.coreos.com/v1alpha1",
					},
					Compliant: "NonCompliant",
					Reason:    "UnsupportedOperatorGroup",
				}},
				metav1.Condition{
					Type:   "ClusterServiceVersionCompliant",
					Status: metav1.ConditionFalse,
					Reason: "UnsupportedOperatorGroup",
					Message: "ClusterServiceVersion - AllNamespaces InstallModeType not supported," +
						" cannot configure to watch all namespaces",
				},
				"ClusterServiceVersion - AllNamespaces InstallModeType not supported,"+
					" cannot configure to watch all namespaces",
			)
		})

		It("Should generate conditions and relatedobjects of Deployments", func() {
			check(
				opPolName,
				false,
				[]policyv1.RelatedObject{{
					Object: policyv1.ObjectResource{
						Kind:       "Deployment",
						APIVersion: "apps/v1",
					},
					Compliant: "NonCompliant",
					Reason:    "Resource not found but should exist",
				}},
				metav1.Condition{
					Type:    "DeploymentCompliant",
					Status:  metav1.ConditionTrue,
					Reason:  "NoExistingDeployments",
					Message: "No existing operator Deployments",
				},
				"No existing operator Deployments",
			)
		})
	})
})
