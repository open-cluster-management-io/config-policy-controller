// Copyright (c) 2021 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package v1beta1

import (
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	policyv1 "open-cluster-management.io/config-policy-controller/api/v1"
)

// RemovalAction is the behavior when the operator policy is removed. The supported options are
// `Keep`, `Delete`, or `DeleteIfUnused`.
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

func (ra RemovalAction) IsKeep() bool {
	return strings.EqualFold(string(ra), string(Keep))
}

func (ra RemovalAction) IsDelete() bool {
	return strings.EqualFold(string(ra), string(Delete))
}

func (ra RemovalAction) IsDeleteIfUnused() bool {
	return strings.EqualFold(string(ra), string(DeleteIfUnused))
}

type RemovalBehavior struct {
	// Use the `operatorGroups` parameter to specify whether to delete the OperatorGroup. The default
	// value is `DeleteIfUnused`, which only deletes the OperatorGroup if there is not another
	// resource using it.
	//
	//+kubebuilder:default=DeleteIfUnused
	//+kubebuilder:validation:Enum=Keep;DeleteIfUnused
	OperatorGroups RemovalAction `json:"operatorGroups,omitempty"`

	// Use the `subscriptions` parameter to specify whether to delete the Subscription. The default
	// value is `Delete`.
	//
	//+kubebuilder:default=Delete
	//+kubebuilder:validation:Enum=Keep;Delete
	Subscriptions RemovalAction `json:"subscriptions,omitempty"`

	// Use the `clusterServiceVersions` parameter to specify whether to delete the
	// ClusterServiceVersion. The default value is `Delete`.
	//
	//+kubebuilder:default=Delete
	//+kubebuilder:validation:Enum=Keep;Delete
	CSVs RemovalAction `json:"clusterServiceVersions,omitempty"`

	// Use the customResourceDefinitions parameter to specify whether to delete any
	// CustomResourceDefinitions associated with the operator. The default value is `Keep`, because
	// deleting them should be done deliberately.
	//
	//+kubebuilder:default=Keep
	//+kubebuilder:validation:Enum=Keep;Delete
	CRDs RemovalAction `json:"customResourceDefinitions,omitempty"`
}

// ApplyDefaults ensures that unset fields in a RemovalBehavior behave as if they were set to the
// default values. In a cluster, Kubernetes API validation should ensure that there are no unset
// values and should apply the default values itself.
func (rb RemovalBehavior) ApplyDefaults() RemovalBehavior {
	withDefaults := *rb.DeepCopy()

	if withDefaults.OperatorGroups == "" {
		withDefaults.OperatorGroups = DeleteIfUnused
	}

	if withDefaults.Subscriptions == "" {
		withDefaults.Subscriptions = Delete
	}

	if withDefaults.CSVs == "" {
		withDefaults.CSVs = Delete
	}

	if withDefaults.CRDs == "" {
		withDefaults.CRDs = Keep
	}

	return withDefaults
}

// ComplianceConfigAction configures how a status condition is reported when the involved operators
// are out of compliance with the operator policy. Options are `Compliant` or `NonCompliant`.
//
// +kubebuilder:validation:Enum=Compliant;NonCompliant
type ComplianceConfigAction string

const (
	// Compliant is a ComplianceConfigAction that only shows the status message and does not affect
	// the overall compliance.
	Compliant ComplianceConfigAction = "Compliant"

	// NonCompliant is a ComplianceConfigAction that shows the status message and sets the overall
	// compliance when the condition is met.
	NonCompliant ComplianceConfigAction = "NonCompliant"
)

// ComplianceConfig defines how resource statuses affect the overall operator policy status and
// compliance.
type ComplianceConfig struct {
	// CatalogSourceUnhealthy specifies how the CatalogSourceUnhealthy typed condition should affect
	// overall policy compliance. The default value is `Compliant`.
	//
	//+kubebuilder:default=Compliant
	CatalogSourceUnhealthy ComplianceConfigAction `json:"catalogSourceUnhealthy,omitempty"`
	// DeploymentsUnavailable specifies how the DeploymentCompliant typed condition should affect
	// overall policy compliance. The default value is `NonCompliant`.
	//
	//+kubebuilder:default=NonCompliant
	DeploymentsUnavailable ComplianceConfigAction `json:"deploymentsUnavailable,omitempty"`
	// UpgradesAvailable specifies how the InstallPlanCompliant typed condition should affect overall
	// policy compliance. The default value is `Compliant`.
	//
	//+kubebuilder:default=Compliant
	UpgradesAvailable ComplianceConfigAction `json:"upgradesAvailable,omitempty"`
}

// OperatorPolicySpec defines the desired state of a particular operator on the cluster.
type OperatorPolicySpec struct {
	Severity          policyv1.Severity          `json:"severity,omitempty"`
	RemediationAction policyv1.RemediationAction `json:"remediationAction,omitempty"`

	// ComplianceType specifies the desired state of the operator on the cluster. If set to
	// `musthave`, the policy is compliant when the operator is found. If set to `mustnothave`,
	// the policy is compliant when the operator is not found.
	//
	// +kubebuilder:validation:Enum=musthave;mustnothave
	ComplianceType policyv1.ComplianceType `json:"complianceType"`

	// OperatorGroup specifies which `OperatorGroup` to inspect. Include the name, namespace, and any
	// `spec` fields for the operator group. For more info, see `kubectl explain operatorgroups.spec`
	// or view https://olm.operatorframework.io/docs/concepts/crds/operatorgroup/.
	//
	//+kubebuilder:pruning:PreserveUnknownFields
	//+optional
	OperatorGroup *runtime.RawExtension `json:"operatorGroup,omitempty"`

	// Subscription specifies which operator `Subscription` resource to inspect. Include the
	// namespace, and any `spec` fields for the Subscription. For more info, see `kubectl explain
	// subscriptions.operators.coreos.com.spec` or view
	// https://olm.operatorframework.io/docs/concepts/crds/subscription/.
	//
	//+kubebuilder:validation:Required
	//+kubebuilder:pruning:PreserveUnknownFields
	Subscription runtime.RawExtension `json:"subscription"`

	// Versions is a list of non-empty strings that specifies which installed versions are compliant
	// when in `inform` mode and which `InstallPlans` are approved when in `enforce` mode.
	Versions []policyv1.NonEmptyString `json:"versions,omitempty"`

	// Use RemovalBehavior to define what resources need to be removed when enforcing `mustnothave`
	// policies. When in `inform` mode, any resources that are deleted if the policy is set to
	// `enforce` makes the policy noncompliant, but resources that are kept are compliant.
	//
	//+kubebuilder:default={}
	RemovalBehavior RemovalBehavior `json:"removalBehavior,omitempty"`

	// UpgradeApproval determines whether 'upgrade' InstallPlans for the operator will be approved
	// by the controller when the policy is enforced and in 'musthave' mode. The initial InstallPlan
	// approval is not affected by this setting. This setting has no effect when the policy is in
	// 'mustnothave' mode. Allowed values are "None" or "Automatic".
	//
	//+kubebuilder:validation:Required
	//+kubebuilder:validation:Enum=None;Automatic
	UpgradeApproval string `json:"upgradeApproval"`

	// ComplianceConfig defines how resource statuses affect the OperatorPolicy status and compliance.
	// When set to Compliant, the condition does not impact the OperatorPolicy compliance. When set to
	// NonCompliant, the condition causes the OperatorPolicy to become NonCompliant.
	//
	//+kubebuilder:default={}
	ComplianceConfig ComplianceConfig `json:"complianceConfig,omitempty"`
}

// OperatorPolicyStatus is the observed state of the operators from the specifications given in the
// operator policy.
type OperatorPolicyStatus struct {
	// ComplianceState reports the most recent compliance state of the operator policy.
	ComplianceState policyv1.ComplianceState `json:"compliant,omitempty"`

	// ObservedGeneration is the latest generation observed by the controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions includes historic details on the condition of the operator policy.
	//
	//+listType=map
	//+listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// RelatedObjects reports a list of resources associated with the operator policy.
	//
	//+optional
	RelatedObjects []policyv1.RelatedObject `json:"relatedObjects"`

	// The resolved name.namespace of the subscription
	ResolvedSubscriptionLabel string `json:"resolvedSubscriptionLabel,omitempty"`

	// The list of overlapping OperatorPolicies (as name.namespace) which all manage the same
	// subscription, including this policy. When no overlapping is detected, this list will be empty.
	OverlappingPolicies []string `json:"overlappingPolicies,omitempty"`

	// Timestamp for a possible intervention to help a Subscription stuck with a
	// ConstraintsNotSatisfiable condition. Can be in the future, indicating the
	// policy is waiting for OLM to resolve the situation. If in the recent past,
	// the policy may update the status of the Subscription.
	SubscriptionInterventionTime *metav1.Time `json:"subscriptionInterventionTime,omitempty"`
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

// Returns true if the SubscriptionInterventionTime is far enough in the past
// to be considered expired, and therefore should be removed from the status.
func (status OperatorPolicyStatus) SubscriptionInterventionExpired() bool {
	if status.SubscriptionInterventionTime == nil {
		return false
	}

	return status.SubscriptionInterventionTime.Time.Before(time.Now().Add(-10 * time.Second))
}

// Returns true if the SubscriptionInterventionTime is in the future.
func (status OperatorPolicyStatus) SubscriptionInterventionWaiting() bool {
	if status.SubscriptionInterventionTime == nil {
		return false
	}

	return status.SubscriptionInterventionTime.Time.After(time.Now())
}

// OperatorPolicy is the schema for the operatorpolicies API. You can use the operator policy to
// manage operators by providing automation for their management and reporting on the status across
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
