package controllers

import (
	"fmt"
	"os"
	"strings"
	"testing"

	operatorv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/yaml"

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
					"startingCSV": "my-operator-v1"
				}`),
			},
			UpgradeApproval: "None",
		},
	}
	desiredGVK := schema.GroupVersionKind{
		Group:   "operators.coreos.com",
		Version: "v1alpha1",
		Kind:    "Subscription",
	}

	// Check values are correctly bootstrapped to the Subscription
	ret, err := buildSubscription(testPolicy, nil)
	assert.Equal(t, err, nil)
	assert.Equal(t, ret.GroupVersionKind(), desiredGVK)
	assert.Equal(t, ret.ObjectMeta.Name, "my-operator")
	assert.Equal(t, ret.ObjectMeta.Namespace, "default")
	assert.Equal(t, ret.Spec.InstallPlanApproval, operatorv1alpha1.ApprovalManual)
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
								"startingCSV": "my-operator-v1"
							}`),
						},
						UpgradeApproval: "None",
					},
				}

				// Check values are correctly bootstrapped to the Subscription
				_, err := buildSubscription(testPolicy, nil)
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
					"startingCSV": "my-operator-v1"
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
	ret, err := buildOperatorGroup(testPolicy, "my-operators", nil)
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

func TestGetApprovedCSVs(t *testing.T) {
	odfIPRaw, err := os.ReadFile("../test/resources/unit/odf-installplan.yaml")
	if err != nil {
		t.Fatalf("Encountered an error when reading the odf-installplan.yaml: %v", err)
	}

	odfIP := &operatorv1alpha1.InstallPlan{}

	err = yaml.Unmarshal(odfIPRaw, odfIP)
	if err != nil {
		t.Fatalf("Encountered an error when umarshaling the odf-installplan.yaml: %v", err)
	}

	policy := &policyv1beta1.OperatorPolicy{
		Spec: policyv1beta1.OperatorPolicySpec{
			UpgradeApproval:   "Automatic",
			RemediationAction: "enforce",
			Versions:          []string{"odf-operator.v4.16.3-rhodf"},
		},
	}

	subscription := &operatorv1alpha1.Subscription{
		Spec: &operatorv1alpha1.SubscriptionSpec{},
		Status: operatorv1alpha1.SubscriptionStatus{
			InstalledCSV: "odf-operator.v4.16.0-rhodf",
			CurrentCSV:   "odf-operator.v4.16.3-rhodf",
		},
	}

	csvs := getApprovedCSVs(policy, subscription, odfIP)

	expectedCSVs := sets.Set[string]{}
	expectedCSVs.Insert(odfIP.Spec.ClusterServiceVersionNames...)

	if !csvs.Equal(expectedCSVs) {
		t.Fatalf(
			"Expected all CSVs to be approved, but missing: %s",
			strings.Join(expectedCSVs.Difference(csvs).UnsortedList(), ", "),
		)
	}

	// Set versions to empty to approve all versions
	policy = &policyv1beta1.OperatorPolicy{
		Spec: policyv1beta1.OperatorPolicySpec{
			UpgradeApproval:   "Automatic",
			RemediationAction: "enforce",
			Versions:          []string{},
		},
	}

	csvs = getApprovedCSVs(policy, subscription, odfIP)

	if !csvs.Equal(expectedCSVs) {
		t.Fatalf(
			"Expected all CSVs to be approved, but missing: %s",
			strings.Join(expectedCSVs.Difference(csvs).UnsortedList(), ", "),
		)
	}

	// Set an old approved version
	policy = &policyv1beta1.OperatorPolicy{
		Spec: policyv1beta1.OperatorPolicySpec{
			UpgradeApproval:   "Automatic",
			RemediationAction: "enforce",
			Versions:          []string{"odf-operator.v4.16.0-rhodf"},
		},
	}

	csvs = getApprovedCSVs(policy, subscription, odfIP)
	if len(csvs) != 0 {
		t.Fatalf("Expected no CSVs to be approved, but got: %s", strings.Join(csvs.UnsortedList(), ", "))
	}
}
