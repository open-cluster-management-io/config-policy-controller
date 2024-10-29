package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	policyv1 "open-cluster-management.io/config-policy-controller/api/v1"
	policyv1beta1 "open-cluster-management.io/config-policy-controller/api/v1beta1"
	"open-cluster-management.io/config-policy-controller/pkg/common"
	"open-cluster-management.io/config-policy-controller/test/utils"
)

var _ = Describe("Testing OperatorPolicy", Ordered, Label("supports-hosted"), func() {
	const (
		opPolTestNS          = "operator-policy-testns"
		parentPolicyYAML     = "../resources/case38_operator_install/parent-policy.yaml"
		parentPolicyName     = "parent-policy"
		eventuallyTimeout    = 60
		consistentlyDuration = 5
		olmWaitTimeout       = 60
	)

	// checks that the compliance state eventually matches what is desired
	checkCompliance := func(
		polName string,
		ns string,
		timeoutSeconds int,
		comp policyv1.ComplianceState,
		consistencyArgs ...interface{},
	) {
		GinkgoHelper()

		var debugMessage string

		DeferCleanup(func() {
			if CurrentSpecReport().Failed() {
				GinkgoWriter.Println(debugMessage)
			}
		})

		compCheck := func(g Gomega) {
			GinkgoHelper()

			unstructPolicy, err := clientManagedDynamic.Resource(gvrOperatorPolicy).Namespace(ns).
				Get(context.TODO(), polName, metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred())

			unstructured.RemoveNestedField(unstructPolicy.Object, "metadata", "managedFields")

			policyJSON, err := json.MarshalIndent(unstructPolicy.Object, "", "  ")
			g.Expect(err).NotTo(HaveOccurred())

			debugMessage = fmt.Sprintf("Debug info for failure.\npolicy JSON: %s", policyJSON)

			policy := policyv1beta1.OperatorPolicy{}
			err = json.Unmarshal(policyJSON, &policy)
			g.Expect(err).NotTo(HaveOccurred())

			compliantMsg := ""
			_, compliantCond := policy.Status.GetCondition("Compliant")
			compliantMsg = compliantCond.Message

			g.Expect(policy.Status.ComplianceState).To(
				Equal(comp),
				fmt.Sprintf(
					"Unexpected compliance state. Status message: %s", strings.ReplaceAll(compliantMsg, ", ", "\n- "),
				))
		}

		Eventually(compCheck, timeoutSeconds, 3).Should(Succeed())

		if len(consistencyArgs) > 0 {
			Consistently(compCheck, consistencyArgs...).Should(Succeed())
		}
	}

	// checks that the policy has the proper compliance, that the relatedObjects of a given
	// type exactly match the list given (no extras or omissions), that the condition is present,
	// and that an event was emitted that matches the given snippet.
	// It initially checks these in an Eventually, so they don't have to be true yet,
	// but it follows up with a Consistently, so they do need to be the "final" state.
	check := func(
		polName string,
		wantNonCompliant bool,
		expectedRelatedObjs []policyv1.RelatedObject,
		expectedCondition metav1.Condition,
		expectedEventMsgSnippet string,
	) {
		GinkgoHelper()

		var debugMessage string

		DeferCleanup(func() {
			if CurrentSpecReport().Failed() {
				GinkgoWriter.Println(debugMessage)
			}
		})

		checkFunc := func(g Gomega) {
			GinkgoHelper()

			unstructPolicy := utils.GetWithTimeout(clientManagedDynamic, gvrOperatorPolicy, polName,
				testNamespace, true, eventuallyTimeout)

			unstructured.RemoveNestedField(unstructPolicy.Object, "metadata", "managedFields")

			policyJSON, err := json.MarshalIndent(unstructPolicy.Object, "", "  ")
			g.Expect(err).NotTo(HaveOccurred())

			debugMessage = fmt.Sprintf("Debug info for failure.\npolicy JSON: %s\nwanted related objects: %+v\n"+
				"wanted condition: %+v\n", string(policyJSON), expectedRelatedObjs, expectedCondition)

			policy := policyv1beta1.OperatorPolicy{}
			err = json.Unmarshal(policyJSON, &policy)
			g.Expect(err).NotTo(HaveOccurred())

			g.Expect(policy.Status.ObservedGeneration).To(Equal(unstructPolicy.GetGeneration()))

			if wantNonCompliant {
				g.Expect(policy.Status.ComplianceState).To(Equal(policyv1.NonCompliant))
			}

			if len(expectedRelatedObjs) != 0 {
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

					g.Expect(foundMatchingName).To(BeTrue(), fmt.Sprintf(
						"Should have related object %s with name '%s'",
						expectedRelObj.Object.Kind, expectedRelObj.Object.Metadata.Name))
				}
			}

			idx, actualCondition := policy.Status.GetCondition(expectedCondition.Type)

			g.Expect(idx).NotTo(Equal(-1))
			g.Expect(actualCondition.Status).To(Equal(expectedCondition.Status))
			g.Expect(actualCondition.Reason).To(Equal(expectedCondition.Reason))

			const cnfPrefix = "constraints not satisfiable: "
			if strings.Contains(actualCondition.Message, cnfPrefix) &&
				strings.Contains(expectedCondition.Message, cnfPrefix) {
				// need to sort message before checking

				expectedMessage := strings.TrimPrefix(expectedCondition.Message, cnfPrefix)
				actualMessage := strings.TrimPrefix(actualCondition.Message, cnfPrefix)

				expectedMessageSlice := strings.Split(expectedMessage, ", ")
				slices.Sort(expectedMessageSlice)

				actualMessageSlice := strings.Split(actualMessage, ", ")
				slices.Sort(actualMessageSlice)

				g.Expect(reflect.DeepEqual(expectedMessageSlice, actualMessageSlice)).To(BeTrue())
			} else {
				g.Expect(actualCondition.Message).To(MatchRegexp(
					fmt.Sprintf(".*%v.*", regexp.QuoteMeta(expectedCondition.Message))))
			}

			events := utils.GetMatchingEvents(
				clientManaged, testNamespace, parentPolicyName, "", expectedEventMsgSnippet, eventuallyTimeout,
			)
			g.Expect(events).NotTo(BeEmpty())

			for _, event := range events {
				g.Expect(event.Annotations[common.ParentDBIDAnnotation]).To(
					Equal("124"), common.ParentDBIDAnnotation+" should have the correct value",
				)
				g.Expect(event.Annotations[common.PolicyDBIDAnnotation]).To(
					Equal("64"), common.PolicyDBIDAnnotation+" should have the correct value",
				)
			}
		}

		Eventually(checkFunc, eventuallyTimeout, 3).Should(Succeed())
		Consistently(checkFunc, consistentlyDuration, 1).Should(Succeed())
	}

	preFunc := func() {
		utils.Kubectl("create", "ns", opPolTestNS)
		utils.KubectlDelete(
			"event", "--field-selector=involvedObject.name="+parentPolicyName, "-n", testNamespace, "--wait",
		)

		if IsHosted {
			KubectlTarget("create", "ns", opPolTestNS)
		}

		DeferCleanup(func() {
			utils.KubectlDelete("operatorpolicy", "-n", testNamespace, "--all", "--wait")
			utils.KubectlDelete(
				"event", "--field-selector=involvedObject.name="+parentPolicyName, "-n", testNamespace, "--wait",
			)
			utils.KubectlDelete("ns", opPolTestNS, "--wait")

			if IsHosted {
				KubectlTarget("delete", "ns", opPolTestNS, "--ignore-not-found")
			}
		})
	}

	Describe("Testing an all default operator policy", Ordered, func() {
		const (
			opPolYAML   = "../resources/case38_operator_install/operator-policy-all-defaults.yaml"
			opPolName   = "oppol-all-defaults"
			subName     = "airflow-helm-operator"
			suggestedNS = "airflow-helm"
		)
		BeforeAll(func() {
			DeferCleanup(func() {
				utils.KubectlDelete("ns", suggestedNS, "--wait")

				if IsHosted {
					KubectlTarget("delete", "ns", suggestedNS, "--ignore-not-found")
				}
			})

			preFunc()

			createObjWithParent(parentPolicyYAML, parentPolicyName,
				opPolYAML, testNamespace, gvrPolicy, gvrOperatorPolicy)
		})

		It("Should create the Subscription with default values", func(ctx context.Context) {
			By("Verifying the policy is compliant")
			check(
				opPolName,
				false,
				[]policyv1.RelatedObject{{
					Object: policyv1.ObjectResource{
						Kind:       "Subscription",
						APIVersion: "operators.coreos.com/v1alpha1",
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

			By("Verifying the subscription has the correct defaults")
			sub, err := targetK8sDynamic.Resource(gvrSubscription).Namespace(suggestedNS).
				Get(ctx, subName, metav1.GetOptions{})
			Expect(err).ToNot(HaveOccurred())

			channel, _, _ := unstructured.NestedString(sub.Object, "spec", "channel")
			// This default will change so just check something was set.
			Expect(channel).ToNot(BeEmpty())

			source, _, _ := unstructured.NestedString(sub.Object, "spec", "source")
			Expect(source).To(Equal("operatorhubio-catalog"))

			sourceNamespace, _, _ := unstructured.NestedString(sub.Object, "spec", "sourceNamespace")
			Expect(sourceNamespace).To(Equal("olm"))
		})
	})

	Describe("Testing an operator policy with invalid partial defaults", Ordered, func() {
		const (
			opPolYAML = "../resources/case38_operator_install/operator-policy-defaults-invalid-source.yaml"
			opPolName = "oppol-defaults-invalid-source"
			subName   = "project-quay"
		)
		BeforeAll(func() {
			preFunc()
			createObjWithParent(parentPolicyYAML, parentPolicyName,
				opPolYAML, testNamespace, gvrPolicy, gvrOperatorPolicy)
		})

		It("Should be NonCompliant specifying a source not matching the PackageManifest", func(ctx context.Context) {
			By("Verifying the policy is noncompliant due to the invalid source")
			Eventually(func(g Gomega) {
				pol := utils.GetWithTimeout(clientManagedDynamic, gvrOperatorPolicy, opPolName,
					testNamespace, true, eventuallyTimeout)
				compliance, found, err := unstructured.NestedString(pol.Object, "status", "compliant")
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(found).To(BeTrue())

				g.Expect(compliance).To(Equal("NonCompliant"))

				expectedMsg := "the subscription defaults could not be determined because the catalog specified in " +
					"the policy does not match what was found in the PackageManifest on the cluster"
				events := utils.GetMatchingEvents(
					clientManaged, testNamespace, parentPolicyName, "", expectedMsg, eventuallyTimeout,
				)
				g.Expect(events).NotTo(BeEmpty())
			}, defaultTimeoutSeconds, 5, ctx).Should(Succeed())
		})
	})

	Describe("Testing OperatorGroup behavior when it is not specified in the policy", Ordered, func() {
		const (
			opPolYAML        = "../resources/case38_operator_install/operator-policy-no-group.yaml"
			opPolName        = "oppol-no-group"
			extraOpGroupYAML = "../resources/case38_operator_install/extra-operator-group.yaml"
			extraOpGroupName = "extra-operator-group"
		)
		BeforeAll(func() {
			preFunc()

			createObjWithParent(parentPolicyYAML, parentPolicyName,
				opPolYAML, testNamespace, gvrPolicy, gvrOperatorPolicy)
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
							Name:      "-",
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
			utils.Kubectl("patch", "operatorpolicy", opPolName, "-n", testNamespace, "--type=json", "-p",
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
			KubectlTarget("apply", "-f", extraOpGroupYAML, "-n", opPolTestNS)
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
			utils.Kubectl("patch", "operatorpolicy", opPolName, "-n", testNamespace, "--type=json", "-p",
				`[{"op": "replace", "path": "/spec/remediationAction", "value": "inform"}]`)
			KubectlTarget("delete", "operatorgroup", "-n", opPolTestNS, "--all")
			KubectlTarget("apply", "-f", extraOpGroupYAML, "-n", opPolTestNS)
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

	Describe("Testing namespace creation", Ordered, func() {
		const (
			opPolYAML = "../resources/case38_operator_install/operator-policy-no-group-enforce.yaml"
			opPolName = "oppol-no-group-enforce"
		)
		BeforeAll(func() {
			DeferCleanup(func() {
				utils.KubectlDelete(
					"-f", parentPolicyYAML, "-n", testNamespace, "--cascade=foreground", "--wait",
				)
				if IsHosted {
					KubectlTarget("delete", "ns", opPolTestNS, "--ignore-not-found")
				}
				utils.KubectlDelete("ns", opPolTestNS, "--wait")
			})

			createObjWithParent(
				parentPolicyYAML, parentPolicyName, opPolYAML, testNamespace, gvrPolicy, gvrOperatorPolicy,
			)
		})

		It("Should be compliant when enforced", func() {
			By("Waiting for the operator policy " + opPolName + " to be compliant")
			// Wait for a while, because it might have upgrades that could take longer
			checkCompliance(opPolName, testNamespace, olmWaitTimeout*2, policyv1.Compliant)
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
			preFunc()

			KubectlTarget("apply", "-f", incorrectOpGroupYAML, "-n", opPolTestNS)

			createObjWithParent(parentPolicyYAML, parentPolicyName,
				opPolYAML, testNamespace, gvrPolicy, gvrOperatorPolicy)
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
			KubectlTarget("delete", "operatorgroup", incorrectOpGroupName, "-n", opPolTestNS)
			KubectlTarget("apply", "-f", scopedOpGroupYAML, "-n", opPolTestNS)
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
			KubectlTarget("patch", "operatorgroup", scopedOpGroupName, "-n", opPolTestNS, "--type=json", "-p",
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
			utils.Kubectl("patch", "operatorpolicy", opPolName, "-n", testNamespace, "--type=json", "-p",
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
			KubectlTarget("apply", "-f", extraOpGroupYAML, "-n", opPolTestNS)
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
			opPolYAML        = "../resources/case38_operator_install/operator-policy-with-group.yaml"
			opPolName        = "oppol-with-group"
			subName          = "project-quay"
			extraOpGroupYAML = "../resources/case38_operator_install/extra-operator-group.yaml"
			extraOpGroupName = "extra-operator-group"
		)

		BeforeAll(func() {
			preFunc()

			KubectlTarget("apply", "-f", extraOpGroupYAML, "-n", opPolTestNS)

			createObjWithParent(parentPolicyYAML, parentPolicyName,
				opPolYAML, testNamespace, gvrPolicy, gvrOperatorPolicy)
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
		It("Should not create the Subscription when another OperatorGroup already exists", func() {
			utils.Kubectl("patch", "operatorpolicy", opPolName, "-n", testNamespace, "--type=json", "-p",
				`[{"op": "replace", "path": "/spec/remediationAction", "value": "enforce"}]`)
			check(
				opPolName,
				true,
				[]policyv1.RelatedObject{{
					Object: policyv1.ObjectResource{
						Kind:       "OperatorGroup",
						APIVersion: "operators.coreos.com/v1",
						Metadata: policyv1.ObjectMetadata{
							Name:      extraOpGroupName,
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
							Name:      "scoped-operator-group",
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

		It("Should create the Subscription after the additional OperatorGroup is removed", func() {
			KubectlTarget("delete", "operatorgroup", extraOpGroupName, "-n", opPolTestNS)
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
			utils.Kubectl("patch", "operatorpolicy", opPolName, "-n", testNamespace, "--type=json", "-p",
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
					Compliant: "NonCompliant",
					Reason:    "ConstraintsNotSatisfiable",
				}},
				metav1.Condition{
					Type:    "SubscriptionCompliant",
					Status:  metav1.ConditionFalse,
					Reason:  "ConstraintsNotSatisfiable",
					Message: "constraints not satisfiable: refer to the Subscription for more details",
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
			preFunc()

			KubectlTarget("apply", "-f", subYAML, "-n", opPolTestNS)

			createObjWithParent(parentPolicyYAML, parentPolicyName,
				opPolYAML, testNamespace, gvrPolicy, gvrOperatorPolicy)
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
			utils.Kubectl("patch", "operatorpolicy", opPolName, "-n", testNamespace, "--type=json", "-p",
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
			opPolYAML        = "../resources/case38_operator_install/operator-policy-no-group-enforce-one-version.yaml"
			opPolName        = "oppol-no-group-enforce-one-version"
			opPolNoExistYAML = "../resources/case38_operator_install/operator-policy-no-exist-enforce.yaml"
			opPolNoExistName = "oppol-no-exist-enforce"
		)
		BeforeAll(func() {
			preFunc()

			createObjWithParent(parentPolicyYAML, parentPolicyName,
				opPolYAML, testNamespace, gvrPolicy, gvrOperatorPolicy)

			createObjWithParent(parentPolicyYAML, parentPolicyName,
				opPolNoExistYAML, testNamespace, gvrPolicy, gvrOperatorPolicy)
		})

		It("Should generate conditions and relatedobjects of CSV", func(ctx SpecContext) {
			Eventually(func(ctx SpecContext) string {
				csv, _ := targetK8sDynamic.Resource(gvrClusterServiceVersion).Namespace(opPolTestNS).
					Get(ctx, "quay-operator.v3.10.0", metav1.GetOptions{})

				if csv == nil {
					return ""
				}

				reason, _, _ := unstructured.NestedString(csv.Object, "status", "reason")

				return reason
			}, olmWaitTimeout, 5, ctx).Should(Equal("InstallSucceeded"))

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
					Type:   "ClusterServiceVersionCompliant",
					Status: metav1.ConditionTrue,
					Reason: "InstallSucceeded",
					Message: "ClusterServiceVersion (quay-operator.v3.10.0) - install strategy completed with " +
						"no errors",
				},
				regexp.QuoteMeta(
					"ClusterServiceVersion (quay-operator.v3.10.0) - install strategy completed with no errors",
				),
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
					Message: "all operator Deployments have their minimum availability",
				},
				"all operator Deployments have their minimum availability",
			)
		})

		It("Should only be noncompliant if the subscription error relates to the one in the operator policy", func() {
			By("Checking that " + opPolNoExistName + " is NonCompliant")
			check(
				opPolNoExistName,
				true,
				[]policyv1.RelatedObject{{
					Object: policyv1.ObjectResource{
						Kind:       "Subscription",
						APIVersion: "operators.coreos.com/v1alpha1",
					},
					Compliant: "NonCompliant",
					Reason:    "ConstraintsNotSatisfiable",
				}},
				metav1.Condition{
					Type:    "SubscriptionCompliant",
					Status:  metav1.ConditionFalse,
					Reason:  "ConstraintsNotSatisfiable",
					Message: "constraints not satisfiable: refer to the Subscription for more details",
				},
				"constraints not satisfiable",
			)

			// Check if the subscription is still compliant on the operator policy trying to install a valid operator.
			// This tests that subscription status filtering is working properly since OLM includes the
			// subscription errors as a condition on all subscriptions in the namespace.
			By("Checking that " + opPolName + " is still Compliant and unaffected by " + opPolNoExistName)
			check(
				opPolName,
				false,
				[]policyv1.RelatedObject{{
					Object: policyv1.ObjectResource{
						Kind:       "Subscription",
						APIVersion: "operators.coreos.com/v1alpha1",
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
	})
	Describe("Test health checks on OLM resources on OperatorPolicy with failed CSV", Ordered, func() {
		const (
			opPolYAML = "../resources/case38_operator_install/operator-policy-no-group-csv-fail.yaml"
			opPolName = "oppol-no-allnamespaces"
		)
		BeforeAll(func() {
			preFunc()

			createObjWithParent(parentPolicyYAML, parentPolicyName,
				opPolYAML, testNamespace, gvrPolicy, gvrOperatorPolicy)
		})

		It("Should generate conditions and relatedobjects of CSV", func(ctx SpecContext) {
			Eventually(func(ctx SpecContext) []unstructured.Unstructured {
				csvList, _ := targetK8sDynamic.Resource(gvrClusterServiceVersion).Namespace(opPolTestNS).
					List(ctx, metav1.ListOptions{})

				return csvList.Items
			}, olmWaitTimeout, 5, ctx).ShouldNot(BeEmpty())

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
					Type:    "ClusterServiceVersionCompliant",
					Status:  metav1.ConditionFalse,
					Reason:  "UnsupportedOperatorGroup",
					Message: "AllNamespaces InstallModeType not supported",
				},
				"AllNamespaces InstallModeType not supported",
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
					Message: "no existing operator Deployments",
				},
				"no existing operator Deployments",
			)
		})

		It("Should not have any compliant events", func() {
			// This test is meant to find an incorrect compliant event that is emitted between some
			// correct noncompliant events.
			events := utils.GetMatchingEvents(
				clientManaged, testNamespace, parentPolicyName, "", "^Compliant;", eventuallyTimeout,
			)

			Expect(events).To(BeEmpty())
		})
	})
	Describe("Test status reporting for CatalogSource", Ordered, func() {
		const (
			OpPlcYAML  = "../resources/case38_operator_install/operator-policy-with-group.yaml"
			OpPlcName  = "oppol-with-group"
			subName    = "project-quay"
			catSrcName = "operatorhubio-catalog"
		)

		BeforeAll(func() {
			By("Applying creating a ns and the test policy")
			preFunc()
			DeferCleanup(func() {
				KubectlTarget("patch", "catalogsource", catSrcName, "-n", "olm", "--type=json", "-p",
					`[{"op": "replace", "path": "/spec/image", "value": "quay.io/operatorhubio/catalog:latest"}]`)
			})

			createObjWithParent(parentPolicyYAML, parentPolicyName,
				OpPlcYAML, testNamespace, gvrPolicy, gvrOperatorPolicy)
		})

		It("Should initially show the CatalogSource is compliant", func() {
			By("Checking the condition fields")
			check(
				OpPlcName,
				false,
				[]policyv1.RelatedObject{{
					Object: policyv1.ObjectResource{
						Kind:       "CatalogSource",
						APIVersion: "operators.coreos.com/v1alpha1",
						Metadata: policyv1.ObjectMetadata{
							Name:      catSrcName,
							Namespace: opPolTestNS,
						},
					},
					Compliant: "Compliant",
					Reason:    "Resource found as expected",
				}},
				metav1.Condition{
					Type:    "CatalogSourcesUnhealthy",
					Status:  metav1.ConditionFalse,
					Reason:  "CatalogSourcesFound",
					Message: "CatalogSource was found",
				},
				"CatalogSource was found",
			)
		})
		It("Should remain compliant when policy is enforced", func() {
			By("Enforcing the policy")
			utils.Kubectl("patch", "operatorpolicy", OpPlcName, "-n", testNamespace, "--type=json", "-p",
				`[{"op": "replace", "path": "/spec/remediationAction", "value": "enforce"}]`)

			By("Checking the condition fields")
			check(
				OpPlcName,
				false,
				[]policyv1.RelatedObject{{
					Object: policyv1.ObjectResource{
						Kind:       "CatalogSource",
						APIVersion: "operators.coreos.com/v1alpha1",
						Metadata: policyv1.ObjectMetadata{
							Name:      catSrcName,
							Namespace: opPolTestNS,
						},
					},
					Compliant: "Compliant",
					Reason:    "Resource found as expected",
				}},
				metav1.Condition{
					Type:    "CatalogSourcesUnhealthy",
					Status:  metav1.ConditionFalse,
					Reason:  "CatalogSourcesFound",
					Message: "CatalogSource was found",
				},
				"CatalogSource was found",
			)
		})
		It("Should report in status when CatalogSource DNE", func() {
			By("Patching the policy to reference a CatalogSource that DNE to emulate failure")
			utils.Kubectl("patch", "operatorpolicy", OpPlcName, "-n", testNamespace, "--type=json", "-p",
				`[{"op": "replace", "path": "/spec/subscription/source", "value": "fakeName"}]`)

			By("Checking the conditions and relatedObj in the policy")
			check(
				OpPlcName,
				false,
				[]policyv1.RelatedObject{{
					Object: policyv1.ObjectResource{
						Kind:       "CatalogSource",
						APIVersion: "operators.coreos.com/v1alpha1",
						Metadata: policyv1.ObjectMetadata{
							Name:      "fakeName",
							Namespace: opPolTestNS,
						},
					},
					Compliant: "Compliant",
					Reason:    "Resource not found but should exist",
				}},
				metav1.Condition{
					Type:    "CatalogSourcesUnhealthy",
					Status:  metav1.ConditionFalse,
					Reason:  "CatalogSourcesNotFound",
					Message: "CatalogSource 'fakeName' was not found",
				},
				"CatalogSource 'fakeName' was not found",
			)
		})
		It("Should report unhealthy status when CatalogSource fails", func() {
			By("Patching the policy to point to an existing CatalogSource")
			utils.Kubectl("patch", "operatorpolicy", OpPlcName, "-n", testNamespace, "--type=json", "-p",
				`[{"op": "replace", "path": "/spec/subscription/source", "value": "operatorhubio-catalog"}]`)

			By("Patching the CatalogSource to reference a broken image link")
			KubectlTarget("patch", "catalogsource", catSrcName, "-n", "olm", "--type=json", "-p",
				`[{"op": "replace", "path": "/spec/image", "value": "quay.io/operatorhubio/fakecatalog:latest"}]`)

			By("Checking the conditions and relatedObj in the policy")
			check(
				OpPlcName,
				false,
				[]policyv1.RelatedObject{{
					Object: policyv1.ObjectResource{
						Kind:       "CatalogSource",
						APIVersion: "operators.coreos.com/v1alpha1",
						Metadata: policyv1.ObjectMetadata{
							Name:      catSrcName,
							Namespace: opPolTestNS,
						},
					},
					Compliant: "Compliant",
					Reason:    "Resource found as expected but is unhealthy",
				}},
				metav1.Condition{
					Type:    "CatalogSourcesUnhealthy",
					Status:  metav1.ConditionFalse,
					Reason:  "CatalogSourcesFoundUnhealthy",
					Message: "CatalogSource was found but is unhealthy",
				},
				"CatalogSource was found but is unhealthy",
			)
		})
		It("Should become NonCompliant when ComplianceConfig is modified", func() {
			By("Patching the policy ComplianceConfig to NonCompliant")
			utils.Kubectl("patch", "operatorpolicy", OpPlcName, "-n", testNamespace, "--type=json", "-p",
				`[{"op": "replace", "path": "/spec/complianceConfig/catalogSourceUnhealthy", "value": "NonCompliant"}]`)

			By("Checking the conditions and relatedObj in the policy")
			check(
				OpPlcName,
				true,
				[]policyv1.RelatedObject{{
					Object: policyv1.ObjectResource{
						Kind:       "CatalogSource",
						APIVersion: "operators.coreos.com/v1alpha1",
						Metadata: policyv1.ObjectMetadata{
							Name:      catSrcName,
							Namespace: opPolTestNS,
						},
					},
					Compliant: "NonCompliant",
					Reason:    "Resource found as expected but is unhealthy",
				}},
				metav1.Condition{
					Type:    "CatalogSourcesUnhealthy",
					Status:  metav1.ConditionTrue,
					Reason:  "CatalogSourcesFoundUnhealthy",
					Message: "CatalogSource was found but is unhealthy",
				},
				"CatalogSource was found but is unhealthy",
			)
		})
	})
	Describe("Testing InstallPlan approval and status behavior", Ordered, func() {
		const (
			opPolYAML = "../resources/case38_operator_install/operator-policy-manual-upgrades.yaml"
			opPolName = "oppol-manual-upgrades"
			subName   = "strimzi-kafka-operator"
		)

		var (
			firstInstallPlanName  string
			secondInstallPlanName string
		)

		BeforeAll(func() {
			preFunc()

			createObjWithParent(parentPolicyYAML, parentPolicyName,
				opPolYAML, testNamespace, gvrPolicy, gvrOperatorPolicy)
		})

		It("Should initially report the ConstraintsNotSatisfiable Subscription", func(ctx SpecContext) {
			Eventually(func(ctx SpecContext) interface{} {
				sub, _ := targetK8sDynamic.Resource(gvrSubscription).Namespace(opPolTestNS).
					Get(ctx, subName, metav1.GetOptions{})

				if sub == nil {
					return ""
				}

				conditions, _, _ := unstructured.NestedSlice(sub.Object, "status", "conditions")
				for _, cond := range conditions {
					condMap, ok := cond.(map[string]interface{})
					if !ok {
						continue
					}

					condType, _, _ := unstructured.NestedString(condMap, "type")
					if condType == "ResolutionFailed" {
						return condMap["reason"]
					}
				}

				return nil
			}, olmWaitTimeout*2, 5, ctx).Should(Equal("ConstraintsNotSatisfiable"))

			check(
				opPolName,
				true,
				[]policyv1.RelatedObject{{
					Object: policyv1.ObjectResource{
						Kind:       "Subscription",
						APIVersion: "operators.coreos.com/v1alpha1",
						Metadata: policyv1.ObjectMetadata{
							Namespace: opPolTestNS,
							Name:      subName,
						},
					},
					Compliant: "NonCompliant",
					Reason:    "ConstraintsNotSatisfiable",
				}},
				metav1.Condition{
					Type:    "SubscriptionCompliant",
					Status:  metav1.ConditionFalse,
					Reason:  "ConstraintsNotSatisfiable",
					Message: "constraints not satisfiable: refer to the Subscription for more details",
				},
				"constraints not satisfiable",
			)
		})
		It("Should initially report that no InstallPlans are found", func() {
			check(
				opPolName,
				true,
				[]policyv1.RelatedObject{{
					Object: policyv1.ObjectResource{
						Kind:       "InstallPlan",
						APIVersion: "operators.coreos.com/v1alpha1",
						Metadata: policyv1.ObjectMetadata{
							Namespace: opPolTestNS,
							Name:      "-",
						},
					},
					Compliant: "Compliant",
					Reason:    "There are no relevant InstallPlans in this namespace",
				}},
				metav1.Condition{
					Type:    "InstallPlanCompliant",
					Status:  metav1.ConditionTrue,
					Reason:  "NoInstallPlansFound",
					Message: "there are no relevant InstallPlans in the namespace",
				},
				"there are no relevant InstallPlans in the namespace",
			)
		})
		It("Should report an available install when informing", func(ctx SpecContext) {
			goodVersion := "strimzi-cluster-operator.v0.36.0"
			utils.Kubectl("patch", "operatorpolicy", opPolName, "-n", testNamespace, "--type=json", "-p",
				`[{"op": "replace", "path": "/spec/subscription/startingCSV", "value": "`+goodVersion+`"},`+
					`{"op": "replace", "path": "/spec/remediationAction", "value": "inform"}]`)
			KubectlTarget("patch", "subscription.operator", subName, "-n", opPolTestNS, "--type=json", "-p",
				`[{"op": "replace", "path": "/spec/startingCSV", "value": "`+goodVersion+`"}]`)
			Eventually(func(ctx SpecContext) int {
				ipList, _ := targetK8sDynamic.Resource(gvrInstallPlan).Namespace(opPolTestNS).
					List(ctx, metav1.ListOptions{})

				return len(ipList.Items)
			}, olmWaitTimeout, 5, ctx).Should(Equal(1))
			check(
				opPolName,
				false,
				[]policyv1.RelatedObject{{
					Object: policyv1.ObjectResource{
						Kind:       "InstallPlan",
						APIVersion: "operators.coreos.com/v1alpha1",
						Metadata: policyv1.ObjectMetadata{
							Namespace: opPolTestNS,
						},
					},
					Compliant: "Compliant",
					Reason:    "The InstallPlan is RequiresApproval",
				}},
				metav1.Condition{
					Type:   "InstallPlanCompliant",
					Status: metav1.ConditionTrue,
					Reason: "InstallPlanRequiresApproval",
					Message: "an InstallPlan to update to [strimzi-cluster-operator.v0.36.0] is available" +
						" for approval",
				},
				"an InstallPlan to update .* is available for approval",
			)
		})
		It("Should become NonCompliant when ComplianceConfig is modified to NonCompliant", func(ctx SpecContext) {
			By("Patching the policy ComplianceConfig to Compliant")
			utils.Kubectl("patch", "operatorpolicy", opPolName, "-n", testNamespace, "--type=json", "-p",
				`[{"op": "replace", "path": "/spec/complianceConfig/upgradesAvailable", "value": "NonCompliant"}]`)

			By("Checking the conditions and relatedObj in the policy")
			check(
				opPolName,
				true,
				[]policyv1.RelatedObject{{
					Object: policyv1.ObjectResource{
						Kind:       "InstallPlan",
						APIVersion: "operators.coreos.com/v1alpha1",
						Metadata: policyv1.ObjectMetadata{
							Namespace: opPolTestNS,
						},
					},
					Compliant: "NonCompliant",
					Reason:    "The InstallPlan is RequiresApproval",
				}},
				metav1.Condition{
					Type:    "InstallPlanCompliant",
					Status:  metav1.ConditionFalse,
					Reason:  "InstallPlanRequiresApproval",
					Message: "an InstallPlan to update to [strimzi-cluster-operator.v0.36.0] is available for approval",
				},
				"an InstallPlan to update .* is available for approval",
			)
		})
		It("Should do the upgrade when enforced, and stop at the next version", func(ctx SpecContext) {
			ipList, err := targetK8sDynamic.Resource(gvrInstallPlan).Namespace(opPolTestNS).
				List(ctx, metav1.ListOptions{})
			Expect(err).NotTo(HaveOccurred())
			Expect(ipList.Items).To(HaveLen(1))

			firstInstallPlanName = ipList.Items[0].GetName()

			utils.Kubectl("patch", "operatorpolicy", opPolName, "-n", testNamespace, "--type=json", "-p",
				`[{"op": "replace", "path": "/spec/remediationAction", "value": "enforce"},`+
					`{"op": "replace", "path": "/spec/upgradeApproval", "value": "Automatic"}]`)

			Eventually(func(ctx SpecContext) int {
				ipList, err = targetK8sDynamic.Resource(gvrInstallPlan).Namespace(opPolTestNS).
					List(ctx, metav1.ListOptions{})

				return len(ipList.Items)
			}, olmWaitTimeout, 5, ctx).Should(Equal(2))

			secondInstallPlanName = ipList.Items[1].GetName()
			if firstInstallPlanName == secondInstallPlanName {
				secondInstallPlanName = ipList.Items[0].GetName()
			}

			Eventually(func(ctx SpecContext) string {
				ip, _ := targetK8sDynamic.Resource(gvrInstallPlan).Namespace(opPolTestNS).
					Get(ctx, firstInstallPlanName, metav1.GetOptions{})
				phase, _, _ := unstructured.NestedString(ip.Object, "status", "phase")

				return phase
			}, olmWaitTimeout, 5, ctx).Should(Equal("Complete"))

			// This check covers several situations that occur quickly: the first InstallPlan eventually
			// progresses to Complete after it is approved, and the next InstallPlan is created and
			// recognized by the policy (but not yet approved).
			// This check may need to be adjusted in the future for new strimzi releases.
			check(
				opPolName,
				false,
				[]policyv1.RelatedObject{{
					Object: policyv1.ObjectResource{
						Kind:       "InstallPlan",
						APIVersion: "operators.coreos.com/v1alpha1",
						Metadata: policyv1.ObjectMetadata{
							Namespace: opPolTestNS,
							Name:      secondInstallPlanName,
						},
					},
					Compliant: "NonCompliant",
					Reason:    "The InstallPlan is RequiresApproval",
				}},
				metav1.Condition{
					Type:   "InstallPlanCompliant",
					Status: metav1.ConditionFalse,
					Reason: "InstallPlanRequiresApproval",
					Message: "an InstallPlan to update to [strimzi-cluster-operator.v0.36.1] is available for " +
						"approval but approval for [strimzi-cluster-operator.v0.36.1] is required",
				},
				"the InstallPlan.*36.0.*was approved",
			)
		})
		It("Should not approve an upgrade while upgradeApproval is None", func(ctx SpecContext) {
			utils.Kubectl("patch", "operatorpolicy", opPolName, "-n", testNamespace, "--type=json", "-p",
				`[{"op": "add", "path": "/spec/versions/-", "value": "strimzi-cluster-operator.v0.36.1"},`+
					`{"op": "replace", "path": "/spec/upgradeApproval", "value": "None"}]`)
			check(
				opPolName,
				false,
				[]policyv1.RelatedObject{{
					Object: policyv1.ObjectResource{
						Kind:       "InstallPlan",
						APIVersion: "operators.coreos.com/v1alpha1",
						Metadata: policyv1.ObjectMetadata{
							Namespace: opPolTestNS,
							Name:      secondInstallPlanName,
						},
					},
					Compliant: "NonCompliant",
					Reason:    "The InstallPlan is RequiresApproval",
				}},
				metav1.Condition{
					Type:    "InstallPlanCompliant",
					Status:  metav1.ConditionFalse,
					Reason:  "InstallPlanRequiresApproval",
					Message: "an InstallPlan to update to [strimzi-cluster-operator.v0.36.1] is available for approval",
				},
				"an InstallPlan.*36.*is available for approval",
			)
		})
		It("Should approve the upgrade when upgradeApproval is changed to Automatic", func(ctx SpecContext) {
			utils.Kubectl("patch", "operatorpolicy", opPolName, "-n", testNamespace, "--type=json", "-p",
				`[{"op": "replace", "path": "/spec/upgradeApproval", "value": "Automatic"}]`)

			Eventually(func(ctx SpecContext) string {
				ip, _ := targetK8sDynamic.Resource(gvrInstallPlan).Namespace(opPolTestNS).
					Get(ctx, secondInstallPlanName, metav1.GetOptions{})
				phase, _, _ := unstructured.NestedString(ip.Object, "status", "phase")

				return phase
			}, olmWaitTimeout, 5, ctx).Should(Equal("Complete"))

			check(
				opPolName,
				false,
				[]policyv1.RelatedObject{{
					Object: policyv1.ObjectResource{
						Kind:       "InstallPlan",
						APIVersion: "operators.coreos.com/v1alpha1",
						Metadata: policyv1.ObjectMetadata{
							Namespace: opPolTestNS,
							Name:      secondInstallPlanName,
						},
					},
					Compliant: "Compliant",
					Reason:    "The InstallPlan is Complete",
				}},
				metav1.Condition{
					Type:    "InstallPlanCompliant",
					Status:  metav1.ConditionTrue,
					Reason:  "NoInstallPlansRequiringApproval",
					Message: "no InstallPlans requiring approval were found",
				},
				"the InstallPlan.*36.*was approved",
			)
		})
	})
	Describe("Testing full installation behavior, including CRD reporting", Ordered, func() {
		const (
			opPolYAML = "../resources/case38_operator_install/operator-policy-no-group-one-version.yaml"
			opPolName = "oppol-no-group"
		)
		BeforeAll(func() {
			preFunc()
			KubectlTarget("delete", "crd", "--selector=olm.managed=true")
			createObjWithParent(parentPolicyYAML, parentPolicyName,
				opPolYAML, testNamespace, gvrPolicy, gvrOperatorPolicy)
		})

		It("Should initially not report on CRDs because they won't exist yet", func(ctx SpecContext) {
			check(
				opPolName,
				false,
				[]policyv1.RelatedObject{{
					Object: policyv1.ObjectResource{
						Kind:       "CustomResourceDefinition",
						APIVersion: "apiextensions.k8s.io/v1",
						Metadata: policyv1.ObjectMetadata{
							Name: "-",
						},
					},
					Compliant: "Inapplicable",
					Reason:    "No relevant CustomResourceDefinitions found",
				}},
				metav1.Condition{
					Type:    "CustomResourceDefinitionCompliant",
					Status:  metav1.ConditionTrue,
					Reason:  "RelevantCRDNotFound",
					Message: "no CRDs were found for the operator",
				},
				"no CRDs were found for the operator",
			)
		})

		It("Should generate conditions and relatedobjects of CRD", func(ctx SpecContext) {
			utils.Kubectl("patch", "operatorpolicy", opPolName, "-n", testNamespace, "--type=json", "-p",
				`[{"op": "replace", "path": "/spec/remediationAction", "value": "enforce"}]`)
			By("Waiting for a CRD to appear, which should indicate the operator is installing")
			Eventually(func(ctx SpecContext) *unstructured.Unstructured {
				crd, _ := targetK8sDynamic.Resource(gvrCRD).Get(ctx,
					"quayregistries.quay.redhat.com", metav1.GetOptions{})

				return crd
			}, olmWaitTimeout, 5, ctx).ShouldNot(BeNil())

			By("Waiting for the Deployment to be available, indicating the installation is complete")
			Eventually(func(g Gomega) {
				dep, err := targetK8sDynamic.Resource(gvrDeployment).Namespace(opPolTestNS).Get(
					ctx, "quay-operator-tng", metav1.GetOptions{})
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(dep).NotTo(BeNil())

				var deploy appsv1.Deployment

				err = runtime.DefaultUnstructuredConverter.FromUnstructured(dep.Object, &deploy)
				g.Expect(err).NotTo(HaveOccurred())

				g.Expect(deploy.Status.Replicas).NotTo(BeZero())
				g.Expect(deploy.Status.ReadyReplicas).To(Equal(deploy.Status.Replicas))
			}, olmWaitTimeout, 5).Should(Succeed())

			check(
				opPolName,
				false,
				[]policyv1.RelatedObject{{
					Object: policyv1.ObjectResource{
						Kind:       "CustomResourceDefinition",
						APIVersion: "apiextensions.k8s.io/v1",
						Metadata: policyv1.ObjectMetadata{
							Name: "quayregistries.quay.redhat.com",
						},
					},
					Compliant: "Compliant",
					Reason:    "Resource found as expected",
				}},
				metav1.Condition{
					Type:    "CustomResourceDefinitionCompliant",
					Status:  metav1.ConditionTrue,
					Reason:  "RelevantCRDFound",
					Message: "there are CRDs present for the operator",
				},
				"there are CRDs present for the operator",
			)
		})

		It("should send a new compliance event if the status is reset", func(ctx SpecContext) {
			By("Waiting 10 seconds for any late reconciles")
			time.Sleep(10 * time.Second)

			originalEvents := utils.GetMatchingEvents(
				clientManaged, testNamespace, parentPolicyName, opPolName, "", eventuallyTimeout,
			)

			By("Resetting the status, and checking for a new event")
			utils.Kubectl("patch", "operatorpolicy", opPolName, "-n", testNamespace, "--type=json", "-p",
				`[{"op": "remove", "path": "/status/compliant"}]`, "--subresource=status")

			Eventually(func(ctx SpecContext) int {
				newEvents := utils.GetMatchingEvents(
					clientManaged, testNamespace, parentPolicyName, opPolName, "", eventuallyTimeout,
				)

				return len(newEvents)
			}, 10, 1, ctx).Should(BeNumerically(">", len(originalEvents)))
		})
	})
	Describe("Testing OperatorPolicy validation messages", Ordered, func() {
		const (
			opPolYAML = "../resources/case38_operator_install/operator-policy-validity-test.yaml"
			opPolName = "oppol-validity-test"
			subName   = "project-quay"
		)

		BeforeAll(func() {
			preFunc()
			DeferCleanup(func() {
				KubectlTarget("delete", "ns", "nonexist-testns")
			})

			createObjWithParent(parentPolicyYAML, parentPolicyName,
				opPolYAML, testNamespace, gvrPolicy, gvrOperatorPolicy)
		})

		It("Should initially report unknown fields", func() {
			check(
				opPolName,
				true,
				[]policyv1.RelatedObject{},
				metav1.Condition{
					Type:    "ValidPolicySpec",
					Status:  metav1.ConditionFalse,
					Reason:  "InvalidPolicySpec",
					Message: `spec.subscription is invalid: json: unknown field "actually"`,
				},
				`the status of the Subscription could not be determined because the policy is invalid`,
			)
			check(
				opPolName,
				true,
				[]policyv1.RelatedObject{},
				metav1.Condition{
					Type:    "ValidPolicySpec",
					Status:  metav1.ConditionFalse,
					Reason:  "InvalidPolicySpec",
					Message: `spec.operatorGroup is invalid: json: unknown field "foo"`,
				},
				`the status of the OperatorGroup could not be determined because the policy is invalid`,
			)
		})
		It("Should report about the prohibited installPlanApproval value", func() {
			// remove the "unknown" fields
			utils.Kubectl("patch", "operatorpolicy", opPolName, "-n", testNamespace, "--type=json", "-p",
				`[{"op": "remove", "path": "/spec/operatorGroup/foo"}, `+
					`{"op": "remove", "path": "/spec/subscription/actually"}]`)
			check(
				opPolName,
				true,
				[]policyv1.RelatedObject{},
				metav1.Condition{
					Type:    "ValidPolicySpec",
					Status:  metav1.ConditionFalse,
					Reason:  "InvalidPolicySpec",
					Message: "installPlanApproval is prohibited in spec.subscription",
				},
				"installPlanApproval is prohibited in spec.subscription",
			)
		})
		It("Should report about the namespaces not matching", func() {
			// Remove the `installPlanApproval` value
			utils.Kubectl("patch", "operatorpolicy", opPolName, "-n", testNamespace, "--type=json", "-p",
				`[{"op": "remove", "path": "/spec/subscription/installPlanApproval"}]`)
			check(
				opPolName,
				true,
				[]policyv1.RelatedObject{},
				metav1.Condition{
					Type:   "ValidPolicySpec",
					Status: metav1.ConditionFalse,
					Reason: "InvalidPolicySpec",
					Message: "the namespace specified in spec.operatorGroup ('operator-policy-testns') must match " +
						"the namespace used for the subscription ('nonexist-testns')",
				},
				"NonCompliant",
			)
		})
		It("Should report about the namespace not existing", func() {
			// Fix the namespace mismatch by removing the operator group spec
			utils.Kubectl("patch", "operatorpolicy", opPolName, "-n", testNamespace, "--type=json", "-p",
				`[{"op": "remove", "path": "/spec/operatorGroup"}]`)
			check(
				opPolName,
				true,
				[]policyv1.RelatedObject{{
					Object: policyv1.ObjectResource{
						Kind:       "Subscription",
						APIVersion: "operators.coreos.com/v1alpha1",
						Metadata: policyv1.ObjectMetadata{
							Name:      subName,
							Namespace: "nonexist-testns",
						},
					},
					Compliant: "NonCompliant",
					Reason:    "Resource not found but should exist",
				}},
				metav1.Condition{
					Type:    "ValidPolicySpec",
					Status:  metav1.ConditionFalse,
					Reason:  "InvalidPolicySpec",
					Message: "the operator namespace ('nonexist-testns') does not exist",
				},
				"NonCompliant",
			)
		})
		It("Should update the status after the namespace is created", func() {
			KubectlTarget("create", "namespace", "nonexist-testns")
			check(
				opPolName,
				true,
				[]policyv1.RelatedObject{{
					Object: policyv1.ObjectResource{
						Kind:       "Subscription",
						APIVersion: "operators.coreos.com/v1alpha1",
						Metadata: policyv1.ObjectMetadata{
							Name:      subName,
							Namespace: "nonexist-testns",
						},
					},
					Compliant: "NonCompliant",
					Reason:    "Resource not found but should exist",
				}},
				metav1.Condition{
					Type:    "ValidPolicySpec",
					Status:  metav1.ConditionTrue,
					Reason:  "PolicyValidated",
					Message: "the policy spec is valid",
				},
				"the policy spec is valid",
			)
		})
	})
	Describe("Testing general OperatorPolicy mustnothave behavior", Ordered, func() {
		const (
			opPolYAML      = "../resources/case38_operator_install/operator-policy-mustnothave.yaml"
			opPolName      = "oppol-mustnothave"
			subName        = "project-quay"
			deploymentName = "quay-operator-tng"
			catSrcName     = "operatorhubio-catalog"
			catSrcNS       = "olm"
		)

		BeforeAll(func() {
			preFunc()
			KubectlTarget("delete", "crd", "--selector=olm.managed=true")

			createObjWithParent(parentPolicyYAML, parentPolicyName,
				opPolYAML, testNamespace, gvrPolicy, gvrOperatorPolicy)
		})

		It("Should be Compliant and report all the things are correctly missing", func() {
			check(
				opPolName,
				false,
				[]policyv1.RelatedObject{{
					Object: policyv1.ObjectResource{
						Kind:       "OperatorGroup",
						APIVersion: "operators.coreos.com/v1",
						Metadata: policyv1.ObjectMetadata{
							Namespace: opPolTestNS,
							Name:      "-",
						},
					},
					Compliant: "Compliant",
					Reason:    "Resource not found as expected",
				}},
				metav1.Condition{
					Type:    "OperatorGroupCompliant",
					Status:  metav1.ConditionTrue,
					Reason:  "OperatorGroupNotPresent",
					Message: "the OperatorGroup is not present",
				},
				`the OperatorGroup is not present`,
			)
			check(
				opPolName,
				false,
				[]policyv1.RelatedObject{{
					Object: policyv1.ObjectResource{
						Kind:       "Subscription",
						APIVersion: "operators.coreos.com/v1alpha1",
						Metadata: policyv1.ObjectMetadata{
							Namespace: opPolTestNS,
							Name:      "project-quay",
						},
					},
					Compliant: "Compliant",
					Reason:    "Resource not found as expected",
				}},
				metav1.Condition{
					Type:    "SubscriptionCompliant",
					Status:  metav1.ConditionTrue,
					Reason:  "SubscriptionNotPresent",
					Message: "the Subscription is not present",
				},
				`the Subscription is not present`,
			)
			check(
				opPolName,
				false,
				[]policyv1.RelatedObject{{
					Object: policyv1.ObjectResource{
						Kind:       "InstallPlan",
						APIVersion: "operators.coreos.com/v1alpha1",
						Metadata: policyv1.ObjectMetadata{
							Namespace: opPolTestNS,
							Name:      "-",
						},
					},
					Compliant: "Compliant",
					Reason:    "There are no relevant InstallPlans in this namespace",
				}},
				metav1.Condition{
					Type:    "InstallPlanCompliant",
					Status:  metav1.ConditionTrue,
					Reason:  "NoInstallPlansFound",
					Message: "there are no relevant InstallPlans in the namespace",
				},
				`there are no relevant InstallPlans in the namespace`,
			)
			check(
				opPolName,
				false,
				[]policyv1.RelatedObject{{
					Object: policyv1.ObjectResource{
						Kind:       "ClusterServiceVersion",
						APIVersion: "operators.coreos.com/v1alpha1",
						Metadata: policyv1.ObjectMetadata{
							Namespace: opPolTestNS,
							Name:      "-",
						},
					},
					Compliant: "Compliant",
					Reason:    "Resource not found as expected",
				}},
				metav1.Condition{
					Type:    "ClusterServiceVersionCompliant",
					Status:  metav1.ConditionTrue,
					Reason:  "ClusterServiceVersionNotPresent",
					Message: "the ClusterServiceVersion is not present",
				},
				`the ClusterServiceVersion is not present`,
			)
			check(
				opPolName,
				false,
				[]policyv1.RelatedObject{},
				metav1.Condition{
					Type:    "DeploymentCompliant",
					Status:  metav1.ConditionTrue,
					Reason:  "DeploymentNotApplicable",
					Message: "MustNotHave policies ignore kind Deployment",
				},
				`MustNotHave policies ignore kind Deployment`,
			)
			check(
				opPolName,
				false,
				[]policyv1.RelatedObject{{
					Object: policyv1.ObjectResource{
						Kind:       "CustomResourceDefinition",
						APIVersion: "apiextensions.k8s.io/v1",
						Metadata: policyv1.ObjectMetadata{
							Name: "-",
						},
					},
					Compliant: "Inapplicable",
					Reason:    "No relevant CustomResourceDefinitions found",
				}},
				metav1.Condition{
					Type:    "CustomResourceDefinitionCompliant",
					Status:  metav1.ConditionTrue,
					Reason:  "RelevantCRDNotFound",
					Message: "no CRDs were found for the operator",
				},
				`no CRDs were found for the operator`,
			)
			check(
				opPolName,
				false,
				[]policyv1.RelatedObject{},
				metav1.Condition{
					Type:    "CatalogSourcesUnhealthy",
					Status:  metav1.ConditionFalse,
					Reason:  "CatalogSourceNotApplicable",
					Message: "MustNotHave policies ignore kind CatalogSource",
				},
				`MustNotHave policies ignore kind CatalogSource`,
			)

			// The `check` function doesn't check that it is compliant, only that each piece is compliant
			checkCompliance(opPolName, testNamespace, olmWaitTimeout, policyv1.Compliant)
		})
		It("Should be NonCompliant and report resources when the operator is installed", func(ctx SpecContext) {
			// Make it musthave and enforced, to install the operator
			utils.Kubectl("patch", "operatorpolicy", opPolName, "-n", testNamespace, "--type=json", "-p",
				`[{"op": "replace", "path": "/spec/complianceType", "value": "musthave"},`+
					`{"op": "replace", "path": "/spec/remediationAction", "value": "enforce"}]`)

			By("Waiting for a CRD to appear, which should indicate the operator is installing")
			Eventually(func(ctx SpecContext) *unstructured.Unstructured {
				crd, _ := targetK8sDynamic.Resource(gvrCRD).Get(ctx,
					"quayregistries.quay.redhat.com", metav1.GetOptions{})

				return crd
			}, olmWaitTimeout, 5, ctx).ShouldNot(BeNil())

			// Revert to the original mustnothave policy
			utils.Kubectl("patch", "operatorpolicy", opPolName, "-n", testNamespace, "--type=json", "-p",
				`[{"op": "replace", "path": "/spec/complianceType", "value": "mustnothave"},`+
					`{"op": "replace", "path": "/spec/remediationAction", "value": "inform"}]`)

			By("Checking the OperatorPolicy status")
			check(
				opPolName,
				true,
				[]policyv1.RelatedObject{{
					Object: policyv1.ObjectResource{
						Kind:       "OperatorGroup",
						APIVersion: "operators.coreos.com/v1",
						Metadata: policyv1.ObjectMetadata{
							Namespace: opPolTestNS,
						},
					},
					Compliant: "NonCompliant",
					Reason:    "Resource found but should not exist",
				}},
				metav1.Condition{
					Type:    "OperatorGroupCompliant",
					Status:  metav1.ConditionFalse,
					Reason:  "OperatorGroupPresent",
					Message: "the OperatorGroup is present",
				},
				`the OperatorGroup is present`,
			)
			check(
				opPolName,
				true,
				[]policyv1.RelatedObject{{
					Object: policyv1.ObjectResource{
						Kind:       "Subscription",
						APIVersion: "operators.coreos.com/v1alpha1",
						Metadata: policyv1.ObjectMetadata{
							Namespace: opPolTestNS,
							Name:      "project-quay",
						},
					},
					Compliant: "NonCompliant",
					Reason:    "Resource found but should not exist",
				}},
				metav1.Condition{
					Type:    "SubscriptionCompliant",
					Status:  metav1.ConditionFalse,
					Reason:  "SubscriptionPresent",
					Message: "the Subscription is present",
				},
				`the Subscription is present`,
			)
			check(
				opPolName,
				true,
				[]policyv1.RelatedObject{{
					Object: policyv1.ObjectResource{
						Kind:       "InstallPlan",
						APIVersion: "operators.coreos.com/v1alpha1",
						Metadata: policyv1.ObjectMetadata{
							Namespace: opPolTestNS,
						},
					},
					Compliant: "Compliant",
					Reason:    "Resource found but will not be handled in mustnothave mode",
				}},
				metav1.Condition{
					Type:    "InstallPlanCompliant",
					Status:  metav1.ConditionTrue,
					Reason:  "InstallPlanNotApplicable",
					Message: "MustNotHave policies ignore kind InstallPlan",
				},
				`MustNotHave policies ignore kind InstallPlan`,
			)
			check(
				opPolName,
				true,
				[]policyv1.RelatedObject{{
					Object: policyv1.ObjectResource{
						Kind:       "ClusterServiceVersion",
						APIVersion: "operators.coreos.com/v1alpha1",
						Metadata: policyv1.ObjectMetadata{
							Namespace: opPolTestNS,
						},
					},
					Compliant: "NonCompliant",
					Reason:    "Resource found but should not exist",
				}},
				metav1.Condition{
					Type:    "ClusterServiceVersionCompliant",
					Status:  metav1.ConditionFalse,
					Reason:  "ClusterServiceVersionPresent",
					Message: "the ClusterServiceVersion (quay-operator.v3.10.0) is present",
				},
				regexp.QuoteMeta("the ClusterServiceVersion (quay-operator.v3.10.0) is present"),
			)
			check(
				opPolName,
				true,
				[]policyv1.RelatedObject{{
					Object: policyv1.ObjectResource{
						Kind:       "CustomResourceDefinition",
						APIVersion: "apiextensions.k8s.io/v1",
						Metadata: policyv1.ObjectMetadata{
							Name: "quayregistries.quay.redhat.com",
						},
					},
					Compliant: "NonCompliant",
					Reason:    "Resource found but should not exist",
				}},
				metav1.Condition{
					Type:    "CustomResourceDefinitionCompliant",
					Status:  metav1.ConditionFalse,
					Reason:  "CustomResourceDefinitionPresent",
					Message: "the CustomResourceDefinition is present",
				},
				`the CustomResourceDefinition is present`,
			)
			check(
				opPolName,
				true,
				[]policyv1.RelatedObject{{
					Object: policyv1.ObjectResource{
						Kind:       "Deployment",
						APIVersion: "apps/v1",
						Metadata: policyv1.ObjectMetadata{
							Name:      deploymentName,
							Namespace: opPolTestNS,
						},
					},
					Compliant: "Compliant",
					Reason:    "Resource found but will not be handled in mustnothave mode",
				}},
				metav1.Condition{
					Type:    "DeploymentCompliant",
					Status:  metav1.ConditionTrue,
					Reason:  "DeploymentNotApplicable",
					Message: "MustNotHave policies ignore kind Deployment",
				},
				`MustNotHave policies ignore kind Deployment`,
			)
			check(
				opPolName,
				false,
				[]policyv1.RelatedObject{{
					Object: policyv1.ObjectResource{
						Kind:       "CatalogSource",
						APIVersion: "operators.coreos.com/v1alpha1",
						Metadata: policyv1.ObjectMetadata{
							Name:      catSrcName,
							Namespace: catSrcNS,
						},
					},
					Compliant: "Compliant",
					Reason:    "Resource found but will not be handled in mustnothave mode",
				}},
				metav1.Condition{
					Type:    "CatalogSourcesUnhealthy",
					Status:  metav1.ConditionFalse,
					Reason:  "CatalogSourceNotApplicable",
					Message: "MustNotHave policies ignore kind CatalogSource",
				},
				`MustNotHave policies ignore kind CatalogSource`,
			)
		})

		// These are the same for inform and enforce, so just write them once
		keptChecks := func() {
			GinkgoHelper()

			check(
				opPolName,
				false,
				[]policyv1.RelatedObject{{
					Object: policyv1.ObjectResource{
						Kind:       "OperatorGroup",
						APIVersion: "operators.coreos.com/v1",
						Metadata: policyv1.ObjectMetadata{
							Namespace: opPolTestNS,
						},
					},
					Reason: "The OperatorGroup is attached to a mustnothave policy, but does not need to be removed",
				}},
				metav1.Condition{
					Type:    "OperatorGroupCompliant",
					Status:  metav1.ConditionTrue,
					Reason:  "OperatorGroupKept",
					Message: "the policy specifies to keep the OperatorGroup",
				},
				`the policy specifies to keep the OperatorGroup`,
			)
			check(
				opPolName,
				false,
				[]policyv1.RelatedObject{{
					Object: policyv1.ObjectResource{
						Kind:       "Subscription",
						APIVersion: "operators.coreos.com/v1alpha1",
						Metadata: policyv1.ObjectMetadata{
							Namespace: opPolTestNS,
							Name:      "project-quay",
						},
					},
					Reason: "The Subscription is attached to a mustnothave policy, but does not need to be removed",
				}},
				metav1.Condition{
					Type:    "SubscriptionCompliant",
					Status:  metav1.ConditionTrue,
					Reason:  "SubscriptionKept",
					Message: "the policy specifies to keep the Subscription",
				},
				`the policy specifies to keep the Subscription`,
			)
			check(
				opPolName,
				false,
				[]policyv1.RelatedObject{{
					Object: policyv1.ObjectResource{
						Kind:       "ClusterServiceVersion",
						APIVersion: "operators.coreos.com/v1alpha1",
						Metadata: policyv1.ObjectMetadata{
							Namespace: opPolTestNS,
						},
					},
					Reason: "The ClusterServiceVersion is attached to a mustnothave policy, " +
						"but does not need to be removed",
				}},
				metav1.Condition{
					Type:    "ClusterServiceVersionCompliant",
					Status:  metav1.ConditionTrue,
					Reason:  "ClusterServiceVersionKept",
					Message: "the policy specifies to keep the ClusterServiceVersion",
				},
				`the policy specifies to keep the ClusterServiceVersion`,
			)
			check(
				opPolName,
				false,
				[]policyv1.RelatedObject{{
					Object: policyv1.ObjectResource{
						Kind:       "CustomResourceDefinition",
						APIVersion: "apiextensions.k8s.io/v1",
						Metadata: policyv1.ObjectMetadata{
							Name: "quayregistries.quay.redhat.com",
						},
					},
					Reason: "The CustomResourceDefinition is attached to a mustnothave policy, but " +
						"does not need to be removed",
				}},
				metav1.Condition{
					Type:    "CustomResourceDefinitionCompliant",
					Status:  metav1.ConditionTrue,
					Reason:  "CustomResourceDefinitionKept",
					Message: "the policy specifies to keep the CustomResourceDefinition",
				},
				`the policy specifies to keep the CustomResourceDefinition`,
			)
		}
		It("Should report resources differently when told to keep them", func(ctx SpecContext) {
			// Change the removal behaviors from Delete to Keep
			utils.Kubectl("patch", "operatorpolicy", opPolName, "-n", testNamespace, "--type=json", "-p",
				`[{"op": "replace", "path": "/spec/removalBehavior/operatorGroups", "value": "Keep"},`+
					`{"op": "replace", "path": "/spec/removalBehavior/subscriptions", "value": "Keep"},`+
					`{"op": "replace", "path": "/spec/removalBehavior/clusterServiceVersions", "value": "Keep"},`+
					`{"op": "replace", "path": "/spec/removalBehavior/customResourceDefinitions", "value": "Keep"}]`)
			By("Checking the OperatorPolicy status")
			keptChecks()
			check(
				opPolName,
				false,
				[]policyv1.RelatedObject{{
					Object: policyv1.ObjectResource{
						Kind:       "Deployment",
						APIVersion: "apps/v1",
						Metadata: policyv1.ObjectMetadata{
							Name:      deploymentName,
							Namespace: opPolTestNS,
						},
					},
					Compliant: "Compliant",
					Reason:    "Resource found but will not be handled in mustnothave mode",
				}},
				metav1.Condition{
					Type:    "DeploymentCompliant",
					Status:  metav1.ConditionTrue,
					Reason:  "DeploymentNotApplicable",
					Message: "MustNotHave policies ignore kind Deployment",
				},
				`MustNotHave policies ignore kind Deployment`,
			)
			check(
				opPolName,
				false,
				[]policyv1.RelatedObject{{
					Object: policyv1.ObjectResource{
						Kind:       "CatalogSource",
						APIVersion: "operators.coreos.com/v1alpha1",
						Metadata: policyv1.ObjectMetadata{
							Name:      catSrcName,
							Namespace: catSrcNS,
						},
					},
					Compliant: "Compliant",
					Reason:    "Resource found but will not be handled in mustnothave mode",
				}},
				metav1.Condition{
					Type:    "CatalogSourcesUnhealthy",
					Status:  metav1.ConditionFalse,
					Reason:  "CatalogSourceNotApplicable",
					Message: "MustNotHave policies ignore kind CatalogSource",
				},
				`MustNotHave policies ignore kind CatalogSource`,
			)
		})
		It("Should not remove anything when enforced while set to Keep everything", func(ctx SpecContext) {
			// Enforce the policy
			utils.Kubectl("patch", "operatorpolicy", opPolName, "-n", testNamespace, "--type=json", "-p",
				`[{"op": "replace", "path": "/spec/remediationAction", "value": "enforce"}]`)
			By("Checking the OperatorPolicy status")
			keptChecks()
			check(
				opPolName,
				false,
				[]policyv1.RelatedObject{{
					Object: policyv1.ObjectResource{
						Kind:       "Deployment",
						APIVersion: "apps/v1",
						Metadata: policyv1.ObjectMetadata{
							Name:      deploymentName,
							Namespace: opPolTestNS,
						},
					},
					Compliant: "Compliant",
					Reason:    "Resource found but will not be handled in mustnothave mode",
				}},
				metav1.Condition{
					Type:    "DeploymentCompliant",
					Status:  metav1.ConditionTrue,
					Reason:  "DeploymentNotApplicable",
					Message: "MustNotHave policies ignore kind Deployment",
				},
				`MustNotHave policies ignore kind Deployment`,
			)
			check(
				opPolName,
				false,
				[]policyv1.RelatedObject{{
					Object: policyv1.ObjectResource{
						Kind:       "CatalogSource",
						APIVersion: "operators.coreos.com/v1alpha1",
						Metadata: policyv1.ObjectMetadata{
							Name:      catSrcName,
							Namespace: catSrcNS,
						},
					},
					Compliant: "Compliant",
					Reason:    "Resource found but will not be handled in mustnothave mode",
				}},
				metav1.Condition{
					Type:    "CatalogSourcesUnhealthy",
					Status:  metav1.ConditionFalse,
					Reason:  "CatalogSourceNotApplicable",
					Message: "MustNotHave policies ignore kind CatalogSource",
				},
				`MustNotHave policies ignore kind CatalogSource`,
			)

			By("Checking that certain (named) resources are still there")
			utils.GetWithTimeout(targetK8sDynamic, gvrClusterServiceVersion, "quay-operator.v3.10.0",
				opPolTestNS, true, eventuallyTimeout)
			utils.GetWithTimeout(targetK8sDynamic, gvrSubscription, subName,
				opPolTestNS, true, eventuallyTimeout)
			utils.GetWithTimeout(targetK8sDynamic, gvrCRD, "quayregistries.quay.redhat.com",
				"", true, eventuallyTimeout)
		})
		It("Should report a special status when the resources are stuck", func(ctx SpecContext) {
			By("Adding a finalizer to each of the resources")
			pol, err := clientManagedDynamic.Resource(gvrOperatorPolicy).
				Namespace(testNamespace).Get(ctx, opPolName, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())
			Expect(pol).NotTo(BeNil())

			relatedObjects, found, err := unstructured.NestedSlice(pol.Object, "status", "relatedObjects")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())

			var opGroupName, installPlanName, csvName string
			crdNames := make([]string, 0)

			for _, relatedObject := range relatedObjects {
				relatedObj, ok := relatedObject.(map[string]interface{})
				Expect(ok).To(BeTrue())

				objKind, found, err := unstructured.NestedString(relatedObj, "object", "kind")
				Expect(err).NotTo(HaveOccurred())
				Expect(found).To(BeTrue())

				objName, found, err := unstructured.NestedString(relatedObj, "object", "metadata", "name")
				Expect(err).NotTo(HaveOccurred())
				Expect(found).To(BeTrue())

				switch objKind {
				case "OperatorGroup":
					opGroupName = objName
				case "Subscription":
					// just do the finalizer; we already know the subscription name
				case "ClusterServiceVersion":
					csvName = objName
				case "InstallPlan":
					installPlanName = objName
				case "CustomResourceDefinition":
					crdNames = append(crdNames, objName)
				default:
					// skip adding / removing the finalizer for other types
					continue
				}

				KubectlTarget("patch", objKind, objName, "-n", opPolTestNS, "--type=json", "-p",
					`[{"op": "add", "path": "/metadata/finalizers", "value": ["donutdelete"]}]`)
				DeferCleanup(func() {
					By("removing the finalizer from " + objKind + " " + objName)
					KubectlTarget("patch", objKind, objName, "-n", opPolTestNS, "--type=json", "-p",
						`[{"op": "remove", "path": "/metadata/finalizers"}]`)
				})
			}

			By("Setting the removal behaviors to Delete")
			utils.Kubectl("patch", "operatorpolicy", opPolName, "-n", testNamespace, "--type=json", "-p",
				`[{"op": "replace", "path": "/spec/removalBehavior/operatorGroups", "value": "DeleteIfUnused"},`+
					`{"op": "replace", "path": "/spec/removalBehavior/subscriptions", "value": "Delete"},`+
					`{"op": "replace", "path": "/spec/removalBehavior/clusterServiceVersions", "value": "Delete"},`+
					`{"op": "replace", "path": "/spec/removalBehavior/customResourceDefinitions", "value": "Delete"}]`)

			check(
				opPolName,
				false,
				[]policyv1.RelatedObject{{
					Object: policyv1.ObjectResource{
						Kind:       "OperatorGroup",
						APIVersion: "operators.coreos.com/v1",
						Metadata: policyv1.ObjectMetadata{
							Namespace: opPolTestNS,
							Name:      opGroupName,
						},
					},
					Compliant: "NonCompliant",
					Reason:    "The object is being deleted but has not been removed yet",
				}},
				metav1.Condition{
					Type:    "OperatorGroupCompliant",
					Status:  metav1.ConditionFalse,
					Reason:  "OperatorGroupDeleting",
					Message: "the OperatorGroup has a deletion timestamp",
				},
				`the OperatorGroup was deleted`,
			)
			check(
				opPolName,
				false,
				[]policyv1.RelatedObject{{
					Object: policyv1.ObjectResource{
						Kind:       "Subscription",
						APIVersion: "operators.coreos.com/v1alpha1",
						Metadata: policyv1.ObjectMetadata{
							Namespace: opPolTestNS,
							Name:      "project-quay",
						},
					},
					Compliant: "NonCompliant",
					Reason:    "The object is being deleted but has not been removed yet",
				}},
				metav1.Condition{
					Type:    "SubscriptionCompliant",
					Status:  metav1.ConditionFalse,
					Reason:  "SubscriptionDeleting",
					Message: "the Subscription has a deletion timestamp",
				},
				`the Subscription was deleted`,
			)
			check(
				opPolName,
				true,
				[]policyv1.RelatedObject{{
					Object: policyv1.ObjectResource{
						Kind:       "InstallPlan",
						APIVersion: "operators.coreos.com/v1alpha1",
						Metadata: policyv1.ObjectMetadata{
							Name:      installPlanName,
							Namespace: opPolTestNS,
						},
					},
					Compliant: "Compliant",
					Reason:    "Resource found but will not be handled in mustnothave mode",
				}},
				metav1.Condition{
					Type:    "InstallPlanCompliant",
					Status:  metav1.ConditionTrue,
					Reason:  "InstallPlanNotApplicable",
					Message: "MustNotHave policies ignore kind InstallPlan",
				},
				`MustNotHave policies ignore kind InstallPlan`,
			)
			check(
				opPolName,
				false,
				[]policyv1.RelatedObject{{
					Object: policyv1.ObjectResource{
						Kind:       "ClusterServiceVersion",
						APIVersion: "operators.coreos.com/v1alpha1",
						Metadata: policyv1.ObjectMetadata{
							Namespace: opPolTestNS,
							Name:      csvName,
						},
					},
					Compliant: "NonCompliant",
					Reason:    "The object is being deleted but has not been removed yet",
				}},
				metav1.Condition{
					Type:    "ClusterServiceVersionCompliant",
					Status:  metav1.ConditionFalse,
					Reason:  "ClusterServiceVersionDeleting",
					Message: "the ClusterServiceVersion (" + csvName + ") has a deletion timestamp",
				},
				regexp.QuoteMeta("the ClusterServiceVersion ("+csvName+") was deleted"),
			)
			desiredCRDObjects := make([]policyv1.RelatedObject, 0)
			for _, name := range crdNames {
				desiredCRDObjects = append(desiredCRDObjects, policyv1.RelatedObject{
					Object: policyv1.ObjectResource{
						Kind:       "CustomResourceDefinition",
						APIVersion: "apiextensions.k8s.io/v1",
						Metadata: policyv1.ObjectMetadata{
							Name: name,
						},
					},
					Compliant: "NonCompliant",
					Reason:    "The object is being deleted but has not been removed yet",
				})
			}
			check(
				opPolName,
				false,
				desiredCRDObjects,
				metav1.Condition{
					Type:    "CustomResourceDefinitionCompliant",
					Status:  metav1.ConditionFalse,
					Reason:  "CustomResourceDefinitionDeleting",
					Message: "the CustomResourceDefinition has a deletion timestamp",
				},
				`the CustomResourceDefinition was deleted`,
			)
		})
		It("Should report things as gone after the finalizers are removed", func() {
			By("Checking that certain (named) resources are not there, indicating the removal was completed")
			utils.GetWithTimeout(targetK8sDynamic, gvrClusterServiceVersion, "quay-operator.v3.10.0",
				opPolTestNS, false, eventuallyTimeout)
			utils.GetWithTimeout(targetK8sDynamic, gvrSubscription, subName,
				opPolTestNS, false, eventuallyTimeout)
			utils.GetWithTimeout(targetK8sDynamic, gvrCRD, "quayregistries.quay.redhat.com",
				"", false, eventuallyTimeout)

			By("Checking the OperatorPolicy status")
			check(
				opPolName,
				false,
				[]policyv1.RelatedObject{{
					Object: policyv1.ObjectResource{
						Kind:       "OperatorGroup",
						APIVersion: "operators.coreos.com/v1",
						Metadata: policyv1.ObjectMetadata{
							Namespace: opPolTestNS,
							Name:      "-",
						},
					},
					Compliant: "Compliant",
					Reason:    "Resource not found as expected",
				}},
				metav1.Condition{
					Type:    "OperatorGroupCompliant",
					Status:  metav1.ConditionTrue,
					Reason:  "OperatorGroupNotPresent",
					Message: "the OperatorGroup is not present",
				},
				`the OperatorGroup was deleted`,
			)
			check(
				opPolName,
				false,
				[]policyv1.RelatedObject{{
					Object: policyv1.ObjectResource{
						Kind:       "Subscription",
						APIVersion: "operators.coreos.com/v1alpha1",
						Metadata: policyv1.ObjectMetadata{
							Namespace: opPolTestNS,
							Name:      "project-quay",
						},
					},
					Compliant: "Compliant",
					Reason:    "Resource not found as expected",
				}},
				metav1.Condition{
					Type:    "SubscriptionCompliant",
					Status:  metav1.ConditionTrue,
					Reason:  "SubscriptionNotPresent",
					Message: "the Subscription is not present",
				},
				`the Subscription was deleted`,
			)
			check(
				opPolName,
				false,
				[]policyv1.RelatedObject{{
					Object: policyv1.ObjectResource{
						Kind:       "InstallPlan",
						APIVersion: "operators.coreos.com/v1alpha1",
						Metadata: policyv1.ObjectMetadata{
							Namespace: opPolTestNS,
							Name:      "-",
						},
					},
					Compliant: "Compliant",
					Reason:    "There are no relevant InstallPlans in this namespace",
				}},
				metav1.Condition{
					Type:    "InstallPlanCompliant",
					Status:  metav1.ConditionTrue,
					Reason:  "NoInstallPlansFound",
					Message: "there are no relevant InstallPlans in the namespace",
				},
				`there are no relevant InstallPlans in the namespace`,
			)
			check(
				opPolName,
				false,
				[]policyv1.RelatedObject{{
					Object: policyv1.ObjectResource{
						Kind:       "ClusterServiceVersion",
						APIVersion: "operators.coreos.com/v1alpha1",
						Metadata: policyv1.ObjectMetadata{
							Namespace: opPolTestNS,
							Name:      "-",
						},
					},
					Compliant: "Compliant",
					Reason:    "Resource not found as expected",
				}},
				metav1.Condition{
					Type:    "ClusterServiceVersionCompliant",
					Status:  metav1.ConditionTrue,
					Reason:  "ClusterServiceVersionNotPresent",
					Message: "the ClusterServiceVersion is not present",
				},
				regexp.QuoteMeta("the ClusterServiceVersion (quay-operator.v3.10.0) was deleted"),
			)
			check(
				opPolName,
				false,
				[]policyv1.RelatedObject{{
					Object: policyv1.ObjectResource{
						Kind:       "CustomResourceDefinition",
						APIVersion: "apiextensions.k8s.io/v1",
						Metadata: policyv1.ObjectMetadata{
							Name: "-",
						},
					},
					Compliant: "Inapplicable",
					Reason:    "No relevant CustomResourceDefinitions found",
				}},
				metav1.Condition{
					Type:    "CustomResourceDefinitionCompliant",
					Status:  metav1.ConditionTrue,
					Reason:  "RelevantCRDNotFound",
					Message: "no CRDs were found for the operator",
				},
				`the CustomResourceDefinition was deleted`,
			)
			check(
				opPolName,
				false,
				[]policyv1.RelatedObject{{
					Object: policyv1.ObjectResource{
						Kind:       "Deployment",
						APIVersion: "apps/v1",
						Metadata: policyv1.ObjectMetadata{
							Name:      deploymentName,
							Namespace: opPolTestNS,
						},
					},
					Compliant: "Compliant",
					Reason:    "Resource found but will not be handled in mustnothave mode",
				}},
				metav1.Condition{
					Type:    "DeploymentCompliant",
					Status:  metav1.ConditionTrue,
					Reason:  "DeploymentNotApplicable",
					Message: "MustNotHave policies ignore kind Deployment",
				},
				`MustNotHave policies ignore kind Deployment`,
			)
			check(
				opPolName,
				false,
				[]policyv1.RelatedObject{{
					Object: policyv1.ObjectResource{
						Kind:       "CatalogSource",
						APIVersion: "operators.coreos.com/v1alpha1",
						Metadata: policyv1.ObjectMetadata{
							Name:      catSrcName,
							Namespace: catSrcNS,
						},
					},
					Compliant: "Compliant",
					Reason:    "Resource found but will not be handled in mustnothave mode",
				}},
				metav1.Condition{
					Type:    "CatalogSourcesUnhealthy",
					Status:  metav1.ConditionFalse,
					Reason:  "CatalogSourceNotApplicable",
					Message: "MustNotHave policies ignore kind CatalogSource",
				},
				`MustNotHave policies ignore kind CatalogSource`,
			)

			// the checks don't verify that the policy is compliant, do that now:
			checkCompliance(opPolName, testNamespace, eventuallyTimeout, policyv1.Compliant)
		})
	})
	Describe("Test CRD deletion delayed because of a finalizer", Ordered, func() {
		const (
			opPolYAML = "../resources/case38_operator_install/operator-policy-mustnothave-any-version.yaml"
			opPolName = "oppol-mustnothave"
			subName   = "project-quay"
		)

		BeforeAll(func(ctx SpecContext) {
			preFunc()
			KubectlTarget("delete", "crd", "--selector=olm.managed=true")

			createObjWithParent(parentPolicyYAML, parentPolicyName,
				opPolYAML, testNamespace, gvrPolicy, gvrOperatorPolicy)
		})
		AfterAll(func(ctx SpecContext) {
			crd, err := targetK8sDynamic.Resource(gvrCRD).Get(
				ctx, "quayregistries.quay.redhat.com", metav1.GetOptions{})
			if k8serrors.IsNotFound(err) {
				return
			}
			Expect(crd).NotTo(BeNil())

			KubectlTarget("patch", "crd", "quayregistries.quay.redhat.com", "--type=json", "-p",
				`[{"op": "remove", "path": "/metadata/finalizers"}]`)
		})
		It("Initially behaves correctly as musthave", func(ctx SpecContext) {
			// Make it musthave and enforced, to install the operator
			utils.Kubectl("patch", "operatorpolicy", opPolName, "-n", testNamespace, "--type=json", "-p",
				`[{"op": "replace", "path": "/spec/complianceType", "value": "musthave"},`+
					`{"op": "replace", "path": "/spec/remediationAction", "value": "enforce"}]`)

			By("Waiting for a CRD to appear, which should indicate the operator is installing")
			Eventually(func(ctx SpecContext) *unstructured.Unstructured {
				crd, _ := targetK8sDynamic.Resource(gvrCRD).Get(ctx,
					"quayregistries.quay.redhat.com", metav1.GetOptions{})

				return crd
			}, olmWaitTimeout, 5, ctx).ShouldNot(BeNil())

			checkCompliance(opPolName, testNamespace, olmWaitTimeout, policyv1.Compliant)

			check(
				opPolName,
				false,
				[]policyv1.RelatedObject{{
					Object: policyv1.ObjectResource{
						Kind:       "CustomResourceDefinition",
						APIVersion: "apiextensions.k8s.io/v1",
						Metadata: policyv1.ObjectMetadata{
							Name: "quayregistries.quay.redhat.com",
						},
					},
					Compliant: "Compliant",
					Reason:    "Resource found as expected",
				}},
				metav1.Condition{
					Type:    "CustomResourceDefinitionCompliant",
					Status:  metav1.ConditionTrue,
					Reason:  "RelevantCRDFound",
					Message: "there are CRDs present for the operator",
				},
				"there are CRDs present for the operator",
			)

			By("Adding a finalizer to the CRD")
			KubectlTarget("patch", "crd", "quayregistries.quay.redhat.com", "--type=json", "-p",
				`[{"op": "add", "path": "/metadata/finalizers", "value": ["donutdelete"]}]`)
			// cleanup for this is handled in an AfterAll
		})
		It("Should become noncompliant because the CRD is not fully removed", func(ctx SpecContext) {
			utils.Kubectl("patch", "operatorpolicy", opPolName, "-n", testNamespace, "--type=json", "-p",
				`[{"op": "replace", "path": "/spec/complianceType", "value": "mustnothave"},`+
					`{"op": "replace", "path": "/spec/remediationAction", "value": "enforce"}]`)

			check(
				opPolName,
				false,
				[]policyv1.RelatedObject{{
					Object: policyv1.ObjectResource{
						Kind:       "CustomResourceDefinition",
						APIVersion: "apiextensions.k8s.io/v1",
						Metadata: policyv1.ObjectMetadata{
							Name: "quayregistries.quay.redhat.com",
						},
					},
					Compliant: "NonCompliant",
					Reason:    "The object is being deleted but has not been removed yet",
				}},
				metav1.Condition{
					Type:    "CustomResourceDefinitionCompliant",
					Status:  metav1.ConditionFalse,
					Reason:  "CustomResourceDefinitionDeleting",
					Message: "the CustomResourceDefinition has a deletion timestamp",
				},
				`the CustomResourceDefinition was deleted`,
			)
		})
		It("Should become compliant after the finalizer is removed", func(ctx SpecContext) {
			KubectlTarget("patch", "crd", "quayregistries.quay.redhat.com", "--type=json", "-p",
				`[{"op": "remove", "path": "/metadata/finalizers"}]`)

			check(
				opPolName,
				false,
				[]policyv1.RelatedObject{{
					Object: policyv1.ObjectResource{
						Kind:       "CustomResourceDefinition",
						APIVersion: "apiextensions.k8s.io/v1",
						Metadata: policyv1.ObjectMetadata{
							Name: "-",
						},
					},
					Compliant: "Inapplicable",
					Reason:    "No relevant CustomResourceDefinitions found",
				}},
				metav1.Condition{
					Type:    "CustomResourceDefinitionCompliant",
					Status:  metav1.ConditionTrue,
					Reason:  "RelevantCRDNotFound",
					Message: "no CRDs were found for the operator",
				},
				`the CustomResourceDefinition was deleted`,
			)

			checkCompliance(opPolName, testNamespace, eventuallyTimeout, policyv1.Compliant)
		})
	})
	Describe("Testing mustnothave behavior for an operator group that is different than the specified one", func() {
		const (
			opPolYAML = "../resources/case38_operator_install/operator-policy-with-group.yaml"
			opPolName = "oppol-with-group"
			subName   = "project-quay"
		)

		BeforeEach(func() {
			preFunc()

			createObjWithParent(parentPolicyYAML, parentPolicyName,
				opPolYAML, testNamespace, gvrPolicy, gvrOperatorPolicy)
		})

		It("should not report an operator group that does not match the spec", func() {
			// create the extra operator group
			KubectlTarget("apply", "-f", "../resources/case38_operator_install/incorrect-operator-group.yaml",
				"-n", opPolTestNS)
			// change the operator policy to mustnothave
			utils.Kubectl("patch", "operatorpolicy", opPolName, "-n", testNamespace, "--type=json", "-p",
				`[{"op": "replace", "path": "/spec/complianceType", "value": "mustnothave"}]`)

			check(
				opPolName,
				false,
				[]policyv1.RelatedObject{{
					Object: policyv1.ObjectResource{
						Kind:       "OperatorGroup",
						APIVersion: "operators.coreos.com/v1",
						Metadata: policyv1.ObjectMetadata{
							Namespace: opPolTestNS,
							Name:      "scoped-operator-group",
						},
					},
					Compliant: "Compliant",
					Reason:    "Resource not found as expected",
				}},
				metav1.Condition{
					Type:    "OperatorGroupCompliant",
					Status:  metav1.ConditionTrue,
					Reason:  "OperatorGroupNotPresent",
					Message: "the OperatorGroup is not present",
				},
				"the OperatorGroup is not present",
			)
		})
	})
	Describe("Test mustnothave message when the namespace does not exist", func() {
		const (
			opPolYAML = "../resources/case38_operator_install/operator-policy-no-group.yaml"
			opPolName = "oppol-no-group"
			subName   = "project-quay"
		)

		BeforeEach(func() {
			preFunc()

			createObjWithParent(parentPolicyYAML, parentPolicyName,
				opPolYAML, testNamespace, gvrPolicy, gvrOperatorPolicy)
		})

		It("should report compliant", func() {
			// change the subscription namespace, and the complianceType to mustnothave
			utils.Kubectl("patch", "operatorpolicy", opPolName, "-n", testNamespace, "--type=json", "-p",
				`[{"op": "replace", "path": "/spec/subscription/namespace", "value": "imaginaryfriend"},`+
					`{"op": "replace", "path": "/spec/complianceType", "value": "mustnothave"}]`)

			check(
				opPolName,
				false,
				[]policyv1.RelatedObject{},
				metav1.Condition{
					Type:    "ValidPolicySpec",
					Status:  metav1.ConditionTrue,
					Reason:  "PolicyValidated",
					Message: "the policy spec is valid",
				},
				"the policy spec is valid",
			)
			checkCompliance(opPolName, testNamespace, eventuallyTimeout, policyv1.Compliant)
		})
	})
	Describe("Testing mustnothave behavior of operator groups in DeleteIfUnused mode", Ordered, func() {
		const (
			opPolYAML = "../resources/case38_operator_install/operator-policy-mustnothave-any-version.yaml"
			otherYAML = "../resources/case38_operator_install/operator-policy-authorino.yaml"
			opPolName = "oppol-mustnothave"
			subName   = "project-quay"
		)

		BeforeEach(func() {
			preFunc()
			KubectlTarget("delete", "crd", "--selector=olm.managed=true")

			createObjWithParent(parentPolicyYAML, parentPolicyName,
				opPolYAML, testNamespace, gvrPolicy, gvrOperatorPolicy)
		})

		It("should delete the inferred operator group when there is only one subscription", func(ctx SpecContext) {
			// enforce it as a musthave in order to install the operator
			utils.Kubectl("patch", "operatorpolicy", opPolName, "-n", testNamespace, "--type=json", "-p",
				`[{"op": "replace", "path": "/spec/complianceType", "value": "musthave"},`+
					`{"op": "replace", "path": "/spec/remediationAction", "value": "enforce"},`+
					`{"op": "replace", "path": "/spec/removalBehavior/operatorGroups", "value": "DeleteIfUnused"}]`)

			By("Waiting for a CRD to appear, which should indicate the operator is installing.")
			Eventually(func(ctx SpecContext) *unstructured.Unstructured {
				crd, _ := targetK8sDynamic.Resource(gvrCRD).Get(ctx,
					"quayregistries.quay.redhat.com", metav1.GetOptions{})

				return crd
			}, olmWaitTimeout, 5, ctx).ShouldNot(BeNil())

			By("Waiting for the policy to become compliant, indicating the operator is installed")
			checkCompliance(opPolName, testNamespace, olmWaitTimeout, policyv1.Compliant)

			By("Verifying that an operator group exists")
			Eventually(func(g Gomega) []unstructured.Unstructured {
				list, err := targetK8sDynamic.Resource(gvrOperatorGroup).Namespace(opPolTestNS).
					List(ctx, metav1.ListOptions{})
				g.Expect(err).NotTo(HaveOccurred())

				return list.Items
			}, eventuallyTimeout, 3, ctx).ShouldNot(BeEmpty())

			// revert it to mustnothave
			utils.Kubectl("patch", "operatorpolicy", opPolName, "-n", testNamespace, "--type=json", "-p",
				`[{"op": "replace", "path": "/spec/complianceType", "value": "mustnothave"}]`)

			By("Verifying that the operator group was removed")
			Eventually(func(g Gomega) []unstructured.Unstructured {
				list, err := targetK8sDynamic.Resource(gvrOperatorGroup).Namespace(opPolTestNS).
					List(ctx, metav1.ListOptions{})
				g.Expect(err).NotTo(HaveOccurred())

				return list.Items
			}, eventuallyTimeout, 3, ctx).Should(BeEmpty())
		})

		It("should delete the specified operator group when there is only one subscription", func(ctx SpecContext) {
			// enforce it as a musthave in order to install the operator
			utils.Kubectl("patch", "operatorpolicy", opPolName, "-n", testNamespace, "--type=json", "-p",
				`[{"op": "replace", "path": "/spec/complianceType", "value": "musthave"},`+
					`{"op": "replace", "path": "/spec/remediationAction", "value": "enforce"},`+
					`{"op": "replace", "path": "/spec/removalBehavior/operatorGroups", "value": "DeleteIfUnused"},`+
					`{"op": "add", "path": "/spec/operatorGroup", "value": {"name": "scoped-operator-group", `+
					`"namespace": "operator-policy-testns", "targetNamespaces": ["operator-policy-testns"]}}]`)

			By("Waiting for a CRD to appear, which should indicate the operator is installing.")
			Eventually(func(ctx SpecContext) *unstructured.Unstructured {
				crd, _ := targetK8sDynamic.Resource(gvrCRD).Get(ctx,
					"quayregistries.quay.redhat.com", metav1.GetOptions{})

				return crd
			}, olmWaitTimeout, 5, ctx).ShouldNot(BeNil())

			By("Waiting for the policy to become compliant, indicating the operator is installed")
			checkCompliance(opPolName, testNamespace, olmWaitTimeout, policyv1.Compliant)

			By("Verifying that an operator group exists")
			Eventually(func(g Gomega) []unstructured.Unstructured {
				list, err := targetK8sDynamic.Resource(gvrOperatorGroup).Namespace(opPolTestNS).
					List(ctx, metav1.ListOptions{})
				g.Expect(err).NotTo(HaveOccurred())

				return list.Items
			}, eventuallyTimeout, 3, ctx).ShouldNot(BeEmpty())

			// revert it to mustnothave
			utils.Kubectl("patch", "operatorpolicy", opPolName, "-n", testNamespace, "--type=json", "-p",
				`[{"op": "replace", "path": "/spec/complianceType", "value": "mustnothave"}]`)

			By("Verifying that the operator group was removed")
			Eventually(func(g Gomega) []unstructured.Unstructured {
				list, err := targetK8sDynamic.Resource(gvrOperatorGroup).Namespace(opPolTestNS).
					List(ctx, metav1.ListOptions{})
				g.Expect(err).NotTo(HaveOccurred())

				return list.Items
			}, eventuallyTimeout, 3, ctx).Should(BeEmpty())
		})

		It("should keep the specified operator group when it is owned by something", func(ctx SpecContext) {
			// enforce it as a musthave in order to install the operator
			utils.Kubectl("patch", "operatorpolicy", opPolName, "-n", testNamespace, "--type=json", "-p",
				`[{"op": "replace", "path": "/spec/complianceType", "value": "musthave"},`+
					`{"op": "replace", "path": "/spec/remediationAction", "value": "enforce"},`+
					`{"op": "replace", "path": "/spec/removalBehavior/operatorGroups", "value": "DeleteIfUnused"},`+
					`{"op": "add", "path": "/spec/operatorGroup", "value": {"name": "scoped-operator-group", `+
					`"namespace": "operator-policy-testns", "targetNamespaces": ["operator-policy-testns"]}}]`)

			By("Waiting for a CRD to appear, which should indicate the operator is installing.")
			Eventually(func(ctx SpecContext) *unstructured.Unstructured {
				crd, _ := targetK8sDynamic.Resource(gvrCRD).Get(ctx,
					"quayregistries.quay.redhat.com", metav1.GetOptions{})

				return crd
			}, olmWaitTimeout, 5, ctx).ShouldNot(BeNil())

			By("Waiting for the policy to become compliant, indicating the operator is installed")
			checkCompliance(opPolName, testNamespace, olmWaitTimeout, policyv1.Compliant)

			By("Verifying that an operator group exists")
			Eventually(func(g Gomega) []unstructured.Unstructured {
				list, err := targetK8sDynamic.Resource(gvrOperatorGroup).Namespace(opPolTestNS).
					List(ctx, metav1.ListOptions{})
				g.Expect(err).NotTo(HaveOccurred())

				return list.Items
			}, eventuallyTimeout, 3, ctx).ShouldNot(BeEmpty())

			By("Creating and setting an owner for the operator group")
			KubectlTarget("create", "configmap", "ownercm", "-n", opPolTestNS, "--from-literal=foo=bar")

			ownerCM := utils.GetWithTimeout(targetK8sDynamic, gvrConfigMap, "ownercm",
				opPolTestNS, true, eventuallyTimeout)
			ownerUID := string(ownerCM.GetUID())
			Expect(ownerUID).NotTo(BeEmpty())

			KubectlTarget("patch", "operatorgroup", "scoped-operator-group", "-n", opPolTestNS, "--type=json", "-p",
				`[{"op": "add", "path": "/metadata/ownerReferences", "value": [{"apiVersion": "v1",
				"kind": "ConfigMap", "name": "ownercm", "uid": "`+ownerUID+`"}]}]`)

			// revert it to mustnothave
			utils.Kubectl("patch", "operatorpolicy", opPolName, "-n", testNamespace, "--type=json", "-p",
				`[{"op": "replace", "path": "/spec/complianceType", "value": "mustnothave"}]`)

			By("Verifying the operator group was not removed")
			Consistently(func(g Gomega) []unstructured.Unstructured {
				list, err := targetK8sDynamic.Resource(gvrOperatorGroup).Namespace(opPolTestNS).
					List(ctx, metav1.ListOptions{})
				g.Expect(err).NotTo(HaveOccurred())

				return list.Items
			}, consistentlyDuration, 3, ctx).ShouldNot(BeEmpty())
		})

		It("should not delete the inferred operator group when there is another subscription", func(ctx SpecContext) {
			// enforce it as a musthave in order to install the operator
			utils.Kubectl("patch", "operatorpolicy", opPolName, "-n", testNamespace, "--type=json", "-p",
				`[{"op": "replace", "path": "/spec/complianceType", "value": "musthave"},`+
					`{"op": "replace", "path": "/spec/remediationAction", "value": "enforce"},`+
					`{"op": "replace", "path": "/spec/removalBehavior/operatorGroups", "value": "DeleteIfUnused"}]`)

			By("Waiting for a CRD to appear, which should indicate the operator is installing.")
			Eventually(func(ctx SpecContext) *unstructured.Unstructured {
				crd, _ := targetK8sDynamic.Resource(gvrCRD).Get(ctx,
					"quayregistries.quay.redhat.com", metav1.GetOptions{})

				return crd
			}, olmWaitTimeout, 5, ctx).ShouldNot(BeNil())

			By("Waiting for the policy to become compliant, indicating the operator is installed")
			checkCompliance(opPolName, testNamespace, olmWaitTimeout, policyv1.Compliant)

			By("Verifying that an operator group exists")
			Eventually(func(g Gomega) []unstructured.Unstructured {
				list, err := targetK8sDynamic.Resource(gvrOperatorGroup).Namespace(opPolTestNS).
					List(ctx, metav1.ListOptions{})
				g.Expect(err).NotTo(HaveOccurred())

				return list.Items
			}, eventuallyTimeout, 3, ctx).ShouldNot(BeEmpty())

			By("Creating another operator policy in the namespace")
			createObjWithParent(parentPolicyYAML, parentPolicyName,
				otherYAML, testNamespace, gvrPolicy, gvrOperatorPolicy)

			// enforce the other policy
			utils.Kubectl("patch", "operatorpolicy", "oppol-authorino", "-n", testNamespace, "--type=json", "-p",
				`[{"op": "replace", "path": "/spec/remediationAction", "value": "enforce"}]`)

			By("Waiting for the policy to become compliant, indicating the operator is installed")
			checkCompliance("oppol-authorino", testNamespace, olmWaitTimeout, policyv1.Compliant)

			// revert main policy to mustnothave
			utils.Kubectl("patch", "operatorpolicy", opPolName, "-n", testNamespace, "--type=json", "-p",
				`[{"op": "replace", "path": "/spec/complianceType", "value": "mustnothave"}]`)

			By("Verifying the operator group was not removed")
			Consistently(func(g Gomega) []unstructured.Unstructured {
				list, err := targetK8sDynamic.Resource(gvrOperatorGroup).Namespace(opPolTestNS).
					List(ctx, metav1.ListOptions{})
				g.Expect(err).NotTo(HaveOccurred())

				return list.Items
			}, consistentlyDuration, 3, ctx).ShouldNot(BeEmpty())
		})
	})
	Describe("Testing defaulted values of removalBehavior in an OperatorPolicy", func() {
		const (
			opPolYAML = "../resources/case38_operator_install/operator-policy-no-group.yaml"
			opPolName = "oppol-no-group"
		)

		BeforeEach(func() {
			preFunc()

			createObjWithParent(parentPolicyYAML, parentPolicyName,
				opPolYAML, testNamespace, gvrPolicy, gvrOperatorPolicy)
		})

		It("Should have applied defaults to the removalBehavior field", func(ctx SpecContext) {
			policy, err := clientManagedDynamic.Resource(gvrOperatorPolicy).Namespace(testNamespace).
				Get(ctx, opPolName, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())
			Expect(policy).NotTo(BeNil())

			remBehavior, found, err := unstructured.NestedStringMap(policy.Object, "spec", "removalBehavior")
			Expect(found).To(BeTrue())
			Expect(err).NotTo(HaveOccurred())

			Expect(remBehavior).To(HaveKeyWithValue("operatorGroups", "DeleteIfUnused"))
			Expect(remBehavior).To(HaveKeyWithValue("subscriptions", "Delete"))
			Expect(remBehavior).To(HaveKeyWithValue("clusterServiceVersions", "Delete"))
			Expect(remBehavior).To(HaveKeyWithValue("customResourceDefinitions", "Keep"))
		})
	})
	Describe("Testing defaulted values of ComplianceConfig in an OperatorPolicy", func() {
		const (
			opPolYAML = "../resources/case38_operator_install/operator-policy-no-group.yaml"
			opPolName = "oppol-no-group"
		)

		BeforeEach(func() {
			preFunc()

			createObjWithParent(parentPolicyYAML, parentPolicyName,
				opPolYAML, testNamespace, gvrPolicy, gvrOperatorPolicy)
		})

		It("Should have applied defaults to the ComplianceConfig field", func(ctx SpecContext) {
			policy, err := clientManagedDynamic.Resource(gvrOperatorPolicy).Namespace(testNamespace).
				Get(ctx, opPolName, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())
			Expect(policy).NotTo(BeNil())

			complianceConfig, found, err := unstructured.NestedStringMap(policy.Object, "spec", "complianceConfig")
			Expect(found).To(BeTrue())
			Expect(err).NotTo(HaveOccurred())

			Expect(complianceConfig).To(HaveKeyWithValue("catalogSourceUnhealthy", "Compliant"))
			Expect(complianceConfig).To(HaveKeyWithValue("deploymentsUnavailable", "NonCompliant"))
			Expect(complianceConfig).To(HaveKeyWithValue("upgradesAvailable", "Compliant"))
		})
	})
	Describe("Testing operator policies that specify the same subscription", Ordered, func() {
		const (
			musthaveYAML    = "../resources/case38_operator_install/operator-policy-no-group.yaml"
			musthaveName    = "oppol-no-group"
			mustnothaveYAML = "../resources/case38_operator_install/operator-policy-mustnothave-any-version.yaml"
			mustnothaveName = "oppol-mustnothave"
		)

		BeforeAll(func() {
			preFunc()

			createObjWithParent(parentPolicyYAML, parentPolicyName,
				musthaveYAML, testNamespace, gvrPolicy, gvrOperatorPolicy)

			createObjWithParent(parentPolicyYAML, parentPolicyName,
				mustnothaveYAML, testNamespace, gvrPolicy, gvrOperatorPolicy)
		})

		It("Should not display a validation error when both are in inform mode", func() {
			check(
				mustnothaveName,
				false,
				[]policyv1.RelatedObject{},
				metav1.Condition{
					Type:    "ValidPolicySpec",
					Status:  metav1.ConditionTrue,
					Reason:  "PolicyValidated",
					Message: `the policy spec is valid`,
				},
				`the policy spec is valid`,
			)
			check(
				musthaveName,
				false,
				[]policyv1.RelatedObject{},
				metav1.Condition{
					Type:    "ValidPolicySpec",
					Status:  metav1.ConditionTrue,
					Reason:  "PolicyValidated",
					Message: `the policy spec is valid`,
				},
				`the policy spec is valid`,
			)
		})

		// This test requires that no other OperatorPolicies are active. Ideally it would be marked
		// with the Serial decorator, but then this whole file would need to be marked that way,
		// which would slow down the suite. As long as no other test files use OperatorPolicy, the
		// Ordered property on this file should ensure this is stable.
		It("Should not cause an infinite reconcile loop", func() {
			recMetrics := utils.GetMetrics("controller_runtime_reconcile_total",
				`controller=\"operator-policy-controller\"`)

			totalReconciles := 0
			for _, metric := range recMetrics {
				val, err := strconv.Atoi(metric)
				Expect(err).NotTo(HaveOccurred())

				totalReconciles += val
			}

			Consistently(func(g Gomega) int {
				loopMetrics := utils.GetMetrics("controller_runtime_reconcile_total",
					`controller=\"operator-policy-controller\"`)

				loopReconciles := 0
				for _, metric := range loopMetrics {
					val, err := strconv.Atoi(metric)
					g.Expect(err).NotTo(HaveOccurred())

					loopReconciles += val
				}

				return loopReconciles
			}, "10s", "1s").Should(Equal(totalReconciles))
		})

		It("Should display a validation error when both are enforced", func() {
			// enforce the policies
			utils.Kubectl("patch", "operatorpolicy", mustnothaveName, "-n", testNamespace, "--type=json", "-p",
				`[{"op": "replace", "path": "/spec/remediationAction", "value": "enforce"}]`)
			utils.Kubectl("patch", "operatorpolicy", musthaveName, "-n", testNamespace, "--type=json", "-p",
				`[{"op": "replace", "path": "/spec/remediationAction", "value": "enforce"}]`)

			check(
				mustnothaveName,
				true,
				[]policyv1.RelatedObject{},
				metav1.Condition{
					Type:   "ValidPolicySpec",
					Status: metav1.ConditionFalse,
					Reason: "InvalidPolicySpec",
					Message: `the specified operator is managed by multiple enforced policies ` +
						`(oppol-mustnothave.` + testNamespace + `, oppol-no-group.` + testNamespace + `)`,
				},
				`the specified operator is managed by multiple enforced policies`,
			)
			check(
				musthaveName,
				true,
				[]policyv1.RelatedObject{},
				metav1.Condition{
					Type:   "ValidPolicySpec",
					Status: metav1.ConditionFalse,
					Reason: "InvalidPolicySpec",
					Message: `the specified operator is managed by multiple enforced policies ` +
						`(oppol-mustnothave.` + testNamespace + `, oppol-no-group.` + testNamespace + `)`,
				},
				`the specified operator is managed by multiple enforced policies`,
			)
		})

		// This test requires that no other OperatorPolicies are active. Ideally it would be marked
		// with the Serial decorator, but then this whole file would need to be marked that way,
		// which would slow down the suite. As long as no other test files use OperatorPolicy, the
		// Ordered property on this file should ensure this is stable.
		It("Should not cause an infinite reconcile loop when enforced", func() {
			check(
				mustnothaveName,
				true,
				[]policyv1.RelatedObject{},
				metav1.Condition{
					Type:   "ValidPolicySpec",
					Status: metav1.ConditionFalse,
					Reason: "InvalidPolicySpec",
					Message: `the specified operator is managed by multiple enforced policies ` +
						`(oppol-mustnothave.` + testNamespace + `, oppol-no-group.` + testNamespace + `)`,
				},
				`the specified operator is managed by multiple enforced policies`,
			)

			recMetrics := utils.GetMetrics("controller_runtime_reconcile_total",
				`controller=\"operator-policy-controller\"`)

			totalReconciles := 0
			for _, metric := range recMetrics {
				val, err := strconv.Atoi(metric)
				Expect(err).NotTo(HaveOccurred())

				totalReconciles += val
			}

			Consistently(func(g Gomega) int {
				loopMetrics := utils.GetMetrics("controller_runtime_reconcile_total",
					`controller=\"operator-policy-controller\"`)

				loopReconciles := 0
				for _, metric := range loopMetrics {
					val, err := strconv.Atoi(metric)
					g.Expect(err).NotTo(HaveOccurred())

					loopReconciles += val
				}

				return loopReconciles
			}, "10s", "1s").Should(Equal(totalReconciles))
		})

		It("Should remove the validation error when an overlapping policy is removed", func() {
			utils.KubectlDelete("operatorpolicy", musthaveName, "-n", testNamespace)

			check(
				mustnothaveName,
				false,
				[]policyv1.RelatedObject{},
				metav1.Condition{
					Type:    "ValidPolicySpec",
					Status:  metav1.ConditionTrue,
					Reason:  "PolicyValidated",
					Message: `the policy spec is valid`,
				},
				`the policy spec is valid`,
			)
		})
	})
	Describe("Testing templates in an OperatorPolicy", Ordered, func() {
		const (
			opPolYAML     = "../resources/case38_operator_install/operator-policy-with-templates.yaml"
			opPolName     = "oppol-with-templates"
			configmapYAML = "../resources/case38_operator_install/template-configmap.yaml"
			opGroupName   = "scoped-operator-group"
			subName       = "project-quay"
		)

		BeforeAll(func() {
			preFunc()

			KubectlTarget("apply", "-f", configmapYAML, "-n", opPolTestNS)

			createObjWithParent(parentPolicyYAML, parentPolicyName,
				opPolYAML, testNamespace, gvrPolicy, gvrOperatorPolicy)
		})

		It("Should lookup values for operator policy resources when enforced", func(ctx SpecContext) {
			// enforce the policy
			utils.Kubectl("patch", "operatorpolicy", opPolName, "-n", testNamespace, "--type=json", "-p",
				`[{"op": "replace", "path": "/spec/remediationAction", "value": "enforce"}]`)

			check(
				opPolName,
				false,
				[]policyv1.RelatedObject{{
					Object: policyv1.ObjectResource{
						Kind:       "OperatorGroup",
						APIVersion: "operators.coreos.com/v1",
						Metadata: policyv1.ObjectMetadata{
							Name:      opGroupName,
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
				"the OperatorGroup required by the policy was created",
			)

			By("Verifying the targetNamespaces in the OperatorGroup")
			og, err := targetK8sDynamic.Resource(gvrOperatorGroup).Namespace(opPolTestNS).
				Get(ctx, opGroupName, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())
			Expect(og).NotTo(BeNil())

			targetNamespaces, found, err := unstructured.NestedStringSlice(og.Object, "spec", "targetNamespaces")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(targetNamespaces).To(HaveExactElements("foo", "bar", opPolTestNS))

			By("Verifying the Subscription channel")
			sub, err := targetK8sDynamic.Resource(gvrSubscription).Namespace(opPolTestNS).
				Get(ctx, subName, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())
			Expect(sub).NotTo(BeNil())

			channel, found, err := unstructured.NestedString(sub.Object, "spec", "channel")
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeTrue())
			Expect(channel).To(Equal("fakechannel"))
		})

		It("Should update the subscription after the configmap is updated", func(ctx SpecContext) {
			KubectlTarget("patch", "configmap", "op-config", "-n", opPolTestNS, "--type=json", "-p",
				`[{"op": "replace", "path": "/data/channel", "value": "stable-3.10"}]`)

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
	Describe("Testing recovery of sub-csv connection", Ordered, func() {
		const (
			opPolYAML = "../resources/case38_operator_install/operator-policy-no-group-enforce.yaml"
			opPolName = "oppol-no-group-enforce"
			subName   = "project-quay"
		)

		scenarioTriggered := true

		BeforeAll(func() {
			preFunc()

			createObjWithParent(parentPolicyYAML, parentPolicyName,
				opPolYAML, testNamespace, gvrPolicy, gvrOperatorPolicy)
		})

		BeforeEach(func() {
			if !scenarioTriggered {
				Skip("test scenario was unable to be triggered")
			}
		})

		It("should get the 'csv exists and is not referenced' condition", func(ctx SpecContext) {
			scenarioTriggered = false

			By("Verifying the policy starts compliant")
			checkCompliance(opPolName, testNamespace, olmWaitTimeout*2, policyv1.Compliant)

			By("Periodically deleting the subscription and checking the status")
			scenarioDeadline := time.Now().Add(40 * time.Second)

		scenarioTriggerLoop:
			for scenarioDeadline.After(time.Now()) {
				KubectlTarget("delete", "subscription", subName, "-n", opPolTestNS)
				time.Sleep(time.Second)

				sub, err := targetK8sDynamic.Resource(gvrSubscription).Namespace(opPolTestNS).
					Get(ctx, subName, metav1.GetOptions{})
				if err != nil || sub == nil {
					continue
				}

				subConds, _, _ := unstructured.NestedSlice(sub.Object, "status", "conditions")
				for _, cond := range subConds {
					condMap, ok := cond.(map[string]interface{})
					if !ok {
						continue
					}

					if condType, _, _ := unstructured.NestedString(condMap, "type"); condType != "ResolutionFailed" {
						continue
					}

					if condStatus, _, _ := unstructured.NestedString(condMap, "status"); condStatus != "True" {
						continue
					}

					condMessage, _, _ := unstructured.NestedString(condMap, "message")
					notRefRgx := regexp.MustCompile(`clusterserviceversion (\S*) exists and is not referenced`)
					if notRefRgx.MatchString(condMessage) {
						scenarioTriggered = true

						break scenarioTriggerLoop
					}
				}

				time.Sleep(5 * time.Second)
			}
		})

		It("Verifies the policy eventually fixes the 'not referenced' condition", func() {
			By("Sleeping 25s, since OperatorPolicy should wait a while before intervening")
			time.Sleep(25 * time.Second)

			By("Verifying the policy becomes compliant")
			checkCompliance(opPolName, testNamespace, 2*olmWaitTimeout, policyv1.Compliant, 30, 3)
		})
	})

	Describe("Testing approving an InstallPlan with multiple CSVs", Ordered, func() {
		const (
			opPolArgoCDYAML   = "../resources/case38_operator_install/operator-policy-argocd.yaml"
			opPolKafkaYAML    = "../resources/case38_operator_install/operator-policy-strimzi-kafka-operator.yaml"
			subscriptionsYAML = "../resources/case38_operator_install/multiple-subscriptions.yaml"
		)

		BeforeAll(func() {
			preFunc()
		})

		It("OperatorPolicy can approve an InstallPlan with two CSVs", func(ctx SpecContext) {
			By("Creating two Subscriptions to generate an InstallPlan with two CSVs")
			KubectlTarget("-n", opPolTestNS, "create", "-f", subscriptionsYAML)

			Eventually(func(g Gomega) {
				var installPlans *unstructured.UnstructuredList

				// Wait up to 10 seconds for the InstallPlan to appear
				g.Eventually(func(g Gomega) {
					var err error
					installPlans, err = targetK8sDynamic.Resource(gvrInstallPlan).Namespace(opPolTestNS).List(
						ctx, metav1.ListOptions{},
					)

					g.Expect(err).ToNot(HaveOccurred())
					// OLM often creates duplicate InstallPlans so account for that.
					g.Expect(
						len(installPlans.Items) == 1 || len(installPlans.Items) == 2).To(BeTrue(),
						"expected 1 or 2 InstallPlans",
					)
				}, 10, 1).Should(Succeed())

				csvNames, _, _ := unstructured.NestedStringSlice(
					installPlans.Items[0].Object, "spec", "clusterServiceVersionNames",
				)

				if len(csvNames) != 2 {
					KubectlTarget("-n", opPolTestNS, "delete", "-f", subscriptionsYAML, "--ignore-not-found")
					KubectlTarget("-n", opPolTestNS, "delete", "installplans", "--all")
					KubectlTarget("-n", opPolTestNS, "create", "-f", subscriptionsYAML)
				}

				g.Expect(csvNames).To(ConsistOf("argocd-operator.v0.9.1", "strimzi-cluster-operator.v0.35.0"))
			}, olmWaitTimeout, 1).Should(Succeed())

			By("Creating an OperatorPolicy to adopt the argocd-operator Subscription")
			createObjWithParent(
				parentPolicyYAML, parentPolicyName, opPolArgoCDYAML, testNamespace, gvrPolicy, gvrOperatorPolicy,
			)

			By("Checking the OperatorPolicy InstallPlan is not approved because of strimzi-cluster-operator.v0.35.0")
			check(
				"argocd-operator",
				true,
				[]policyv1.RelatedObject{{
					Object: policyv1.ObjectResource{
						Kind:       "InstallPlan",
						APIVersion: "operators.coreos.com/v1alpha1",
						Metadata: policyv1.ObjectMetadata{
							Namespace: opPolTestNS,
						},
					},
					Compliant: "Compliant",
					Reason:    "The InstallPlan is RequiresApproval",
				}},
				metav1.Condition{
					Type:   "InstallPlanCompliant",
					Status: metav1.ConditionTrue,
					Reason: "InstallPlanRequiresApproval",
					Message: "an InstallPlan to update to [argocd-operator.v0.9.1, " +
						"strimzi-cluster-operator.v0.35.0] " +
						"is available for approval but approval for [strimzi-cluster-operator.v0.35.0] is required",
				},
				"an InstallPlan to update to .* is available for approval",
			)

			By("Creating an OperatorPolicy to adopt the strimzi-cluster-operator Subscription")
			createObjWithParent(
				parentPolicyYAML, parentPolicyName, opPolKafkaYAML, testNamespace, gvrPolicy, gvrOperatorPolicy,
			)

			By("Verifying the initial installPlan for startingCSV was approved")
			Eventually(func(g Gomega) {
				installPlans, err := targetK8sDynamic.Resource(gvrInstallPlan).Namespace(opPolTestNS).List(
					ctx, metav1.ListOptions{},
				)
				g.Expect(err).ToNot(HaveOccurred())

				var approvedInstallPlan bool

				// OLM often creates duplicate InstallPlans with the same generation. They are the same but OLM
				// concurrency can encounter race conditions, so just see that one of them is approved by
				// the policies.
				for _, installPlan := range installPlans.Items {
					csvNames, _, _ := unstructured.NestedStringSlice(
						installPlan.Object, "spec", "clusterServiceVersionNames",
					)

					slices.Sort(csvNames)

					if !reflect.DeepEqual(
						csvNames, []string{"argocd-operator.v0.9.1", "strimzi-cluster-operator.v0.35.0"},
					) {
						continue
					}

					approved, _, _ := unstructured.NestedBool(installPlan.Object, "spec", "approved")
					if approved {
						approvedInstallPlan = true

						break
					}
				}

				g.Expect(approvedInstallPlan).To(BeTrue(), "Expect an InstallPlan for startingCSV to be approved")
			}, olmWaitTimeout, 1).Should(Succeed())
		})
	})
	Describe("Test reporting of unapproved version after installation", func() {
		const (
			opPolYAML     = "../resources/case38_operator_install/operator-policy-no-group-enforce.yaml"
			opPolName     = "oppol-no-group-enforce"
			latestQuay310 = "quay-operator.v3.10.6"
		)

		BeforeEach(func() {
			preFunc()

			createObjWithParent(parentPolicyYAML, parentPolicyName,
				opPolYAML, testNamespace, gvrPolicy, gvrOperatorPolicy)
		})

		It("Should start compliant", func(ctx SpecContext) {
			Eventually(func(ctx SpecContext) string {
				csv, _ := targetK8sDynamic.Resource(gvrClusterServiceVersion).Namespace(opPolTestNS).
					Get(ctx, latestQuay310, metav1.GetOptions{})

				if csv == nil {
					return ""
				}

				reason, _, _ := unstructured.NestedString(csv.Object, "status", "reason")

				return reason
			}, olmWaitTimeout, 5, ctx).Should(Equal("InstallSucceeded"))

			check(
				opPolName,
				false,
				[]policyv1.RelatedObject{{
					Object: policyv1.ObjectResource{
						Kind:       "InstallPlan",
						APIVersion: "operators.coreos.com/v1alpha1",
						Metadata: policyv1.ObjectMetadata{
							Namespace: opPolTestNS,
						},
					},
					Compliant: "Compliant",
					Reason:    "The InstallPlan is Complete",
				}},
				metav1.Condition{
					Type:    "InstallPlanCompliant",
					Status:  metav1.ConditionTrue,
					Reason:  "NoInstallPlansRequiringApproval",
					Message: "no InstallPlans requiring approval were found",
				},
				"no InstallPlans requiring approval were found",
			)
		})
		It("Should report a violation after the versions list is patched to exclude the current version", func() {
			By("Patching the versions field to exclude the installed version")
			utils.Kubectl("patch", "operatorpolicy", opPolName, "-n", testNamespace, "--type=json", "-p",
				`[{"op": "replace", "path": "/spec/versions", "value": ["pie.v3.14159"]}]`)

			check(
				opPolName,
				false,
				[]policyv1.RelatedObject{{
					Object: policyv1.ObjectResource{
						Kind:       "ClusterServiceVersion",
						APIVersion: "operators.coreos.com/v1alpha1",
						Metadata: policyv1.ObjectMetadata{
							Namespace: opPolTestNS,
							Name:      latestQuay310,
						},
					},
					Compliant: "NonCompliant",
					Reason:    "ClusterServiceVersion (" + latestQuay310 + ") is not an approved version",
				}},
				metav1.Condition{
					Type:    "ClusterServiceVersionCompliant",
					Status:  metav1.ConditionFalse,
					Reason:  "UnapprovedVersion",
					Message: "ClusterServiceVersion (" + latestQuay310 + ") is not an approved version",
				},
				"ClusterServiceVersion .* is not an approved version",
			)
		})
	})
})
