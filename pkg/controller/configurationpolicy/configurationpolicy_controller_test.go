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
package configurationpolicy

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	policiesv1alpha1 "github.ibm.com/IBMPrivateCloud/multicloud-operators-policy-controller/pkg/apis/policies/v1alpha1"
	"github.ibm.com/IBMPrivateCloud/multicloud-operators-policy-controller/pkg/common"
	coretypes "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	sub "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	testclient "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var mgr manager.Manager
var err error

func TestReconcile(t *testing.T) {
	var (
		name      = "foo"
		namespace = "default"
	)
	instance := &policiesv1alpha1.ConfigurationPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: "default",
		},
		Spec: policiesv1alpha1.ConfigurationPolicySpec{
			Severity: "low",
			NamespaceSelector: policiesv1alpha1.Target{
				Include: []string{"default", "kube-*"},
				Exclude: []string{"kube-system"},
			},
			RemediationAction: "inform",
			RoleTemplates: []*policiesv1alpha1.RoleTemplate{
				&policiesv1alpha1.RoleTemplate{
					TypeMeta: metav1.TypeMeta{
						Kind: "roletemplate",
					},
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "",
						Name:      "operator-role-policy",
					},
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"dev": "true",
						},
					},
					ComplianceType: "musthave",
					Rules: []policiesv1alpha1.PolicyRuleTemplate{
						policiesv1alpha1.PolicyRuleTemplate{
							ComplianceType: "musthave",
							PolicyRule: sub.PolicyRule{
								APIGroups: []string{"extensions", "apps"},
								Resources: []string{"deployments"},
								Verbs:     []string{"get", "list", "watch", "create", "delete", "patch"},
							},
						},
					},
				},
			},
			ObjectTemplates: []*policiesv1alpha1.ObjectTemplate{
				&policiesv1alpha1.ObjectTemplate{
					ComplianceType:   "musthave",
					ObjectDefinition: runtime.RawExtension{},
				},
			},
		},
	}

	// Objects to track in the fake client.
	objs := []runtime.Object{instance}
	// Register operator types with the runtime scheme.
	s := scheme.Scheme
	s.AddKnownTypes(policiesv1alpha1.SchemeGroupVersion, instance)

	// Create a fake client to mock API calls.
	cl := fake.NewFakeClient(objs...)
	// Create a ReconcileConfigurationPolicy object with the scheme and fake client
	r := &ReconcileConfigurationPolicy{client: cl, scheme: s, recorder: nil}

	// Mock request to simulate Reconcile() being called on an event for a
	// watched resource .
	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      name,
			Namespace: namespace,
		},
	}
	var simpleClient kubernetes.Interface = testclient.NewSimpleClientset()
	common.Initialize(&simpleClient, nil)
	InitializeClient(&simpleClient)
	res, err := r.Reconcile(req)
	if err != nil {
		t.Fatalf("reconcile: (%v)", err)
	}
	t.Log(res)
}

func TestPeriodicallyExecSamplePolicies(t *testing.T) {
	var (
		name      = "foo"
		namespace = "default"
	)
	var typeMeta = metav1.TypeMeta{
		Kind: "namespace",
	}
	var objMeta = metav1.ObjectMeta{
		Name: "default",
	}
	var ns = coretypes.Namespace{
		TypeMeta:   typeMeta,
		ObjectMeta: objMeta,
	}
	var def = map[string]interface{}{
		"apiDefinition": "v1",
		"kind":          "Pod",
		"metadata": metav1.ObjectMeta{
			Name: "nginx-pod",
		},
		"spec": map[string]interface{}{},
	}
	defJSON, err := json.Marshal(def)
	if err != nil {
		t.Log(err)
	}

	// Mock request to simulate Reconcile() being called on an event for a
	// watched resource .
	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      name,
			Namespace: namespace,
		},
	}
	instance := &policiesv1alpha1.ConfigurationPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: "default",
		},
		Spec: policiesv1alpha1.ConfigurationPolicySpec{
			Severity: "low",
			NamespaceSelector: policiesv1alpha1.Target{
				Include: []string{"default", "kube-*"},
				Exclude: []string{"kube-system"},
			},
			RemediationAction: "inform",
			RoleTemplates: []*policiesv1alpha1.RoleTemplate{
				&policiesv1alpha1.RoleTemplate{
					TypeMeta: metav1.TypeMeta{
						Kind: "roletemplate",
					},
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "",
						Name:      "operator-role-policy",
					},
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"dev": "true",
						},
					},
					ComplianceType: "musthave",
					Rules: []policiesv1alpha1.PolicyRuleTemplate{
						policiesv1alpha1.PolicyRuleTemplate{
							ComplianceType: "musthave",
							PolicyRule: sub.PolicyRule{
								APIGroups: []string{"extensions", "apps"},
								Resources: []string{"deployments"},
								Verbs:     []string{"get", "list", "watch", "create", "delete", "patch"},
							},
						},
					},
				},
			},
			ObjectTemplates: []*policiesv1alpha1.ObjectTemplate{
				&policiesv1alpha1.ObjectTemplate{
					ComplianceType: "musthave",
					ObjectDefinition: runtime.RawExtension{
						Raw: defJSON,
					},
				},
			},
		},
	}

	// Objects to track in the fake client.
	objs := []runtime.Object{instance}
	// Register operator types with the runtime scheme.
	s := scheme.Scheme
	s.AddKnownTypes(policiesv1alpha1.SchemeGroupVersion, instance)

	// Create a fake client to mock API calls.
	cl := fake.NewFakeClient(objs...)

	// Create a ReconcileConfigurationPolicy object with the scheme and fake client.
	r := &ReconcileConfigurationPolicy{client: cl, scheme: s, recorder: nil}
	var simpleClient kubernetes.Interface = testclient.NewSimpleClientset()
	simpleClient.CoreV1().Namespaces().Create(&ns)
	common.Initialize(&simpleClient, nil)
	res, err := r.Reconcile(req)
	if err != nil {
		t.Fatalf("reconcile: (%v)", err)
	}
	t.Log(res)
	var target = []string{"default"}
	samplePolicy.Spec.NamespaceSelector.Include = target
	err = handleAddingPolicy(&samplePolicy)
	assert.Nil(t, err)
	PeriodicallyExecSamplePolicies(1, true)
}

func TestCheckUnNamespacedPolicies(t *testing.T) {
	var simpleClient kubernetes.Interface = testclient.NewSimpleClientset()
	common.Initialize(&simpleClient, nil)
	var samplePolicy = policiesv1alpha1.ConfigurationPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: "default",
		}}

	var policies = map[string]*policiesv1alpha1.ConfigurationPolicy{}
	policies["policy1"] = &samplePolicy

	err := checkUnNamespacedPolicies(policies)
	assert.Nil(t, err)
}

func TestEnsureDefaultLabel(t *testing.T) {
	updateNeeded := ensureDefaultLabel(&samplePolicy)
	assert.True(t, updateNeeded)

	var labels1 = map[string]string{}
	labels1["category"] = grcCategory
	samplePolicy.Labels = labels1
	updateNeeded = ensureDefaultLabel(&samplePolicy)
	assert.False(t, updateNeeded)

	var labels2 = map[string]string{}
	labels2["category"] = "foo"
	samplePolicy.Labels = labels2
	updateNeeded = ensureDefaultLabel(&samplePolicy)
	assert.True(t, updateNeeded)

	var labels3 = map[string]string{}
	labels3["foo"] = grcCategory
	samplePolicy.Labels = labels3
	updateNeeded = ensureDefaultLabel(&samplePolicy)
	assert.True(t, updateNeeded)
}

func TestCheckAllClusterLevel(t *testing.T) {
	var subject = sub.Subject{
		APIGroup:  "",
		Kind:      "User",
		Name:      "user1",
		Namespace: "default",
	}
	var subjects = []sub.Subject{}
	subjects = append(subjects, subject)
	var clusterRoleBinding = sub.ClusterRoleBinding{
		Subjects: subjects,
	}
	var items = []sub.ClusterRoleBinding{}
	items = append(items, clusterRoleBinding)
	var clusterRoleBindingList = sub.ClusterRoleBindingList{
		Items: items,
	}
	var users, groups = checkAllClusterLevel(&clusterRoleBindingList)
	assert.Equal(t, 1, users)
	assert.Equal(t, 0, groups)
}

func TestCheckViolationsPerNamespace(t *testing.T) {
	var subject = sub.Subject{
		APIGroup:  "",
		Kind:      "User",
		Name:      "user1",
		Namespace: "default",
	}
	var subjects = []sub.Subject{}
	subjects = append(subjects, subject)
	var roleBinding = sub.RoleBinding{
		Subjects: subjects,
	}
	var items = []sub.RoleBinding{}
	items = append(items, roleBinding)
	var roleBindingList = sub.RoleBindingList{
		Items: items,
	}
	var samplePolicySpec = policiesv1alpha1.ConfigurationPolicySpec{
		MaxRoleBindingUsersPerNamespace:  1,
		MaxRoleBindingGroupsPerNamespace: 1,
		MaxClusterRoleBindingUsers:       1,
		MaxClusterRoleBindingGroups:      1,
	}
	samplePolicy.Spec = samplePolicySpec
	checkViolationsPerNamespace(&roleBindingList, &samplePolicy)
}

func TestCreateParentPolicy(t *testing.T) {
	var ownerReference = metav1.OwnerReference{
		Name: "foo",
	}
	var ownerReferences = []metav1.OwnerReference{}
	ownerReferences = append(ownerReferences, ownerReference)
	samplePolicy.OwnerReferences = ownerReferences

	policy := createParentPolicy(&samplePolicy)
	assert.NotNil(t, policy)
	createParentPolicyEvent(&samplePolicy)
}

func TestConvertPolicyStatusToString(t *testing.T) {
	var compliantDetail = map[string][]string{}
	var compliantDetails = map[string]map[string][]string{}
	details := []string{}

	details = append(details, "detail1", "detail2")

	compliantDetail["w"] = details
	compliantDetails["a"] = compliantDetail
	compliantDetails["b"] = compliantDetail
	compliantDetails["c"] = compliantDetail
	samplePolicyStatus := policiesv1alpha1.ConfigurationPolicyStatus{
		ComplianceState:   "Compliant",
		CompliancyDetails: compliantDetails,
	}
	samplePolicy.Status = samplePolicyStatus
	var policyInString = convertPolicyStatusToString(&samplePolicy)
	assert.NotNil(t, policyInString)
	checkComplianceChangeBasedOnDetails(&samplePolicy)
	checkComplianceBasedOnDetails(&samplePolicy)
	addViolationCount(&samplePolicy, 1, 1, "default")
}

func TestDeleteExternalDependency(t *testing.T) {
	mgr, err = manager.New(cfg, manager.Options{})
	reconcileConfigurationPolicy := ReconcileConfigurationPolicy{client: mgr.GetClient(), scheme: mgr.GetScheme(), recorder: mgr.GetEventRecorderFor("samplepolicy-controller")}
	reconcileConfigurationPolicy.deleteExternalDependency(&samplePolicy)
}

func TestHandleAddingPolicy(t *testing.T) {
	var simpleClient kubernetes.Interface = testclient.NewSimpleClientset()
	var typeMeta = metav1.TypeMeta{
		Kind: "namespace",
	}
	var objMeta = metav1.ObjectMeta{
		Name: "default",
	}
	var ns = coretypes.Namespace{
		TypeMeta:   typeMeta,
		ObjectMeta: objMeta,
	}
	simpleClient.CoreV1().Namespaces().Create(&ns)
	common.Initialize(&simpleClient, nil)
	err := handleAddingPolicy(&samplePolicy)
	assert.Nil(t, err)
	handleRemovingPolicy(&samplePolicy)
}

func TestGetContainerID(t *testing.T) {
	var containerStateWaiting = coretypes.ContainerStateWaiting{
		Reason: "unknown",
	}
	var containerState = coretypes.ContainerState{
		Waiting: &containerStateWaiting,
	}
	var containerStatus = coretypes.ContainerStatus{
		State:       containerState,
		ContainerID: "id",
	}
	var containerStatuses []coretypes.ContainerStatus
	containerStatuses = append(containerStatuses, containerStatus)
	var podStatus = coretypes.PodStatus{
		ContainerStatuses: containerStatuses,
	}
	var pod = coretypes.Pod{
		Status: podStatus,
	}
	getContainerID(pod, "foo")
}

func TestFlattenRole(t *testing.T) {
	rule := newRule("get,watch,list", "apps", "deployments", "")
	rule2 := newRule("get,watch,list,create,delete,update,patch", "extensions", "deployments", "") //Note: no space between verbs
	role := newRole("dev", "default", rule, rule2)
	actualResult := flattenRole(*role)

	expectedResult := map[string]map[string]bool{
		"deployments.apps": map[string]bool{"get": true, "watch": true, "list": true},
		"deployments.extensions": map[string]bool{"get": true, "watch": true, "list": true,
			"create": true, "delete": true, "update": true, "patch": true},
	}

	match := true
	if len(expectedResult) != len(actualResult) {
		match = false
		t.Fatalf("the expected results and the actual results have different length")
	}
	if !match {
		return
	}

	for key, resG := range expectedResult {
		if _, ok := actualResult[key]; ok {
			// check the lenght of the maps
			if len(actualResult[key]) > len(resG) {
				t.Fatalf("The verbs %v in actual results, are MORE than the ones %v in expectedResult ", actualResult[key], resG)
			} else if len(actualResult[key]) < len(resG) {
				t.Fatalf("The verbs %v in actual results, are LESS than the ones %v in expectedResult ", actualResult[key], resG)
			}
			for keyVerb := range resG {
				if _, ok := actualResult[key][keyVerb]; ok {
					// the verb in resG exists in actualresults
				} else {
					t.Fatalf("The verb %s is not found in actual results, when looking into key %s", keyVerb, key)
				}
			}
		} else {
			// the key is not there, we have no match
			t.Fatalf("The Key %s is not found in actual results", key)
		}
	}
}

func TestDeepCompareRoleTtoRole(t *testing.T) {
	flatRole := map[string]map[string]bool{
		"deployments.apps":       map[string]bool{"get": true, "watch": true, "list": true, "patch": true},
		"deployments.extensions": map[string]bool{"get": true},
		"secrets.":               map[string]bool{"get": true},
	}

	flatRoleT := map[string]map[string]map[string]bool{
		"musthave": map[string]map[string]bool{
			"deployments.apps":       map[string]bool{"get": true, "patch": true},
			"deployments.extensions": map[string]bool{"get": true},
		},
		"mustnothave": map[string]map[string]bool{
			"deployments.apps": map[string]bool{"apply": true},
		},
		"mustonlyhave": map[string]map[string]bool{
			"secrets.": map[string]bool{"get": true},
		},
	}

	match, res := deepCompareRoleTtoRole(flatRoleT, flatRole)
	if !match {
		t.Errorf("no match! \nmissing keys: %v \nmissing verbs: %v \nadditional keys: %v \nadditional verbs: %v\n", res.missingKeys, res.missingVerbs, res.AdditionalKeys, res.AddtionalVerbs)
	}
}

func TestFlattenRoleTemplate(t *testing.T) {
	ruleT := newRuleTemplate("get,watch,list,patch", "apps", "deployments", "", policiesv1alpha1.MustHave)
	ruleT2 := newRuleTemplate("get,watch,list,patch", "extensions", "deployments", "", policiesv1alpha1.MustNotHave)
	ruleT3 := newRuleTemplate("get,watch,list,patch", "", "secrets", "", policiesv1alpha1.MustOnlyHave)
	roleT := newRoleTemplate("dev", "default", policiesv1alpha1.MustHave, ruleT, ruleT2, ruleT3)
	actualResult := flattenRoleTemplate(*roleT)

	expectedResult := map[string]map[string]map[string]bool{
		"musthave":     map[string]map[string]bool{"deployments.apps": map[string]bool{"get": true, "patch": true, "watch": true, "list": true}},
		"mustnothave":  map[string]map[string]bool{"deployments.extensions": map[string]bool{"get": true, "patch": true, "watch": true, "list": true}},
		"mustonlyhave": map[string]map[string]bool{"secrets.": map[string]bool{"get": true, "patch": true, "watch": true, "list": true}},
	}

	if len(expectedResult) != len(actualResult) {
		t.Fatalf("\n actual:   %v \n expected: %v ", actualResult, expectedResult)
		return
	}

	for key, resG := range expectedResult {
		if _, ok := actualResult[key]; ok {
			// check the lenght of the maps
			if len(actualResult[key]) > len(resG) {
				t.Fatalf("The verbs %v in actual results, are MORE than the ones %v in expectedResult ", actualResult[key], resG)
			} else if len(actualResult[key]) < len(resG) {
				t.Fatalf("The verbs %v in actual results, are LESS than the ones %v in expectedResult ", actualResult[key], resG)
			}
			for keyRes, val := range resG {
				if len(actualResult[key][keyRes]) != len(val) {
					t.Fatalf("The keys %v in actual results, are not equal to the ones %v in expectedResult ", actualResult[key], resG)
				}
				if _, ok := actualResult[key][keyRes]; ok {
					// the res.api in resG exists in actualresults
					for keyVerb := range val {
						if _, ok := actualResult[key][keyRes][keyVerb]; ok {
							// the verb in resG exists in actualresults
						} else {
							t.Fatalf("The verb %s is not found in actual results, when looking into key %s", keyVerb, key)
						}
					}
				} else {
					t.Fatalf("The resource %s is not found in actual results, when looking into key %s", keyRes, key)
				}
			}

		} else {
			// the key is not there, we have no match
			t.Fatalf("The Key %s is not found in actual results", key)
		}
	}
	return
}

func newRule(verbs, apiGroups, resources, nonResourceURLs string) rbacv1.PolicyRule {
	return rbacv1.PolicyRule{
		Verbs:           strings.Split(verbs, ","),
		APIGroups:       strings.Split(apiGroups, ","),
		Resources:       strings.Split(resources, ","),
		NonResourceURLs: strings.Split(nonResourceURLs, ","),
	}
}

func newRole(name, namespace string, rules ...rbacv1.PolicyRule) *rbacv1.Role {
	return &rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name}, Rules: rules}
}

func newRuleTemplate(verbs, apiGroups, resources, nonResourceURLs string, complianceT policiesv1alpha1.ComplianceType) policiesv1alpha1.PolicyRuleTemplate {
	return policiesv1alpha1.PolicyRuleTemplate{
		ComplianceType: complianceT,
		PolicyRule: rbacv1.PolicyRule{
			Verbs:           strings.Split(verbs, ","),
			APIGroups:       strings.Split(apiGroups, ","),
			Resources:       strings.Split(resources, ","),
			NonResourceURLs: strings.Split(nonResourceURLs, ","),
		},
	}
}

func newRoleTemplate(name, namespace string, rolecompT policiesv1alpha1.ComplianceType, rulesT ...policiesv1alpha1.PolicyRuleTemplate) *policiesv1alpha1.RoleTemplate {
	return &policiesv1alpha1.RoleTemplate{ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name},
		ComplianceType: rolecompT,
		Rules:          rulesT}
}
