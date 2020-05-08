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
package v1

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var samplePolicy = ConfigurationPolicy{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "foo",
		Namespace: "default",
	}}

var samplePolicySpec = ConfigurationPolicySpec{
	Severity:                        "high",
	RemediationAction:               "enforce",
	MaxRoleBindingUsersPerNamespace: 1,
	MaxClusterRoleBindingGroups:     1,
}

var typeMeta = metav1.TypeMeta{
	Kind:       "Policy",
	APIVersion: "v1alpha1",
}

var objectMeta = metav1.ObjectMeta{
	Name:      "foo",
	Namespace: "default",
}

var listMeta = metav1.ListMeta{
	Continue: "continue",
}

var items = []ConfigurationPolicy{}

func TestPolicyDeepCopyInto(t *testing.T) {
	policy := Policy{
		ObjectMeta: objectMeta,
		TypeMeta:   typeMeta,
	}
	policy2 := Policy{}
	policy.DeepCopyInto(&policy2)
	assert.True(t, reflect.DeepEqual(policy, policy2))
}

func TestPolicyDeepCopy(t *testing.T) {
	typeMeta := metav1.TypeMeta{
		Kind:       "Policy",
		APIVersion: "v1alpha1",
	}

	objectMeta := metav1.ObjectMeta{
		Name:      "foo",
		Namespace: "default",
	}

	policy := Policy{
		ObjectMeta: objectMeta,
		TypeMeta:   typeMeta,
	}
	policy2 := policy.DeepCopy()
	assert.True(t, reflect.DeepEqual(policy, *policy2))
}

func TestConfigurationPolicyDeepCopyInto(t *testing.T) {
	policy2 := ConfigurationPolicy{}
	samplePolicy.DeepCopyInto(&policy2)
	assert.True(t, reflect.DeepEqual(samplePolicy, policy2))
}

func TestConfigurationPolicyDeepCopy(t *testing.T) {
	policy := ConfigurationPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: "default",
		}}
	policy2 := policy.DeepCopy()
	assert.True(t, reflect.DeepEqual(policy, *policy2))
}

func TestConfigurationPolicySpecDeepCopyInto(t *testing.T) {
	policySpec2 := ConfigurationPolicySpec{}
	samplePolicySpec.DeepCopyInto(&policySpec2)
	assert.True(t, reflect.DeepEqual(samplePolicySpec, policySpec2))
}

func TestConfigurationPolicySpecDeepCopy(t *testing.T) {
	policySpec2 := samplePolicySpec.DeepCopy()
	assert.True(t, reflect.DeepEqual(samplePolicySpec, *policySpec2))
}

func TestConfigurationPolicyListDeepCopy(t *testing.T) {
	items = append(items, samplePolicy)
	samplePolicyList := ConfigurationPolicyList{
		TypeMeta: typeMeta,
		ListMeta: listMeta,
		Items:    items,
	}
	samplePolicyList2 := samplePolicyList.DeepCopy()
	assert.True(t, reflect.DeepEqual(samplePolicyList, *samplePolicyList2))
}

func TestConfigurationPolicyListDeepCopyInto(t *testing.T) {
	items = append(items, samplePolicy)
	samplePolicyList := ConfigurationPolicyList{
		TypeMeta: typeMeta,
		ListMeta: listMeta,
		Items:    items,
	}
	samplePolicyList2 := ConfigurationPolicyList{}
	samplePolicyList.DeepCopyInto(&samplePolicyList2)
	assert.True(t, reflect.DeepEqual(samplePolicyList, samplePolicyList2))
}

func TestConfigurationPolicyStatusDeepCopy(t *testing.T) {
	var compliantDetail = map[string][]string{}
	var compliantDetails = map[string]map[string][]string{}
	details := []string{}

	details = append(details, "detail1", "detail2")

	compliantDetail["w"] = details
	compliantDetails["a"] = compliantDetail
	compliantDetails["b"] = compliantDetail
	compliantDetails["c"] = compliantDetail
	samplePolicyStatus := ConfigurationPolicyStatus{
		ComplianceState:   "Compliant",
		CompliancyDetails: compliantDetails,
	}
	samplePolicyStatus2 := samplePolicyStatus.DeepCopy()
	assert.True(t, reflect.DeepEqual(samplePolicyStatus, *samplePolicyStatus2))
}

func TestConfigurationPolicyStatusDeepCopyInto(t *testing.T) {
	var compliantDetail = map[string][]string{}
	var compliantDetails = map[string]map[string][]string{}
	details := []string{}

	details = append(details, "detail1", "detail2")

	compliantDetail["w"] = details
	compliantDetails["a"] = compliantDetail
	compliantDetails["b"] = compliantDetail
	compliantDetails["c"] = compliantDetail
	samplePolicyStatus := ConfigurationPolicyStatus{
		ComplianceState:   "Compliant",
		CompliancyDetails: compliantDetails,
	}
	var samplePolicyStatus2 ConfigurationPolicyStatus
	samplePolicyStatus.DeepCopyInto(&samplePolicyStatus2)
	assert.True(t, reflect.DeepEqual(samplePolicyStatus, samplePolicyStatus2))
}
