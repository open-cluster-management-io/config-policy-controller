package controllers

import (
	"testing"

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
