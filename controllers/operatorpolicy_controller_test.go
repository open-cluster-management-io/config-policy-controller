package controllers

import (
	"testing"

	operatorv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
			Subscription: policyv1beta1.SubscriptionSpec{
				SubscriptionSpec: operatorv1alpha1.SubscriptionSpec{
					Channel:                "stable",
					Package:                "my-operator",
					InstallPlanApproval:    "Automatic",
					CatalogSource:          "my-catalog",
					CatalogSourceNamespace: "my-ns",
					StartingCSV:            "my-operator-v1",
				},
				Namespace: "default",
			},
		},
	}
	desiredGVK := schema.GroupVersionKind{
		Group:   "operators.coreos.com",
		Version: "v1alpha1",
		Kind:    "Subscription",
	}

	// Check values are correctly bootstrapped to the Subscription
	ret := buildSubscription(testPolicy)
	assert.Equal(t, ret.GroupVersionKind(), desiredGVK)
	assert.Equal(t, ret.ObjectMeta.Name, "my-operator")
	assert.Equal(t, ret.ObjectMeta.Namespace, "default")
	assert.Equal(t, ret.Spec, &testPolicy.Spec.Subscription.SubscriptionSpec)
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
			Subscription: policyv1beta1.SubscriptionSpec{
				SubscriptionSpec: operatorv1alpha1.SubscriptionSpec{
					Channel:                "stable",
					Package:                "my-operator",
					InstallPlanApproval:    "Automatic",
					CatalogSource:          "my-catalog",
					CatalogSourceNamespace: "my-ns",
					StartingCSV:            "my-operator-v1",
				},
				Namespace: "default",
			},
		},
	}
	desiredGVK := schema.GroupVersionKind{
		Group:   "operators.coreos.com",
		Version: "v1",
		Kind:    "OperatorGroup",
	}

	// Ensure OperatorGroup values are populated correctly
	ret := buildOperatorGroup(testPolicy)
	assert.Equal(t, ret.GroupVersionKind(), desiredGVK)
	assert.Equal(t, ret.ObjectMeta.GetName(), "my-operator-default-og")
	assert.Equal(t, ret.ObjectMeta.GetNamespace(), "default")
	assert.Equal(t, ret.Spec.TargetNamespaces, []string{})
}
