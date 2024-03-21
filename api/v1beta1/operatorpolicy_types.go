// Copyright (c) 2021 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	policyv1 "open-cluster-management.io/config-policy-controller/api/v1"
)

// RemovalAction is the behavior when the operator policy is removed. Options are 'Keep', 'Delete',
// or 'DeleteIfUnused'.
//
// +kubebuilder:validation:Enum=Keep;Delete;DeleteIfUnused
type RemovalAction string

const (
	// Keep is a RemovalBehavior indicating that the controller may not delete a type.
	Keep RemovalAction = "Keep"

	// Delete is a RemovalBehavior indicating that the controller may delete a type.
	Delete RemovalAction = "Delete"

	// DeleteIfUnused is a RemovalBehavior indicating that the controller may delete a type only if it
	// is not being used by another subscription.
	DeleteIfUnused RemovalAction = "DeleteIfUnused"
)

// RemovalBehavior defines resource behavior when the operator policy is removed.
type RemovalBehavior struct {
	// OperatorGroups is the removal action for kind OperatorGroup.
	OperatorGroups RemovalAction `json:"operatorGroups,omitempty"`

	// Subscriptions is the removal action for kind Subscription.
	Subscriptions RemovalAction `json:"subscriptions,omitempty"`

	// CSVs is the removal action for kind ClusterServiceVersion.
	CSVs RemovalAction `json:"clusterServiceVersions,omitempty"`

	// InstallPlan is the removal action for kind InstallPlan.
	InstallPlan RemovalAction `json:"installPlans,omitempty"`

	// CRDs is the removal action for kind CustomResourceDefinition.
	CRDs RemovalAction `json:"customResourceDefinitions,omitempty"`

	// APIServiceDefinitions is the removal action for kind APIServices that have been defined in the
	// associated ClusterServiceVersion.
	APIServiceDefinitions RemovalAction `json:"apiServiceDefinitions,omitempty"`
}

// StatusConfigAction configures how a status condition is reported when the involved operators are
// out of compliance with the operator policy. Options are 'StatusMessageOnly' or
// 'NonCompliant'.
//
// +kubebuilder:validation:Enum=StatusMessageOnly;NonCompliant
type StatusConfigAction string

const (
	// StatusMessageOnly is a StatusConfigAction that only shows the status message.
	StatusMessageOnly StatusConfigAction = "StatusMessageOnly"

	// NonCompliant is a StatusConfigAction that shows the status message and sets the compliance to
	// NonCompliant.
	NonCompliant StatusConfigAction = "NonCompliant"
)

// StatusConfig defines how resource statuses affect the overall operator policy status and
// compliance.
type StatusConfig struct {
	// CatalogSourcesUnhealthy defines how the CatalogSourcesUnhealthy condition affects the operator
	// policy status.
	CatalogSourceUnhealthy StatusConfigAction `json:"catalogSourceUnhealthy,omitempty"`

	// DeploymentsUnavailable defines how the DeploymentsUnavailable condition affects the operator
	// policy status.
	DeploymentsUnavailable StatusConfigAction `json:"deploymentsUnavailable,omitempty"`

	// UpgradesAvailable defines how the UpgradesAvailable condition affects the operator policy
	// status.
	UpgradesAvailable StatusConfigAction `json:"upgradesAvailable,omitempty"`

	// UpgradesProgressing defines how the UpgradesProgressing condition affects the operator policy
	// status.
	UpgradesProgressing StatusConfigAction `json:"upgradesProgressing,omitempty"`
}

// OperatorPolicySpec defines the desired state of a particular operator on the cluster.
type OperatorPolicySpec struct {
	Severity          policyv1.Severity          `json:"severity,omitempty"`
	RemediationAction policyv1.RemediationAction `json:"remediationAction,omitempty"`
	ComplianceType    policyv1.ComplianceType    `json:"complianceType"`

	// OperatorGroup specifies the OperatorGroup to be handled. Include the name, namespace, and any
	// `spec` fields for the OperatorGroup. For more info, see `kubectl explain operatorgroups.spec`
	// or view https://olm.operatorframework.io/docs/concepts/crds/operatorgroup/
	//
	// +kubebuilder:pruning:PreserveUnknownFields
	// +optional
	OperatorGroup *runtime.RawExtension `json:"operatorGroup,omitempty"`

	// Subscription specifies the operator Subscription to be handled. Include the namespace, and any
	// `spec` fields for the Subscription. For more info, see `kubectl explain
	// subscriptions.operators.coreos.com.spec` or view
	// https://olm.operatorframework.io/docs/concepts/crds/subscription/
	//
	// +kubebuilder:pruning:PreserveUnknownFields
	Subscription runtime.RawExtension `json:"subscription"`

	// Versions is a list of nonempty strings that specifies which installed versions are compliant
	// when in 'inform' mode and which InstallPlans are approved when in 'enforce' mode.
	Versions []policyv1.NonEmptyString `json:"versions,omitempty"`
}

// OperatorPolicyStatus reports the observed state of the operators resulting from the
// specifications given in the operator policy.
type OperatorPolicyStatus struct {
	// ComplianceState reports the most recent compliance state of the operator policy.
	ComplianceState policyv1.ComplianceState `json:"compliant,omitempty"`

	// Conditions reports historic details on the condition of the operator policy.
	//
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// RelatedObjects reports a list of resources associated with the operator policy.
	//
	// +optional
	RelatedObjects []policyv1.RelatedObject `json:"relatedObjects"`
}

// RelatedObjsOfKind iterates over the related objects in the status and returns a map of the index
// in the array to the related object that has the given kind.
func (status OperatorPolicyStatus) RelatedObjsOfKind(kind string) map[int]policyv1.RelatedObject {
	objs := make(map[int]policyv1.RelatedObject)

	for i, related := range status.RelatedObjects {
		if related.Object.Kind == kind {
			objs[i] = related
		}
	}

	return objs
}

// GetCondition iterates over the status conditions of the policy and returns the index and
// condition matching the given condition Type. It will return -1 as the index if no condition of
// the specified Type is found.
func (status OperatorPolicyStatus) GetCondition(condType string) (int, metav1.Condition) {
	for i, cond := range status.Conditions {
		if cond.Type == condType {
			return i, cond
		}
	}

	return -1, metav1.Condition{}
}

// OperatorPolicy is the Schema for the operatorpolicies API. Operator policy eases the management
// of OLM operators by providing automation for their management and reporting on the status across
// the various operator objects.
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
type OperatorPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   OperatorPolicySpec   `json:"spec,omitempty"`
	Status OperatorPolicyStatus `json:"status,omitempty"`
}

// OperatorPolicyList contains a list of operator policies.
//
// +kubebuilder:object:root=true
type OperatorPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []OperatorPolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&OperatorPolicy{}, &OperatorPolicyList{})
}
