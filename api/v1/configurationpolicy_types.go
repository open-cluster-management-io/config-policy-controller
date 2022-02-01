// Copyright (c) 2021 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package v1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
)

// A custom type is required since there is no way to have a kubebuilder marker
// apply to the items of a slice.

// +kubebuilder:validation:MinLength=1
type NonEmptyString string

// RemediationAction : enforce or inform
// +kubebuilder:validation:Enum=Inform;inform;Enforce;enforce
type RemediationAction string

// Severity : low, medium, high, or critical
// +kubebuilder:validation:Enum=low;Low;medium;Medium;high;High;critical;Critical
type Severity string

const (
	// Enforce is an remediationAction to make changes
	Enforce RemediationAction = "Enforce"

	// Inform is an remediationAction to only inform
	Inform RemediationAction = "Inform"
)

// ComplianceState shows the state of enforcement
type ComplianceState string

const (
	// Compliant is an ComplianceState
	Compliant ComplianceState = "Compliant"

	// NonCompliant is an ComplianceState
	NonCompliant ComplianceState = "NonCompliant"

	// UnknownCompliancy is an ComplianceState
	UnknownCompliancy ComplianceState = "UnknownCompliancy"
)

// Condition is the base struct for representing resource conditions
type Condition struct {
	// Type of condition, e.g Complete or Failed.
	Type string `json:"type"`
	// Status of the condition, one of True, False, Unknown.
	Status corev1.ConditionStatus `json:"status,omitempty" protobuf:"bytes,12,rep,name=status"`
	// The last time the condition transitioned from one status to another.
	// +optional
	LastTransitionTime metav1.Time `json:"lastTransitionTime,omitempty" protobuf:"bytes,3,opt,name=lastTransitionTime"`
	// The reason for the condition's last transition.
	// +optional
	Reason string `json:"reason,omitempty" protobuf:"bytes,4,opt,name=reason"`
	// A human readable message indicating details about the transition.
	// +optional
	Message string `json:"message,omitempty" protobuf:"bytes,5,opt,name=message"`
}

// Target defines the list of namespaces to include/exclude
type Target struct {
	Include []NonEmptyString `json:"include,omitempty"`
	Exclude []NonEmptyString `json:"exclude,omitempty"`
}

// ConfigurationPolicySpec defines the desired state of ConfigurationPolicy
type ConfigurationPolicySpec struct {
	Severity          Severity          `json:"severity,omitempty"`          // low, medium, high
	RemediationAction RemediationAction `json:"remediationAction,omitempty"` // enforce, inform
	NamespaceSelector Target            `json:"namespaceSelector,omitempty"`
	LabelSelector     map[string]string `json:"labelSelector,omitempty"`
	ObjectTemplates   []*ObjectTemplate `json:"object-templates,omitempty"`
}

// ObjectTemplate describes how an object should look
type ObjectTemplate struct {
	// ComplianceType specifies whether it is: musthave, mustnothave, mustonlyhave
	ComplianceType ComplianceType `json:"complianceType"`

	MetadataComplianceType MetadataComplianceType `json:"metadataComplianceType,omitempty"`

	// ObjectDefinition defines required fields for the object
	// +kubebuilder:pruning:PreserveUnknownFields
	ObjectDefinition runtime.RawExtension `json:"objectDefinition,omitempty"`
}

// ConfigurationPolicyStatus defines the observed state of ConfigurationPolicy
type ConfigurationPolicyStatus struct {
	ComplianceState   ComplianceState  `json:"compliant,omitempty"`         // Compliant/NonCompliant/UnknownCompliancy
	CompliancyDetails []TemplateStatus `json:"compliancyDetails,omitempty"` // reason for non-compliancy
	RelatedObjects    []RelatedObject  `json:"relatedObjects,omitempty"`    // List of resources processed by the policy
}

// CompliancePerClusterStatus contains aggregate status of other policies in cluster
type CompliancePerClusterStatus struct {
	AggregatePolicyStatus map[string]*ConfigurationPolicyStatus `json:"aggregatePoliciesStatus,omitempty"`
	ComplianceState       ComplianceState                       `json:"compliant,omitempty"`
	ClusterName           string                                `json:"clustername,omitempty"`
}

// ComplianceMap map to hold CompliancePerClusterStatus objects
type ComplianceMap map[string]*CompliancePerClusterStatus

// ResourceState genric description of a state
type ResourceState string

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// ConfigurationPolicy is the Schema for the configurationpolicies API
type ConfigurationPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ConfigurationPolicySpec   `json:"spec,omitempty"`
	Status ConfigurationPolicyStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// ConfigurationPolicyList contains a list of ConfigurationPolicy
type ConfigurationPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ConfigurationPolicy `json:"items"`
}

// TemplateStatus hold the status result
type TemplateStatus struct {
	ComplianceState ComplianceState `json:"Compliant,omitempty"` // Compliant, NonCompliant, UnkownCompliancy
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	Conditions []Condition `json:"conditions,omitempty"`

	Validity Validity `json:"Validity,omitempty"` // a template can be invalid if it has conflicting roles
}

// Validity describes if it is valid or not
type Validity struct {
	Valid  *bool  `json:"valid,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// ComplianceType describes whether we must or must not have a given resource
// +kubebuilder:validation:Enum=MustHave;Musthave;musthave;MustOnlyHave;Mustonlyhave;mustonlyhave;MustNotHave;Mustnothave;mustnothave
type ComplianceType string

const (
	// MustNotHave is an enforcement state to exclude a resource
	MustNotHave ComplianceType = "Mustnothave"

	// MustHave is an enforcement state to include a resource
	MustHave ComplianceType = "Musthave"

	// MustOnlyHave is an enforcement state to exclusively include a resource
	MustOnlyHave ComplianceType = "Mustonlyhave"
)

// MetadataComplianceType describes how to check compliance for the labels/annotations of a given object
// +kubebuilder:validation:Enum=MustHave;Musthave;musthave;MustOnlyHave;Mustonlyhave;mustonlyhave
type MetadataComplianceType string

// RelatedObject is the list of objects matched by this Policy resource.
type RelatedObject struct {
	//
	Object ObjectResource `json:"object,omitempty"`
	//
	Compliant string `json:"compliant,omitempty"`
	//
	Reason string `json:"reason,omitempty"`
}

// ObjectResource is an object identified by the policy as a resource that needs to be validated.
type ObjectResource struct {
	// Kind of the referent. More info:
	// https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds
	Kind string `json:"kind,omitempty"`
	// API version of the referent.
	APIVersion string `json:"apiVersion,omitempty"`
	// Metadata values from the referent.
	Metadata ObjectMetadata `json:"metadata,omitempty"`
}

// ObjectMetadata contains the resource metadata for an object being processed by the policy
type ObjectMetadata struct {
	// Name of the referent. More info:
	// https://kubernetes.io/docs/concepts/overview/working-with-objects/names/#names
	Name string `json:"name,omitempty"`
	// Namespace of the referent. More info:
	// https://kubernetes.io/docs/concepts/overview/working-with-objects/namespaces/
	Namespace string `json:"namespace,omitempty"`
}

func init() {
	SchemeBuilder.Register(&ConfigurationPolicy{}, &ConfigurationPolicyList{})
}
