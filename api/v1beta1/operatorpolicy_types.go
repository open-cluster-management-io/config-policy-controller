// Copyright (c) 2021 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	policyv1 "open-cluster-management.io/config-policy-controller/api/v1"
)

// StatusConfigAction : StatusMessageOnly or NonCompliant
// +kubebuilder:validation:Enum=StatusMessageOnly;NonCompliant
type StatusConfigAction string

// RemovalAction : Keep, Delete, or DeleteIfUnused
// +kubebuilder:validation:Enum=Keep;Delete;DeleteIfUnused
type RemovalAction string

const (
	// StatusMessageOnly is a StatusConfigAction that only shows the status message
	StatusMessageOnly StatusConfigAction = "StatusMessageOnly"
	// NonCompliant is a StatusConfigAction that shows the status message and sets
	// the compliance to NonCompliant
	NonCompliant StatusConfigAction = "NonCompliant"
)

const (
	// Keep is a RemovalBehavior indicating that the controller may not delete a type
	Keep RemovalAction = "Keep"
	// Delete is a RemovalBehavior indicating that the controller may delete a type
	Delete RemovalAction = "Delete"
	// DeleteIfUnused is a RemovalBehavior indicating that the controller may delete
	// a type only if is not being used by another subscription
	DeleteIfUnused RemovalAction = "DeleteIfUnused"
)

// RemovalBehavior defines resource behavior when policy is removed
type RemovalBehavior struct {
	// Kind OperatorGroup
	OperatorGroups RemovalAction `json:"operatorGroups,omitempty"`
	// Kind Subscription
	Subscriptions RemovalAction `json:"subscriptions,omitempty"`
	// Kind ClusterServiceVersion
	CSVs RemovalAction `json:"clusterServiceVersions,omitempty"`
	// Kind InstallPlan
	InstallPlan RemovalAction `json:"installPlans,omitempty"`
	// Kind CustomResourceDefinitions
	CRDs RemovalAction `json:"customResourceDefinitions,omitempty"`
	// Kind APIServiceDefinitions
	APIServiceDefinitions RemovalAction `json:"apiServiceDefinitions,omitempty"`
}

// StatusConfig defines how resource statuses affect the OperatorPolicy status and compliance
type StatusConfig struct {
	CatalogSourceUnhealthy StatusConfigAction `json:"catalogSourceUnhealthy,omitempty"`
	DeploymentsUnavailable StatusConfigAction `json:"deploymentsUnavailable,omitempty"`
	UpgradesAvailable      StatusConfigAction `json:"upgradesAvailable,omitempty"`
	UpgradesProgressing    StatusConfigAction `json:"upgradesProgressing,omitempty"`
}

// OperatorPolicySpec defines the desired state of OperatorPolicy
type OperatorPolicySpec struct {
	Severity          policyv1.Severity          `json:"severity,omitempty"`          // low, medium, high
	RemediationAction policyv1.RemediationAction `json:"remediationAction,omitempty"` // inform, enforce
	ComplianceType    policyv1.ComplianceType    `json:"complianceType"`              // musthave

	// Include the name, namespace, and any `spec` fields for the OperatorGroup.
	// For more info, see `kubectl explain operatorgroup.spec` or
	// https://olm.operatorframework.io/docs/concepts/crds/operatorgroup/
	// +kubebuilder:pruning:PreserveUnknownFields
	// +optional
	OperatorGroup *runtime.RawExtension `json:"operatorGroup,omitempty"`

	// Include the namespace, and any `spec` fields for the Subscription.
	// For more info, see `kubectl explain subscription.spec` or
	// https://olm.operatorframework.io/docs/concepts/crds/subscription/
	// +kubebuilder:validation:Required
	// +kubebuilder:pruning:PreserveUnknownFields
	Subscription runtime.RawExtension `json:"subscription"`

	// Versions is a list of nonempty strings that specifies which installed versions are compliant when
	// in 'inform' mode, and which installPlans are approved when in 'enforce' mode
	Versions []policyv1.NonEmptyString `json:"versions,omitempty"`

	// FUTURE
	//nolint:dupword
	// RemovalBehavior RemovalBehavior           `json:"removalBehavior,omitempty"`
	//nolint:dupword
	// StatusConfig    StatusConfig              `json:"statusConfig,omitempty"`
}

// OperatorPolicyStatus defines the observed state of OperatorPolicy
type OperatorPolicyStatus struct {
	// Most recent compliance state of the policy
	ComplianceState policyv1.ComplianceState `json:"compliant,omitempty"`
	// Historic details on the condition of the policy
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// List of resources processed by the policy
	RelatedObjects []policyv1.RelatedObject `json:"relatedObjects"`
}

func (status OperatorPolicyStatus) RelatedObjsOfKind(kind string) map[int]policyv1.RelatedObject {
	objs := make(map[int]policyv1.RelatedObject)

	for i, related := range status.RelatedObjects {
		if related.Object.Kind == kind {
			objs[i] = related
		}
	}

	return objs
}

// Searches the conditions of the policy, and returns the index and condition matching the
// given condition Type. It will return -1 as the index if no condition of the specified
// Type is found.
func (status OperatorPolicyStatus) GetCondition(condType string) (int, metav1.Condition) {
	for i, cond := range status.Conditions {
		if cond.Type == condType {
			return i, cond
		}
	}

	return -1, metav1.Condition{}
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// OperatorPolicy is the Schema for the operatorpolicies API
type OperatorPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   OperatorPolicySpec   `json:"spec,omitempty"`
	Status OperatorPolicyStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// OperatorPolicyList contains a list of OperatorPolicy
type OperatorPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []OperatorPolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&OperatorPolicy{}, &OperatorPolicyList{})
}
