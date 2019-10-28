// Copyright 2019 The Kubernetes Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// RemediationAction : enforce or inform
type RemediationAction string

// Severity : low, medium or high
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

// Target defines the list of namespaces to include/exclude
type Target struct {
	Include []string `json:"include,omitempty"`
	Exclude []string `json:"exclude,omitempty"`
}

// SamplePolicySpec defines the desired state of SamplePolicy
// +k8s:openapi-gen=true
type SamplePolicySpec struct {
	Severity                         Severity          `json:"severity,omitempty"`          //low, medium, high
	RemediationAction                RemediationAction `json:"remediationAction,omitempty"` //enforce, inform
	NamespaceSelector                Target            `json:"namespaceSelector,omitempty"` // selecting a list of namespaces where the policy applies
	LabelSelector                    map[string]string `json:"labelSelector,omitempty"`
	MaxRoleBindingUsersPerNamespace  int               `json:"maxRoleBindingUsersPerNamespace,omitempty"`
	MaxRoleBindingGroupsPerNamespace int               `json:"maxRoleBindingGroupsPerNamespace,omitempty"`
	MaxClusterRoleBindingUsers       int               `json:"maxClusterRoleBindingUsers,omitempty"`
	MaxClusterRoleBindingGroups      int               `json:"maxClusterRoleBindingGroups,omitempty"`
}

// SamplePolicyStatus defines the observed state of SamplePolicy
// +k8s:openapi-gen=true
type SamplePolicyStatus struct {
	ComplianceState   ComplianceState                `json:"compliant,omitempty"`         // Compliant, NonCompliant, UnkownCompliancy
	CompliancyDetails map[string]map[string][]string `json:"compliancyDetails,omitempty"` // reason for non-compliancy
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// SamplePolicy is the Schema for the samplepolicies API
// +k8s:openapi-gen=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=samplepolicies,scope=Namespaced
type SamplePolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SamplePolicySpec   `json:"spec,omitempty"`
	Status SamplePolicyStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// SamplePolicyList contains a list of SamplePolicy
type SamplePolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SamplePolicy `json:"items"`
}

// Policy is a specification for a Policy resource
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +genclient
type Policy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`
}

// PolicyList is a list of Policy resources
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +k8s:lister-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type PolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []Policy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&SamplePolicy{}, &SamplePolicyList{})
}
