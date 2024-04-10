package controllers

import (
	"fmt"
	"testing"

	operatorv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	policyv1beta1 "open-cluster-management.io/config-policy-controller/api/v1beta1"
)

func TestBuildSubscription(t *testing.T) {
	testPolicy := &policyv1beta1.OperatorPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-policy",
			Namespace: "default",
		},
		Spec: policyv1beta1.OperatorPolicySpec{
			Severity:          "low",
			RemediationAction: "enforce",
			ComplianceType:    "musthave",
			Subscription: runtime.RawExtension{
				Raw: []byte(`{
					"namespace": "default",
					"source": "my-catalog",
					"sourceNamespace": "my-ns",
					"name": "my-operator",
					"channel": "stable",
					"startingCSV": "my-operator-v1",
					"installPlanApproval": "Automatic"
				}`),
			},
		},
	}
	desiredGVK := schema.GroupVersionKind{
		Group:   "operators.coreos.com",
		Version: "v1alpha1",
		Kind:    "Subscription",
	}

	// Check values are correctly bootstrapped to the Subscription
	ret, err := buildSubscription(testPolicy, "my-operators")
	assert.Equal(t, err, nil)
	assert.Equal(t, ret.GroupVersionKind(), desiredGVK)
	assert.Equal(t, ret.ObjectMeta.Name, "my-operator")
	assert.Equal(t, ret.ObjectMeta.Namespace, "default")
}

func TestBuildSubscriptionInvalidNames(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		expected string
	}{
		{
			name:     "",
			expected: "name is required in spec.subscription",
		},
		{
			name: "wrong$s",
			expected: "the name 'wrong$s' used for the subscription is invalid: a lowercase RFC 1123 label must " +
				"consist of lower case alphanumeric characters or '-', and must start and end with an alphanumeric " +
				"character (e.g. 'my-name',  or '123-abc', regex used for validation is " +
				"'[a-z0-9]([-a-z0-9]*[a-z0-9])?')",
		},
	}

	for _, test := range testCases {
		test := test

		t.Run(
			"name="+test.name,
			func(t *testing.T) {
				t.Parallel()

				testPolicy := &policyv1beta1.OperatorPolicy{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "my-policy",
						Namespace: "default",
					},
					Spec: policyv1beta1.OperatorPolicySpec{
						Severity:          "low",
						RemediationAction: "enforce",
						ComplianceType:    "musthave",
						Subscription: runtime.RawExtension{
							Raw: []byte(`{
								"namespace": "default",
								"source": "my-catalog",
								"sourceNamespace": "my-ns",
								"name": "` + test.name + `",
								"channel": "stable",
								"startingCSV": "my-operator-v1",
								"installPlanApproval": "Automatic"
							}`),
						},
					},
				}

				// Check values are correctly bootstrapped to the Subscription
				_, err := buildSubscription(testPolicy, "my-operators")
				assert.Equal(t, err.Error(), test.expected)
			},
		)
	}
}

func TestBuildOperatorGroup(t *testing.T) {
	testPolicy := &policyv1beta1.OperatorPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-policy",
			Namespace: "default",
		},
		Spec: policyv1beta1.OperatorPolicySpec{
			Severity:          "low",
			RemediationAction: "enforce",
			ComplianceType:    "musthave",
			Subscription: runtime.RawExtension{
				Raw: []byte(`{
					"source": "my-catalog",
					"sourceNamespace": "my-ns",
					"name": "my-operator",
					"channel": "stable",
					"startingCSV": "my-operator-v1",
					"installPlanApproval": "Automatic"
				}`),
			},
		},
	}
	desiredGVK := schema.GroupVersionKind{
		Group:   "operators.coreos.com",
		Version: "v1",
		Kind:    "OperatorGroup",
	}

	// Ensure OperatorGroup values are populated correctly
	ret, err := buildOperatorGroup(testPolicy, "my-operators")
	assert.Equal(t, err, nil)
	assert.Equal(t, ret.GroupVersionKind(), desiredGVK)
	assert.Equal(t, ret.ObjectMeta.GetGenerateName(), "my-operators-")
	assert.Equal(t, ret.ObjectMeta.GetNamespace(), "my-operators")
}

func TestMessageIncludesSubscription(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		subscriptionName string
		packageName      string
		message          string
		expected         bool
	}{
		{
			subscriptionName: "quay-does-not-exist",
			packageName:      "quay-does-not-exist",
			message: "no operators found from catalog some-catalog in namespace default referenced by subscription " +
				"quay-does-not-exist",
			expected: true,
		},
		{
			subscriptionName: "quay",
			packageName:      "quay",
			message: "no operators found from catalog some-catalog in namespace default referenced by subscription " +
				"quay-operator-does-not-exist",
			expected: false,
		},
		{
			subscriptionName: "quay-does-not-exist",
			packageName:      "quay-does-not-exist",
			message: "no operators found in package quay-does-not-exist in the catalog referenced by subscription " +
				"quay-does-not-exist",
			expected: true,
		},
		{
			subscriptionName: "quay-does-not-exist",
			packageName:      "quay-does-not-exist",
			message: "no operators found in package quay-does-not-exist in the catalog referenced by subscription " +
				"quay-does-not-exist",
			expected: true,
		},
		{
			subscriptionName: "quay-does-not-exist",
			packageName:      "quay-does-not-exist",
			message: "no operators found in channel a channel of package quay-does-not-exist in the catalog " +
				"referenced by subscription quay-does-not-exist",
			expected: true,
		},
		{
			subscriptionName: "quay-does-not-exist",
			packageName:      "other",
			message: "no operators found in channel a channel of package quay-does-not-exist in the catalog " +
				"referenced by subscription quay-does-not-exist",
			expected: true,
		},
		{
			subscriptionName: "other",
			packageName:      "quay-does-not-exist",
			message: "no operators found in channel a channel of package quay-does-not-exist in the catalog " +
				"referenced by subscription quay-does-not-exist",
			expected: true,
		},
		{
			subscriptionName: "quay-does-not-exist",
			packageName:      "quay-does-not-exist",
			//nolint: dupword
			message: "no operators found with name quay-does-not-exist in channel channel of package " +
				" quay-does-not-exist in the catalog referenced by subscription quay-does-not-exist",
			expected: true,
		},
		{
			subscriptionName: "quay",
			packageName:      "quay",
			//nolint: dupword
			message: "no operators found with name quay-does-not-exist in channel channel of package " +
				" quay-does-not-exist in the catalog referenced by subscription quay-does-not-exist",
			expected: false,
		},
		{
			subscriptionName: "quay",
			packageName:      "quay",
			message:          "multiple name matches for status.installedCSV of subscription default/quay: quay.v123",
			expected:         true,
		},
		{
			subscriptionName: "quay",
			packageName:      "quay",
			message:          "multiple name matches for status.installedCSV of subscription some-ns/quay: quay.v123",
			expected:         false,
		},
	}

	for i, test := range testCases {
		test := test

		t.Run(
			fmt.Sprintf("test[%d]", i),
			func(t *testing.T) {
				t.Parallel()

				subscription := &operatorv1alpha1.Subscription{
					ObjectMeta: metav1.ObjectMeta{
						Name:      test.subscriptionName,
						Namespace: "default",
					},
					Spec: &operatorv1alpha1.SubscriptionSpec{
						Package: test.packageName,
					},
				}

				match, err := messageIncludesSubscription(subscription, test.message)
				assert.Equal(t, err, nil)
				assert.Equal(t, match, test.expected)
			},
		)
	}
}

func TestMessageContentOrderMatching(t *testing.T) {
	t.Parallel()

	testPolicy := &policyv1beta1.OperatorPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-policy",
			Namespace: "default",
		},
		Spec: policyv1beta1.OperatorPolicySpec{
			Severity:          "low",
			RemediationAction: "enforce",
			ComplianceType:    "musthave",
			Subscription: runtime.RawExtension{
				Raw: []byte(`{
					"source": "my-catalog",
					"sourceNamespace": "my-ns",
					"name": "my-operator",
					"channel": "stable",
					"startingCSV": "my-operator-v1",
					"installPlanApproval": "Automatic"
				}`),
			},
		},
		Status: policyv1beta1.OperatorPolicyStatus{
			ComplianceState: "NonCompliant",
			Conditions: []metav1.Condition{
				{
					Type:               "SubscriptionCompliant",
					Status:             "False",
					ObservedGeneration: 0,
					LastTransitionTime: metav1.Now(),
					Reason:             "ConstraintsNotSatisfiable",
					Message: "constraints not satisfiable: " +
						"no operators found in package gatekeeper-operator-product " +
						"in the catalog referenced by subscription gatekeeper-operator-product, " +
						"clusterserviceversion gatekeeper-operator-product.v3.11.1 exists " +
						"and is not referenced by a subscription, " +
						"subscription gatekeeper-operator-product4 exists",
				},
			},
		},
	}

	testCond := &operatorv1alpha1.SubscriptionCondition{
		Type:   "ResolutionFailed",
		Status: "True",
		Reason: "ConstraintsNotSatisfiable",
		Message: "constraints not satisfiable: " +
			"subscription gatekeeper-operator-product4 exists, " +
			"no operators found in package gatekeeper-operator-product " +
			"in the catalog referenced by subscription gatekeeper-operator-product, " +
			"clusterserviceversion gatekeeper-operator-product.v3.11.1 exists " +
			"and is not referenced by a subscription",
	}

	ret := constraintMessageMatch(testPolicy, testCond)
	assert.Equal(t, true, ret)
}
