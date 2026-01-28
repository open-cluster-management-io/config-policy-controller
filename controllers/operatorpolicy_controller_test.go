package controllers

import (
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	operatorv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/yaml"
	dynamicfake "k8s.io/client-go/dynamic/fake"

	policyv1beta1 "open-cluster-management.io/config-policy-controller/api/v1beta1"
)

func TestBuildResources_SubscriptionErrorUpdatesStatus(t *testing.T) {
	t.Parallel()

	r := &OperatorPolicyReconciler{}

	policy := &policyv1beta1.OperatorPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-policy",
			Namespace: "default",
			Annotations: map[string]string{
				"policy.open-cluster-management.io/disable-templates": "true",
			},
		},
		Spec: policyv1beta1.OperatorPolicySpec{
			Severity:          "low",
			RemediationAction: "inform",
			ComplianceType:    "musthave",
			Subscription: runtime.RawExtension{
				Raw: []byte(`{
					"namespace": "default",
					"source": "my-catalog",
					"sourceNamespace": "my-ns",
					"name": "",
					"channel": "stable"
				}`),
			},
			UpgradeApproval: "None",
		},
	}

	_, _, changed, returnedErr := r.buildResources(t.Context(), policy)
	assert.True(t, changed, "expected status to be updated")
	assert.NoError(t, returnedErr)

	_, cond := policy.Status.GetCondition(validPolicyConditionType)
	assert.Equal(t, metav1.ConditionFalse, cond.Status)
	assert.Equal(t, "InvalidPolicySpec", cond.Reason)
	assert.Equal(t, "name is required in spec.subscription", cond.Message)
}

func TestBuildResources_subInstallPlanApprovalErrorUpdatesStatus(t *testing.T) {
	t.Parallel()

	r := &OperatorPolicyReconciler{}

	policy := &policyv1beta1.OperatorPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-policy",
			Namespace: "default",
			Annotations: map[string]string{
				"policy.open-cluster-management.io/disable-templates": "true",
			},
		},
		Spec: policyv1beta1.OperatorPolicySpec{
			Severity:          "low",
			RemediationAction: "inform",
			ComplianceType:    "musthave",
			Subscription: runtime.RawExtension{
				Raw: []byte(`{
					"namespace": "default",
					"source": "my-catalog",
					"sourceNamespace": "my-ns",
					"name": "my-operator",
					"channel": "stable",
					"installPlanApproval": "ERR"
				}`),
			},
			UpgradeApproval: "None",
		},
	}

	_, _, changed, returnedErr := r.buildResources(t.Context(), policy)
	assert.True(t, changed, "expected status to be updated")
	assert.NoError(t, returnedErr)

	_, cond := policy.Status.GetCondition(validPolicyConditionType)
	assert.Equal(t, metav1.ConditionFalse, cond.Status)
	assert.Equal(t, "InvalidPolicySpec", cond.Reason)
	assert.Equal(t, "installPlanApproval is prohibited in spec.subscription", cond.Message)
}

func TestBuildResources_SubDefaultsPkgManifestNotFoundUpdatesStatusAndReturnsErr(t *testing.T) {
	// To test line 542
	t.Parallel()

	r := &OperatorPolicyReconciler{
		// Use a fake dynamic client without any objects so PackageManifest GET returns NotFound
		DynamicClient: dynamicfake.NewSimpleDynamicClient(runtime.NewScheme()),
	}

	policy := &policyv1beta1.OperatorPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-policy",
			Namespace: "default",
			Annotations: map[string]string{
				"policy.open-cluster-management.io/disable-templates": "true",
			},
		},
		Spec: policyv1beta1.OperatorPolicySpec{
			Severity:          "low",
			RemediationAction: "inform",
			ComplianceType:    "musthave",
			Subscription: runtime.RawExtension{
				// Omit namespace key entirely so buildSubscription succeeds with empty namespace,
				// and omit source/sourceNamespace so defaultsNeeded = true (triggers PackageManifest lookup).
				Raw: []byte(`{
					"name": "my-operator",
					"channel": "stable",
					"startingCSV": "my-operator-v1"
				}`),
			},
			UpgradeApproval: "None",
		},
	}

	sub, opGroup, changed, returnedErr := r.buildResources(t.Context(), policy)
	assert.True(t, changed, "expected status to be updated")
	assert.ErrorIs(t, returnedErr, ErrPackageManifest, "expected returned error to wrap ErrPackageManifest")

	_, cond := policy.Status.GetCondition(validPolicyConditionType)
	assert.Equal(t, metav1.ConditionFalse, cond.Status)
	assert.Equal(t, "InvalidPolicySpec", cond.Reason)
	assert.Equal(t,
		"the subscription defaults could not be determined because the PackageManifest was not found",
		cond.Message)
	assert.Nil(t, sub, "expected subscription to be nil")
	assert.Nil(t, opGroup, "expected operator group to be nil")
}

func TestBuildResources_OperatorGroupErrorUpdatesStatus(t *testing.T) {
	t.Parallel()

	r := &OperatorPolicyReconciler{}

	policy := &policyv1beta1.OperatorPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-policy",
			Namespace: "default",
			Annotations: map[string]string{
				"policy.open-cluster-management.io/disable-templates": "true",
			},
		},
		Spec: policyv1beta1.OperatorPolicySpec{
			Severity:          "low",
			RemediationAction: "inform",
			ComplianceType:    "musthave",
			Subscription: runtime.RawExtension{
				Raw: []byte(`{
					"namespace": "default",
					"source": "my-catalog",
					"sourceNamespace": "my-ns",
					"name": "my-operator",
					"channel": "stable"
				}`),
			},
			// Invalid operatorGroup (missing name) should trigger a validation error
			OperatorGroup: &runtime.RawExtension{
				Raw: []byte(`{}`),
			},
			UpgradeApproval: "None",
		},
	}

	_, _, changed, returnedErr := r.buildResources(t.Context(), policy)
	assert.True(t, changed, "expected status to be updated")
	assert.NoError(t, returnedErr)

	_, cond := policy.Status.GetCondition(validPolicyConditionType)
	assert.Equal(t, metav1.ConditionFalse, cond.Status)
	assert.Equal(t, "InvalidPolicySpec", cond.Reason)
	assert.Equal(t, "name is required in spec.operatorGroup", cond.Message)
}

func TestBuildResources_TemplateResolverCreationErrorUpdatesStatus(t *testing.T) {
	t.Parallel()

	r := &OperatorPolicyReconciler{
		DynamicWatcher: nil,
	}

	policy := &policyv1beta1.OperatorPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-policy",
			Namespace: "default",
		},
		Spec: policyv1beta1.OperatorPolicySpec{
			Severity:          "low",
			RemediationAction: "inform",
			ComplianceType:    "musthave",
			// Include an obviously bad template so template resolution would fail if reached.
			// In this test, resolver creation itself is expected to fail earlier.
			Versions: []string{"{{ .badTemplate "},
			Subscription: runtime.RawExtension{
				Raw: []byte(`{
					"namespace": "default",
					"name": "my-operator",
					"channel": "stable",
					"startingCSV": "my-operator-v1"
				}`),
			},
			UpgradeApproval: "None",
		},
	}

	sub, opGroup, changed, returnedErr := r.buildResources(t.Context(), policy)
	assert.True(t, changed, "expected status to be updated")
	assert.NoError(t, returnedErr)
	assert.Nil(t, sub, "expected subscription to be nil on early return")
	assert.Nil(t, opGroup, "expected operator group to be nil on early return")

	_, cond := policy.Status.GetCondition(validPolicyConditionType)
	assert.Equal(t, metav1.ConditionFalse, cond.Status)
	assert.Equal(t, "InvalidPolicySpec", cond.Reason)
	assert.Equal(t,
		"unable to create template resolver: could not resolve the version template: "+
			"failed to parse the template JSON string [\"{{ .badTemplate \"]: template: tmpl:1: "+
			"unterminated character constant", cond.Message)
}

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
	assert.NoError(t, err)
	assert.Equal(t, ret.GroupVersionKind(), desiredGVK)
	assert.Equal(t, "my-operator", ret.ObjectMeta.Name)
	assert.Equal(t, "default", ret.ObjectMeta.Namespace)
	assert.Equal(t, operatorv1alpha1.ApprovalManual, ret.Spec.InstallPlanApproval)
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
				assert.Equal(t, test.expected, err.Error())
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
	assert.NoError(t, err)
	assert.Equal(t, ret.GroupVersionKind(), desiredGVK)
	assert.Equal(t, "my-operators-", ret.ObjectMeta.GetGenerateName())
	assert.Equal(t, "my-operators", ret.ObjectMeta.GetNamespace())
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
				assert.NoError(t, err)
				assert.Equal(t, test.expected, match)
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

	csvs := getApprovedCSVs(t.Context(), policy, subscription, odfIP)

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

	csvs = getApprovedCSVs(t.Context(), policy, subscription, odfIP)

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

	csvs = getApprovedCSVs(t.Context(), policy, subscription, odfIP)
	if len(csvs) != 0 {
		t.Fatalf("Expected no CSVs to be approved, but got: %s", strings.Join(csvs.UnsortedList(), ", "))
	}
}

func TestGetApprovedCSVsWithPackageNameLookup(t *testing.T) {
	mtcOadpIPRaw, err := os.ReadFile("../test/resources/unit/mtc-oadp-installplan.yaml")
	if err != nil {
		t.Fatalf("Encountered an error when reading the mtc-oadp-installplan.yaml: %v", err)
	}

	installPlan := &operatorv1alpha1.InstallPlan{}

	err = yaml.Unmarshal(mtcOadpIPRaw, installPlan)
	if err != nil {
		t.Fatalf("Encountered an error when unmarshaling the mtc-oadp-installplan.yaml: %v", err)
	}

	policy := &policyv1beta1.OperatorPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name: "mtc-operator",
		},
		Spec: policyv1beta1.OperatorPolicySpec{
			RemediationAction: "enforce",
			ComplianceType:    "musthave",
			Severity:          "medium",
			UpgradeApproval:   "Automatic",
			Versions:          []string{"mtc-operator.v1.8.9"},
		},
	}

	subscription := &operatorv1alpha1.Subscription{
		Status: operatorv1alpha1.SubscriptionStatus{
			CurrentCSV: "mtc-operator.v1.8.9",
		},
	}

	csvs := getApprovedCSVs(t.Context(), policy, subscription, installPlan)

	expectedCSVs := sets.Set[string]{}
	expectedCSVs.Insert("mtc-operator.v1.8.9")
	expectedCSVs.Insert("oadp-operator.v1.5.0")

	if !csvs.Equal(expectedCSVs) {
		t.Fatalf(
			"Expected both MTC and OADP CSVs to be approved. Got: %s, Expected: %s",
			strings.Join(csvs.UnsortedList(), ", "),
			strings.Join(expectedCSVs.UnsortedList(), ", "),
		)
	}
}

func TestGetDependencyCSVs(t *testing.T) {
	happyPackageToCSV := map[string]string{
		"starting-operator":          "starting-operator.v7.8.9",
		"first-dependency-operator":  "primary-operator.v1.1.1",
		"second-dependency-operator": "secondary-operator.v2.2.2",
	}

	happyPackageDependencies := map[string]sets.Set[string]{
		"starting-operator.v7.8.9": func() sets.Set[string] {
			s := sets.New[string]()
			s.Insert("first-dependency-operator")

			return s
		}(),
		"primary-operator.v1.1.1": func() sets.Set[string] {
			s := sets.New[string]()
			s.Insert("second-dependency-operator")

			return s
		}(),
		"secondary-operator.v2.2.2": sets.New[string](),
	}

	packageDependenciesAlreadyInstalled := map[string]sets.Set[string]{
		"starting-operator.v7.8.9": func() sets.Set[string] {
			s := sets.New[string]()
			s.Insert("first-dependency-operator")

			return s
		}(),
	}
	dependencyCSVsAlreadyInstalled := map[string]string{
		"irrelevant-uninstalled-operator": "irrelevant-uninstalled-operator.v1.2.3",
	}

	expectedCSVs := sets.Set[string]{}

	dependencyCSVs := getDependencyCSVs("starting-operator.v7.8.9", nil, nil)
	if !dependencyCSVs.Equal(expectedCSVs) {
		t.Fatalf("Expected no dependency CSVs to be approved. Got: %s, Expected: %s",
			strings.Join(dependencyCSVs.UnsortedList(), ", "),
			strings.Join(expectedCSVs.UnsortedList(), ", "))
	}

	dependencyCSVs = getDependencyCSVs(
		"starting-operator.v7.8.9", dependencyCSVsAlreadyInstalled, packageDependenciesAlreadyInstalled)
	if !dependencyCSVs.Equal(expectedCSVs) {
		t.Fatalf("Expected no dependency CSVs to be approved. Got: %s, Expected: %s",
			strings.Join(dependencyCSVs.UnsortedList(), ", "),
			strings.Join(expectedCSVs.UnsortedList(), ", "))
	}

	dependencyCSVs = getDependencyCSVs("starting-operator.v7.8.9", happyPackageToCSV, happyPackageDependencies)

	expectedCSVs.Insert("primary-operator.v1.1.1")
	expectedCSVs.Insert("secondary-operator.v2.2.2")

	if !dependencyCSVs.Equal(expectedCSVs) {
		t.Fatalf(
			"Expected dependency CSVs to be approved. Got: %s, Expected: %s",
			strings.Join(dependencyCSVs.UnsortedList(), ", "),
			strings.Join(expectedCSVs.UnsortedList(), ", "))
	}
}

func TestCanonicalizeVersions(t *testing.T) {
	policy := &policyv1beta1.OperatorPolicy{
		Spec: policyv1beta1.OperatorPolicySpec{
			UpgradeApproval:   "Automatic",
			RemediationAction: "enforce",
			Versions: []string{
				"",
				" ",
				"foo",
				" bar ",
				"one,two,three",
				"four,",
				",, ,,five, ,",
				" , six , ",
			},
		},
	}

	expected := []string{
		"foo",
		"bar",
		"one",
		"two",
		"three",
		"four",
		"five",
		"six",
	}

	canonicalizeVersions(policy)

	if !reflect.DeepEqual(policy.Spec.Versions, expected) {
		t.Fatalf("Expected %v canonicalized versions, got %v", expected, policy.Spec.Versions)
	}
}
