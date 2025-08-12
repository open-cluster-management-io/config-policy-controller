// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package controllers

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	depclient "github.com/stolostron/kubernetes-dependency-watches/client"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/yaml"

	policyv1 "open-cluster-management.io/config-policy-controller/api/v1"
)

func TestReconcile(t *testing.T) {
	name := "foo"
	namespace := "default"
	instance := &policyv1.ConfigurationPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: "default",
		},
		Spec: policyv1.ConfigurationPolicySpec{
			Severity: "low",
			NamespaceSelector: policyv1.Target{
				Include: []policyv1.NonEmptyString{"default", "kube-*"},
				Exclude: []policyv1.NonEmptyString{"kube-system"},
			},
			RemediationAction: "inform",
			ObjectTemplates: []*policyv1.ObjectTemplate{
				{
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
	s.AddKnownTypes(policyv1.GroupVersion, instance)

	// Create a fake client to mock API calls.
	clBuilder := fake.NewClientBuilder()
	clBuilder.WithRuntimeObjects(objs...)
	cl := clBuilder.Build()
	// Create a ReconcileConfigurationPolicy object with the scheme and fake client
	r := &ConfigurationPolicyReconciler{Client: cl, Scheme: s, Recorder: nil}

	// Mock request to simulate Reconcile() being called on an event for a
	// watched resource .
	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      name,
			Namespace: namespace,
		},
	}

	res, err := r.Reconcile(context.TODO(), req)
	if err != nil {
		t.Fatalf("reconcile: (%v)", err)
	}

	t.Log(res)
}

func TestMergeMaps(t *testing.T) {
	spec1 := map[string]interface{}{
		"containers": map[string]string{
			"image": "nginx1.7.9",
			"name":  "nginx",
		},
	}
	spec2 := map[string]interface{}{
		"containers": map[string]string{
			"image": "nginx1.7.9",
			"test":  "test",
		},
	}

	merged, _, err := mergeMaps(spec1, spec2, "mustonlyhave", true)
	if err != nil {
		t.Fatalf("compareSpecs: (%v)", err)
	}

	mergedExpected := map[string]interface{}{
		"containers": map[string]string{
			"image": "nginx1.7.9",
			"name":  "nginx",
		},
	}
	assert.True(t, reflect.DeepEqual(merged, mergedExpected))

	spec1 = map[string]interface{}{
		"containers": map[string]interface{}{
			"image": "nginx1.7.9",
			"test":  "1111",
			"timestamp": map[string]int64{
				"seconds": 1631796491,
			},
		},
	}
	spec2 = map[string]interface{}{
		"containers": map[string]interface{}{
			"image": "nginx1.7.9",
			"name":  "nginx",
			"timestamp": map[string]int64{
				"seconds": 1631796491,
			},
		},
	}

	merged, _, err = mergeMaps(spec1, spec2, "musthave", true)
	if err != nil {
		t.Fatalf("compareSpecs: (%v)", err)
	}

	mergedExpected = map[string]interface{}{
		"containers": map[string]interface{}{
			"image": "nginx1.7.9",
			"name":  "nginx",
			"test":  "1111",
			// This verifies that the type of the number has not changed as part of compare specs.
			// With standard JSON marshaling and unmarshaling, it will cause an int64 to be
			// converted to a float64. This ensures this does not happen.
			"timestamp": map[string]int64{
				"seconds": 1631796491,
			},
		},
	}

	assert.Equal(t, fmt.Sprintf("%+v", mergedExpected), fmt.Sprintf("%+v", merged))
}

func TestConvertPolicyStatusToString(t *testing.T) {
	compliantDetail := policyv1.TemplateStatus{
		ComplianceState: policyv1.NonCompliant,
		Conditions:      []policyv1.Condition{},
	}
	compliantDetails := []policyv1.TemplateStatus{}

	for range 3 {
		compliantDetails = append(compliantDetails, compliantDetail)
	}

	samplePolicy := getSamplePolicy()

	samplePolicyStatus := policyv1.ConfigurationPolicyStatus{
		ComplianceState:   "Compliant",
		CompliancyDetails: compliantDetails,
	}
	samplePolicy.Status = samplePolicyStatus
	policyInString := defaultComplianceMessage(&samplePolicy)

	assert.NotNil(t, policyInString)
}

func TestConvertPolicyStatusToStringLongMsg(t *testing.T) {
	msg := "Do. Or do not. There is no try."
	for len([]rune(msg)) < 1024 {
		msg += " Do. Or do not. There is no try."
	}

	samplePolicy := getSamplePolicy()

	samplePolicy.Status = policyv1.ConfigurationPolicyStatus{
		ComplianceState: "Compliant",
		CompliancyDetails: []policyv1.TemplateStatus{
			{
				ComplianceState: policyv1.NonCompliant,
				Conditions:      []policyv1.Condition{{Message: msg}},
			},
		},
	}
	statusMsg := defaultComplianceMessage(&samplePolicy)

	assert.Greater(t, len(statusMsg), 1024)
}

func TestMergeArraysMustHave(t *testing.T) {
	t.Parallel()

	testcases := map[string]struct {
		desiredList  []interface{}
		currentList  []interface{}
		expectedList []interface{}
	}{
		"merge array with existing element into array preserves array": {
			[]interface{}{
				map[string]interface{}{"a": "apple", "b": "boy"},
			},
			[]interface{}{
				map[string]interface{}{"a": "apple", "b": "boy"},
				map[string]interface{}{"c": "candy", "d": "dog"},
			},
			[]interface{}{
				map[string]interface{}{"a": "apple", "b": "boy"},
				map[string]interface{}{"c": "candy", "d": "dog"},
			},
		},
		"merge array with partial existing element into array preserves existing array": {
			[]interface{}{
				map[string]interface{}{"b": "boy"},
			},
			[]interface{}{
				map[string]interface{}{"a": "apple", "b": "boy"},
				map[string]interface{}{"c": "candy", "d": "dog"},
			},
			[]interface{}{
				map[string]interface{}{"a": "apple", "b": "boy"},
				map[string]interface{}{"c": "candy", "d": "dog"},
			},
		},
		"merge array with multiple partial existing elements into array preserves existing array": {
			[]interface{}{
				map[string]interface{}{"a": "apple"},
				map[string]interface{}{"c": "candy"},
			},
			[]interface{}{
				map[string]interface{}{"a": "apple", "b": "boy"},
				map[string]interface{}{"c": "candy", "d": "dog"},
			},
			[]interface{}{
				map[string]interface{}{"a": "apple", "b": "boy"},
				map[string]interface{}{"c": "candy", "d": "dog"},
			},
		},
		"merge array with existing elements into subset array becomes new array": {
			[]interface{}{
				map[string]interface{}{"a": "apple", "b": "boy"},
				map[string]interface{}{"b": "boy", "c": "candy"},
				map[string]interface{}{"c": "candy", "d": "dog"},
			},
			[]interface{}{
				map[string]interface{}{"a": "apple", "b": "boy"},
				map[string]interface{}{"c": "candy", "d": "dog"},
			},
			[]interface{}{
				map[string]interface{}{"a": "apple", "b": "boy"},
				map[string]interface{}{"b": "boy", "c": "candy"},
				map[string]interface{}{"c": "candy", "d": "dog"},
			},
		},
		"merge two differing single-element arrays becomes array with both elements": {
			[]interface{}{
				map[string]interface{}{
					"apiGroups": []string{"extensions", "apps"},
					"resources": []string{"deployments"},
					"verbs":     []string{"get", "list"},
				},
			},
			[]interface{}{
				map[string]interface{}{
					"apiGroups": []string{"extensions", "apps"},
					"resources": []string{"deployments"},
					"verbs":     []string{"get", "list", "watch", "create", "delete"},
				},
			},
			[]interface{}{
				map[string]interface{}{
					"apiGroups": []string{"extensions", "apps"},
					"resources": []string{"deployments"},
					"verbs":     []string{"get", "list"},
				},
				map[string]interface{}{
					"apiGroups": []string{"extensions", "apps"},
					"resources": []string{"deployments"},
					"verbs":     []string{"get", "list", "watch", "create", "delete"},
				},
			},
		},
	}

	for testName, test := range testcases {
		t.Run(testName, func(t *testing.T) {
			t.Parallel()

			actualMergedList, _ := mergeArrays(test.desiredList, test.currentList, "musthave", true)
			assert.Equal(t, fmt.Sprintf("%+v", test.expectedList), fmt.Sprintf("%+v", actualMergedList))
			check, _ := checkListsAreEquivalent(test.expectedList, actualMergedList)
			assert.True(t, check)
		})
	}
}

func TestMergeArraysMustOnlyHave(t *testing.T) {
	t.Parallel()

	testcases := map[string]struct {
		desiredList  []interface{}
		currentList  []interface{}
		expectedList []interface{}
	}{
		"merge array with one element into array with two becomes new array": {
			[]interface{}{
				map[string]interface{}{"a": "apple", "b": "boy"},
			},
			[]interface{}{
				map[string]interface{}{"a": "apple", "b": "boy"},
				map[string]interface{}{"c": "candy", "d": "dog"},
			},
			[]interface{}{
				map[string]interface{}{"a": "apple", "b": "boy"},
			},
		},
		"merge array with partial existing element into array becomes new array": {
			[]interface{}{
				map[string]interface{}{"b": "boy"},
			},
			[]interface{}{
				map[string]interface{}{"a": "apple", "b": "boy"},
				map[string]interface{}{"c": "candy", "d": "dog"},
			},
			[]interface{}{
				map[string]interface{}{"b": "boy"},
			},
		},
		"merge array with existing elements into subset array becomes new array": {
			[]interface{}{
				map[string]interface{}{"a": "apple", "b": "boy"},
				map[string]interface{}{"b": "boy", "c": "candy"},
				map[string]interface{}{"c": "candy", "d": "dog"},
			},
			[]interface{}{
				map[string]interface{}{"a": "apple", "b": "boy"},
				map[string]interface{}{"c": "candy", "d": "dog"},
			},
			[]interface{}{
				map[string]interface{}{"a": "apple", "b": "boy"},
				map[string]interface{}{"b": "boy", "c": "candy"},
				map[string]interface{}{"c": "candy", "d": "dog"},
			},
		},
		"merge two differing single-element arrays becomes new array": {
			[]interface{}{
				map[string]interface{}{
					"apiGroups": []string{"extensions", "apps"},
					"resources": []string{"deployments"},
					"verbs":     []string{"get", "list"},
				},
			},
			[]interface{}{
				map[string]interface{}{
					"apiGroups": []string{"extensions", "apps"},
					"resources": []string{"deployments"},
					"verbs":     []string{"get", "list", "watch", "create", "delete"},
				},
			},
			[]interface{}{
				map[string]interface{}{
					"apiGroups": []string{"extensions", "apps"},
					"resources": []string{"deployments"},
					"verbs":     []string{"get", "list"},
				},
			},
		},
	}

	for testName, test := range testcases {
		t.Run(testName, func(t *testing.T) {
			t.Parallel()

			actualMergedList, _ := mergeArrays(test.desiredList, test.currentList, "mustonlyhave", true)
			assert.Equal(t, fmt.Sprintf("%+v", test.expectedList), fmt.Sprintf("%+v", actualMergedList))
			check, _ := checkListsAreEquivalent(test.expectedList, actualMergedList)
			assert.True(t, check)
		})
	}
}

func TestCheckListsAreEquivalent(t *testing.T) {
	twoFullItems := []interface{}{
		map[string]interface{}{
			"a": "apple",
			"b": "boy",
		},
		map[string]interface{}{
			"c": "candy",
			"d": "dog",
		},
	}

	check, _ := checkListsAreEquivalent(twoFullItems, twoFullItems)
	assert.True(t, check)

	twoFullItemsDifferentOrder := []interface{}{
		map[string]interface{}{
			"c": "candy",
			"d": "dog",
		},
		map[string]interface{}{
			"a": "apple",
			"b": "boy",
		},
	}

	check, _ = checkListsAreEquivalent(twoFullItems, twoFullItemsDifferentOrder)
	assert.True(t, check)
	check, _ = checkListsAreEquivalent(twoFullItemsDifferentOrder, twoFullItems)
	assert.True(t, check)

	oneFullItem := []interface{}{
		map[string]interface{}{
			"a": "apple",
			"b": "boy",
		},
	}

	check, _ = checkListsAreEquivalent(twoFullItems, oneFullItem)
	assert.False(t, check)
	check, _ = checkListsAreEquivalent(oneFullItem, twoFullItems)
	assert.False(t, check)

	oneSmallItem := []interface{}{
		map[string]interface{}{
			"b": "boy",
		},
	}

	check, _ = checkListsAreEquivalent(twoFullItems, oneSmallItem)
	assert.False(t, check)
	check, _ = checkListsAreEquivalent(oneSmallItem, twoFullItems)
	assert.False(t, check)

	twoSmallItems := []interface{}{
		map[string]interface{}{
			"a": "apple",
		},
		map[string]interface{}{
			"c": "candy",
		},
	}

	check, _ = checkListsAreEquivalent(twoFullItems, twoSmallItems)
	assert.False(t, check)
	check, _ = checkListsAreEquivalent(twoSmallItems, twoFullItems)
	assert.False(t, check)

	oneSmallOneBig := []interface{}{
		map[string]interface{}{
			"a": "apple",
		},
		map[string]interface{}{
			"c": "candy",
			"d": "dog",
		},
	}

	check, _ = checkListsAreEquivalent(twoFullItems, oneSmallOneBig)
	assert.False(t, check)
	check, _ = checkListsAreEquivalent(oneSmallOneBig, twoFullItems)
	assert.False(t, check)

	oneBigOneSmall := []interface{}{
		map[string]interface{}{
			"a": "apple",
			"b": "boy",
		},
		map[string]interface{}{
			"c": "candy",
		},
	}

	check, _ = checkListsAreEquivalent(twoFullItems, oneBigOneSmall)
	assert.False(t, check)
	check, _ = checkListsAreEquivalent(oneBigOneSmall, twoFullItems)
	assert.False(t, check)
}

func TestCheckListsMatchDiffMapLength(t *testing.T) {
	existingObject := []interface{}{
		map[string]interface{}{
			"containers": []interface{}{
				map[string]interface{}{
					"name":  "my-container",
					"image": "quay.io/org/test:latest",
				},
			},
		},
	}

	mergedObject := []interface{}{
		map[string]interface{}{
			"containers": []interface{}{
				map[string]interface{}{
					"name":  "my-container",
					"image": "quay.io/org/test:latest",
					"stdin": false,
					"tty":   false,
				},
			},
		},
	}

	check, _ := checkListsAreEquivalent(existingObject, mergedObject)
	assert.True(t, check)
}

func TestNestedUnsortedLists(t *testing.T) {
	objDefYaml := `
kind: FakeOperator
status:
  refs:
    - conditions:
        - status: "False"
          type: CatalogSourcesUnhealthy
      kind: Subscription
`

	orderOneYaml := `
kind: FakeOperator
status:
  refs:
    - apiVersion: operators.coreos.com/v1alpha1
      conditions:
      - lastTransitionTime: "2023-10-22T06:49:54Z"
    - apiVersion: operators.coreos.com/v1alpha1
      conditions:
        - lastTransitionTime: "2023-10-22T06:40:14Z"
          status: "False"
          type: CatalogSourcesUnhealthy
        - status: "False"
          type: BundleUnpacking
      kind: Subscription
`

	orderTwoYaml := `
kind: FakeOperator
status:
  refs:
    - apiVersion: operators.coreos.com/v1alpha1
      conditions:
      - lastTransitionTime: "2023-10-22T06:49:54Z"
    - apiVersion: operators.coreos.com/v1alpha1
      conditions:
        - status: "False"
          type: BundleUnpacking
        - lastTransitionTime: "2023-10-22T06:40:14Z"
          status: "False"
          type: CatalogSourcesUnhealthy
      kind: Subscription
`

	policyObjDef := make(map[string]interface{})

	err := yaml.UnmarshalStrict([]byte(objDefYaml), &policyObjDef)
	if err != nil {
		t.Error(err)
	}

	orderOneObj := make(map[string]interface{})

	err = yaml.UnmarshalStrict([]byte(orderOneYaml), &orderOneObj)
	if err != nil {
		t.Error(err)
	}

	orderTwoObj := make(map[string]interface{})

	err = yaml.UnmarshalStrict([]byte(orderTwoYaml), &orderTwoObj)
	if err != nil {
		t.Error(err)
	}

	desiredObj := &unstructured.Unstructured{Object: policyObjDef}
	existingObjOrderOne := unstructured.Unstructured{Object: orderOneObj}
	existingObjOrderTwo := unstructured.Unstructured{Object: orderTwoObj}

	//nolint:dogsled
	errormsg, updateNeeded, _, _, _ := handleSingleKey("status", desiredObj, &existingObjOrderOne, "musthave", true)
	if len(errormsg) != 0 {
		t.Error("Got unexpected error message", errormsg)
	}

	assert.False(t, updateNeeded)

	//nolint:dogsled
	errormsg, updateNeeded, _, _, _ = handleSingleKey("status", desiredObj, &existingObjOrderTwo, "musthave", true)
	if len(errormsg) != 0 {
		t.Error("Got unexpected error message", errormsg)
	}

	assert.False(t, updateNeeded)
}

func TestAddRelatedObject(t *testing.T) {
	compliant := true
	scopedGVR := depclient.ScopedGVR{
		GroupVersionResource: policyv1.SchemeBuilder.GroupVersion.WithResource("ConfigurationPolicy"),
		Namespaced:           true,
	}
	namespace := "default"
	name := "foo"
	reason := "reason"
	relatedList := addRelatedObjects(
		compliant, scopedGVR, "ConfigurationPolicy", namespace, []string{name}, reason, nil,
	)
	related := relatedList[0]

	// get the related object and validate what we added is in the status
	assert.Equal(t, string(policyv1.Compliant), related.Compliant)
	assert.Equal(t, "reason", related.Reason)
	assert.Equal(t, scopedGVR.GroupVersion().String(), related.Object.APIVersion)
	assert.Equal(t, "ConfigurationPolicy", related.Object.Kind)
	assert.Equal(t, name, related.Object.Metadata.Name)
	assert.Equal(t, namespace, related.Object.Metadata.Namespace)

	// add the same object and make sure the existing one is overwritten
	reason = "new"
	compliant = false
	relatedList = addRelatedObjects(compliant, scopedGVR, "ConfigurationPolicy", namespace, []string{name}, reason, nil)
	related = relatedList[0]

	assert.Len(t, relatedList, 1)
	assert.Equal(t, string(policyv1.NonCompliant), related.Compliant)
	assert.Equal(t, "new", related.Reason)

	// add a new related object and make sure the entry is appended
	name = "bar"
	relatedList = append(
		relatedList,
		addRelatedObjects(compliant, scopedGVR, "ConfigurationPolicy", namespace, []string{name}, reason, nil)...,
	)

	assert.Len(t, relatedList, 2)

	related = relatedList[1]

	assert.Equal(t, name, related.Object.Metadata.Name)
}

func TestUpdatedRelatedObjects(t *testing.T) {
	r := &ConfigurationPolicyReconciler{}

	policy := &policyv1.ConfigurationPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: "default",
		},
		Spec: policyv1.ConfigurationPolicySpec{
			Severity: "low",
			NamespaceSelector: policyv1.Target{
				Include: []policyv1.NonEmptyString{"default", "kube-*"},
				Exclude: []policyv1.NonEmptyString{"kube-system"},
			},
			RemediationAction: "inform",
			ObjectTemplates: []*policyv1.ObjectTemplate{
				{
					ComplianceType:   "musthave",
					ObjectDefinition: runtime.RawExtension{},
				},
			},
		},
	}
	scopedGVR := depclient.ScopedGVR{
		GroupVersionResource: policyv1.SchemeBuilder.GroupVersion.WithResource("ConfigurationPolicy"),
		Namespaced:           true,
	}
	name := "foo"
	relatedList := addRelatedObjects(true, scopedGVR, "ConfigurationPolicy", "default", []string{name}, "reason", nil)

	// add the same object but after sorting it should be first
	name = "bar"
	relatedList = append(relatedList, addRelatedObjects(
		true, scopedGVR, "ConfigurationPolicy", "default", []string{name}, "reason", nil)...,
	)

	r.updatedRelatedObjects(policy, relatedList)
	assert.Equal(t, "bar", relatedList[0].Object.Metadata.Name)

	// append another object named bar but also with namespace bar
	relatedList = append(relatedList, addRelatedObjects(
		true, scopedGVR, "ConfigurationPolicy", "bar", []string{name}, "reason", nil)...,
	)

	r.updatedRelatedObjects(policy, relatedList)
	assert.Equal(t, "bar", relatedList[0].Object.Metadata.Namespace)

	// clear related objects and test sorting with no namespace
	scopedGVR.Namespaced = false
	name = "foo"
	relatedList = addRelatedObjects(true, scopedGVR, "ConfigurationPolicy", "", []string{name}, "reason", nil)
	name = "bar"
	relatedList = append(relatedList, addRelatedObjects(
		true, scopedGVR, "ConfigurationPolicy", "", []string{name}, "reason", nil)...,
	)

	r.updatedRelatedObjects(policy, relatedList)
	assert.Equal(t, "bar", relatedList[0].Object.Metadata.Name)
}

func TestAddRelatedObjectProperties(t *testing.T) {
	compliant := true
	scopedGVR := depclient.ScopedGVR{
		GroupVersionResource: policyv1.SchemeBuilder.GroupVersion.WithResource("ConfigurationPolicy"),
		Namespaced:           true,
	}
	namespace := "default"
	name := "foo"
	reason := "reason"

	// Create test data for ObjectProperties
	createdByPolicy := true
	testUID := "test-uid-12345"
	testDiff := "test diff content"
	dryRunMatches := true

	creationInfo := &policyv1.ObjectProperties{
		CreatedByPolicy:    &createdByPolicy,
		UID:                testUID,
		Diff:               testDiff,
		MatchesAfterDryRun: dryRunMatches,
	}

	relatedList := addRelatedObjects(
		compliant, scopedGVR, "ConfigurationPolicy", namespace, []string{name}, reason, creationInfo,
	)
	related := relatedList[0]

	// Validate that the properties are correctly set
	assert.NotNil(t, related.Properties, "Properties should not be nil when creationInfo is provided")
	assert.NotNil(t, related.Properties.CreatedByPolicy, "CreatedByPolicy should not be nil")
	assert.Equal(t, createdByPolicy, *related.Properties.CreatedByPolicy)
	assert.Equal(t, testUID, related.Properties.UID)
	assert.Equal(t, testDiff, related.Properties.Diff)
	assert.Equal(t, dryRunMatches, related.Properties.MatchesAfterDryRun)

	// add the same object and make sure the existing one is overwritten
	reason = "new"
	compliant = false

	// Create new test data for ObjectProperties with different values
	newCreatedByPolicy := false
	newTestDiff := "updated diff content"
	newDryRunMatches := false

	newCreationInfo := &policyv1.ObjectProperties{
		CreatedByPolicy:    &newCreatedByPolicy,
		UID:                testUID,
		Diff:               newTestDiff,
		MatchesAfterDryRun: newDryRunMatches,
	}

	relatedList = addRelatedObjects(
		compliant, scopedGVR, "ConfigurationPolicy", namespace, []string{name}, reason, newCreationInfo)
	related = relatedList[0]

	assert.Len(t, relatedList, 1)
	assert.Equal(t, string(policyv1.NonCompliant), related.Compliant)
	assert.Equal(t, "new", related.Reason)

	// Validate that the properties were updated with new values
	assert.NotNil(t, related.Properties, "Properties should not be nil when newCreationInfo is provided")
	assert.NotNil(t, related.Properties.CreatedByPolicy, "CreatedByPolicy should not be nil")
	assert.Equal(t, newCreatedByPolicy, *related.Properties.CreatedByPolicy)
	assert.Equal(t, testUID, related.Properties.UID)
	assert.Equal(t, newTestDiff, related.Properties.Diff)
	assert.Equal(t, newDryRunMatches, related.Properties.MatchesAfterDryRun)
}

func TestCreateStatus(t *testing.T) {
	testcases := []struct {
		testName          string
		resourceName      string
		namespaceToEvent  map[string]*objectTmplEvalResultWithEvent
		expectedCompliant bool
		expectedReason    string
		expectedMsg       string
	}{
		{
			"must have single object compliant",
			"configmaps",
			map[string]*objectTmplEvalResultWithEvent{
				"toy-story": {
					result: objectTmplEvalResult{
						objectNames: []string{"buzz"},
					},
					event: objectTmplEvalEvent{
						compliant: true,
						reason:    reasonWantFoundExists,
					},
				},
			},
			true,
			"K8s `must have` object already exists",
			"configmaps [buzz] found as specified in namespace toy-story",
		},
		{
			"must have single object compliant cluster-scoped",
			"namespaces",
			map[string]*objectTmplEvalResultWithEvent{
				"": {
					result: objectTmplEvalResult{
						objectNames: []string{"movies"},
					},
					event: objectTmplEvalEvent{
						compliant: true,
						reason:    reasonWantFoundExists,
					},
				},
			},
			true,
			"K8s `must have` object already exists",
			"namespaces [movies] found as specified",
		},
		{
			"must have multiple namespaces single object compliant",
			"configmaps",
			map[string]*objectTmplEvalResultWithEvent{
				"toy-story": {
					result: objectTmplEvalResult{
						objectNames: []string{"buzz"},
					},
					event: objectTmplEvalEvent{
						compliant: true,
						reason:    reasonWantFoundExists,
					},
				},
				"toy-story3": {
					result: objectTmplEvalResult{
						objectNames: []string{"buzz"},
					},
					event: objectTmplEvalEvent{
						compliant: true,
						reason:    reasonWantFoundExists,
					},
				},
			},
			true,
			"K8s `must have` object already exists",
			"configmaps [buzz] found as specified in namespaces: toy-story, toy-story3",
		},
		{
			"must have unnamed object compliant",
			"secrets",
			map[string]*objectTmplEvalResultWithEvent{
				"toy-story": {
					result: objectTmplEvalResult{
						objectNames: []string{"buzz"},
					},
					event: objectTmplEvalEvent{
						compliant: true,
						reason:    reasonWantFoundExists,
					},
				},
				"toy-story4": {
					result: objectTmplEvalResult{
						objectNames: []string{"bo-peep"},
					},
					event: objectTmplEvalEvent{
						compliant: true,
						reason:    reasonWantFoundExists,
					},
				},
			},
			true,
			"K8s `must have` object already exists",
			"secrets [buzz] found as specified in namespace toy-story; " +
				"secrets [bo-peep] found as specified in namespace toy-story4",
		},
		{
			"must have single object created",
			"configmaps",
			map[string]*objectTmplEvalResultWithEvent{
				"toy-story": {
					result: objectTmplEvalResult{
						objectNames: []string{"buzz"},
					},
					event: objectTmplEvalEvent{
						compliant: true,
						reason:    reasonWantFoundCreated,
					},
				},
			},
			true,
			"K8s creation success",
			"configmaps [buzz] was created successfully in namespace toy-story",
		},
		{
			"must have single object created in one namespace and exists in another",
			"configmaps",
			map[string]*objectTmplEvalResultWithEvent{
				"toy-story": {
					result: objectTmplEvalResult{
						objectNames: []string{"buzz"},
					},
					event: objectTmplEvalEvent{
						compliant: true,
						reason:    reasonWantFoundCreated,
					},
				},
				"toy-story4": {
					result: objectTmplEvalResult{
						objectNames: []string{"buzz"},
					},
					event: objectTmplEvalEvent{
						compliant: true,
						reason:    reasonWantFoundExists,
					},
				},
			},
			true,
			"K8s `must have` object already exists; K8s creation success",
			"configmaps [buzz] found as specified in namespace toy-story4; configmaps [buzz] was created " +
				"successfully in namespace toy-story",
		},
		{
			"must have single object not found in one of the namespaces",
			"configmaps",
			map[string]*objectTmplEvalResultWithEvent{
				"toy-story": {
					result: objectTmplEvalResult{
						objectNames: []string{"buzz"},
					},
					event: objectTmplEvalEvent{
						compliant: true,
						reason:    reasonWantFoundExists,
					},
				},
				"toy-story4": {
					result: objectTmplEvalResult{
						objectNames: []string{"buzz"},
					},
					event: objectTmplEvalEvent{
						compliant: false,
						reason:    reasonWantFoundDNE,
					},
				},
			},
			false,
			"K8s does not have a `must have` object",
			"configmaps [buzz] not found in namespace toy-story4",
		},
		{
			"must have single object no match",
			"configmaps",
			map[string]*objectTmplEvalResultWithEvent{
				"toy-story": {
					result: objectTmplEvalResult{
						objectNames: []string{"buzz"},
					},
					event: objectTmplEvalEvent{
						compliant: false,
						reason:    reasonWantFoundNoMatch,
					},
				},
			},
			false,
			"K8s does not have a `must have` object",
			"configmaps [buzz] found but not as specified in namespace toy-story",
		},
		{
			"must not have single object exists",
			"configmaps",
			map[string]*objectTmplEvalResultWithEvent{
				"toy-story": {
					result: objectTmplEvalResult{
						objectNames: []string{"buzz"},
					},
					event: objectTmplEvalEvent{
						compliant: false,
						reason:    reasonWantNotFoundExists,
					},
				},
			},
			false,
			"K8s has a `must not have` object",
			"configmaps [buzz] found in namespace toy-story",
		},
		{
			"must not have single object not found",
			"configmaps",
			map[string]*objectTmplEvalResultWithEvent{
				"toy-story": {
					result: objectTmplEvalResult{
						objectNames: []string{"buzz"},
					},
					event: objectTmplEvalEvent{
						compliant: true,
						reason:    reasonWantNotFoundDNE,
					},
				},
			},
			true,
			"K8s `must not have` object already missing",
			"configmaps [buzz] missing as expected in namespace toy-story",
		},
		{
			"must not have single object deleted",
			"configmaps",
			map[string]*objectTmplEvalResultWithEvent{
				"toy-story": {
					result: objectTmplEvalResult{
						objectNames: []string{"buzz"},
					},
					event: objectTmplEvalEvent{
						compliant: true,
						reason:    reasonDeleteSuccess,
					},
				},
			},
			true,
			"K8s deletion success",
			"configmaps [buzz] was deleted successfully in namespace toy-story",
		},
		{
			"unnamed object single error",
			"configmaps",
			map[string]*objectTmplEvalResultWithEvent{
				"toy-story": {
					result: objectTmplEvalResult{
						objectNames: []string{""},
					},
					event: objectTmplEvalEvent{
						compliant: false,
						reason:    "K8s missing namespace",
						message: "namespaced object of kind ConfigMap has no namespace specified " +
							"from the policy namespaceSelector nor the object metadata",
					},
				},
			},
			false,
			"K8s missing namespace",
			"namespaced object of kind ConfigMap has no namespace specified from the policy namespaceSelector " +
				"nor the object metadata",
		},
		{
			"unnamed object multiple error",
			"configmaps",
			map[string]*objectTmplEvalResultWithEvent{
				"toy-story1": {
					result: objectTmplEvalResult{
						objectNames: []string{"rex", "woody"},
					},
					event: objectTmplEvalEvent{
						compliant: false,
						reason:    reasonWantFoundNoMatch,
					},
				},
				"toy-story2": {
					result: objectTmplEvalResult{
						objectNames: []string{"buzz", "potato"},
					},
					event: objectTmplEvalEvent{
						compliant: false,
						reason:    reasonWantFoundNoMatch,
					},
				},
			},
			false,
			"K8s does not have a `must have` object",
			"configmaps [rex, woody] found but not as specified in namespace toy-story1; " +
				"configmaps [buzz, potato] found but not as specified in namespace toy-story2",
		},
		{
			"multiple errors",
			"configmaps",
			map[string]*objectTmplEvalResultWithEvent{
				"toy-story": {
					result: objectTmplEvalResult{
						objectNames: []string{"buzz"},
					},
					event: objectTmplEvalEvent{
						compliant: false,
						reason:    "K8s missing namespace",
						message: "namespaced object buzz of kind ConfigMap has no namespace specified " +
							"from the policy namespaceSelector nor the object metadata",
					},
				},
				"toy-story4": {
					result: objectTmplEvalResult{
						objectNames: []string{"buzz"},
					},
					event: objectTmplEvalEvent{
						compliant: false,
						reason:    "K8s decode object definition error",
						message: "Decoding error, please check your policy file! Aborting handling the object " +
							"template at index [0] in policy `create-configmaps` with error = `some error`",
					},
				},
			},
			false,
			"K8s decode object definition error; K8s missing namespace",
			"Decoding error, please check your policy file! Aborting handling the object template at index [0] in " +
				"policy `create-configmaps` with error = `some error`; namespaced object buzz of kind ConfigMap has " +
				"no namespace specified from the policy namespaceSelector nor the object metadata",
		},
	}

	for _, test := range testcases {
		t.Run(test.testName, func(t *testing.T) {
			compliant, reason, msg := createStatus(test.resourceName, test.namespaceToEvent)

			assert.Equal(t, test.expectedCompliant, compliant)
			assert.Equal(t, test.expectedReason, reason)
			assert.Equal(t, test.expectedMsg, msg)
		})
	}
}

func TestHandleKeysServiceAccount(t *testing.T) {
	t.Parallel()

	desiredServiceAccountYaml := `
apiVersion: v1
kind: ServiceAccount
metadata:
  name: sa-test
  namespace: sa-test
`

	existingServiceAccountYaml := `
apiVersion: v1
imagePullSecrets:
  name: test-user-dockercfg
kind: ServiceAccount
metadata:
  name: sa-test
  namespace: sa-test
secrets:
  name: test-user-dockercfg
`

	serviceAccountObj := make(map[string]interface{})

	err := yaml.UnmarshalStrict([]byte(desiredServiceAccountYaml), &serviceAccountObj)
	if err != nil {
		t.Error(err)
	}

	existingServiceAccountObj := make(map[string]interface{})

	err = yaml.UnmarshalStrict([]byte(existingServiceAccountYaml), &existingServiceAccountObj)
	if err != nil {
		t.Error(err)
	}

	existingServiceAccountObjCopy := make(map[string]interface{})

	err = yaml.UnmarshalStrict([]byte(existingServiceAccountYaml), &existingServiceAccountObjCopy)
	if err != nil {
		t.Error(err)
	}

	desiredObj := unstructured.Unstructured{Object: serviceAccountObj}
	existingObj := unstructured.Unstructured{Object: existingServiceAccountObj}
	existingObjCopy := unstructured.Unstructured{Object: existingServiceAccountObjCopy}
	compType := policyv1.MustOnlyHave
	mdCompType := policyv1.MustOnlyHave

	throwSpecViolation, _, updateNeeded, statusMismatch, _ := handleKeys(&desiredObj, &existingObj,
		&existingObjCopy, compType, mdCompType)

	assert.False(t, throwSpecViolation)
	assert.False(t, updateNeeded)
	assert.False(t, statusMismatch)
}

func TestShouldEvaluatePolicy(t *testing.T) {
	t.Parallel()

	policy := policyv1.ConfigurationPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "policy",
			Namespace:  "managed",
			Generation: 2,
			UID:        uuid.NewUUID(),
		},
		Spec: policyv1.ConfigurationPolicySpec{},
	}

	// Add a 60 second buffer to avoid race conditions
	inFuture := time.Now().UTC().Add(60 * time.Second).Format(time.RFC3339)
	futureResourceVersion := "900000" // 900_000
	lastEvaluatedInFuture := &sync.Map{}
	lastEvaluatedInFuture.Store(policy.UID, futureResourceVersion)

	lastEvaluatedInvalid := &sync.Map{}
	lastEvaluatedInvalid.Store(policy.UID, "Do or do not. There is no try.")

	twelveSecsAgo := time.Now().UTC().Add(-12 * time.Second).Format(time.RFC3339)
	recentResourceVersion := "700000" // 700_000
	lastEvaluatedTwelveSecsAgo := &sync.Map{}
	lastEvaluatedTwelveSecsAgo.Store(policy.UID, recentResourceVersion)

	twelveHoursAgo := time.Now().UTC().Add(-12 * time.Hour).Format(time.RFC3339)
	oldResourceVersion := "20000" // 20_000
	lastEvaluatedTwelveHoursAgo := &sync.Map{}
	lastEvaluatedTwelveHoursAgo.Store(policy.UID, oldResourceVersion)

	tests := []struct {
		testDescription          string
		resourceVersion          string
		lastEvaluated            string
		lastEvaluatedGeneration  int64
		evaluationInterval       policyv1.EvaluationInterval
		complianceState          policyv1.ComplianceState
		expected                 bool
		expectedPositiveDuration bool
		deletionTimestamp        *metav1.Time
		finalizers               []string
		lastEvaluatedCache       *sync.Map
	}{
		{
			"Just evaluated and the generation is unchanged",
			futureResourceVersion,
			inFuture,
			2,
			policyv1.EvaluationInterval{Compliant: "10s", NonCompliant: "10s"},
			policyv1.Compliant,
			false,
			true,
			nil,
			[]string{},
			lastEvaluatedInFuture,
		},
		{
			"The generation has changed",
			futureResourceVersion,
			inFuture,
			1,
			policyv1.EvaluationInterval{Compliant: "10s", NonCompliant: "10s"},
			policyv1.Compliant,
			true,
			false,
			nil,
			[]string{},
			lastEvaluatedInFuture,
		},
		{
			"lastEvaluated not set",
			futureResourceVersion,
			"",
			2,
			policyv1.EvaluationInterval{Compliant: "10s", NonCompliant: "10s"},
			policyv1.Compliant,
			true,
			false,
			nil,
			[]string{},
			&sync.Map{},
		},
		{
			"Invalid lastEvaluated",
			futureResourceVersion,
			"Do or do not. There is no try.",
			2,
			policyv1.EvaluationInterval{Compliant: "10s", NonCompliant: "10s"},
			policyv1.Compliant,
			true,
			false,
			nil,
			[]string{},
			lastEvaluatedInvalid,
		},
		{
			"Unknown compliance state",
			futureResourceVersion,
			inFuture,
			2,
			policyv1.EvaluationInterval{Compliant: "10s", NonCompliant: "10s"},
			policyv1.UnknownCompliancy,
			true,
			false,
			nil,
			[]string{},
			lastEvaluatedInFuture,
		},
		{
			"Default evaluation interval with a past lastEvaluated when compliant",
			futureResourceVersion,
			twelveSecsAgo,
			2,
			policyv1.EvaluationInterval{Compliant: "10s", NonCompliant: "10s"},
			policyv1.Compliant,
			true,
			false,
			nil,
			[]string{},
			lastEvaluatedTwelveSecsAgo,
		},
		{
			"Default evaluation interval with a past lastEvaluated when noncompliant",
			futureResourceVersion,
			twelveSecsAgo,
			2,
			policyv1.EvaluationInterval{Compliant: "10s", NonCompliant: "10s"},
			policyv1.NonCompliant,
			true,
			false,
			nil,
			[]string{},
			lastEvaluatedTwelveSecsAgo,
		},
		{
			"Never evaluation interval with past lastEvaluated when compliant",
			futureResourceVersion,
			twelveHoursAgo,
			2,
			policyv1.EvaluationInterval{Compliant: "never"},
			policyv1.Compliant,
			false,
			false,
			nil,
			[]string{},
			lastEvaluatedTwelveHoursAgo,
		},
		{
			"Never evaluation interval with past lastEvaluated when noncompliant",
			futureResourceVersion,
			twelveHoursAgo,
			2,
			policyv1.EvaluationInterval{Compliant: "10s", NonCompliant: "never"},
			policyv1.NonCompliant,
			false,
			false,
			nil,
			[]string{},
			lastEvaluatedTwelveHoursAgo,
		},
		{
			"Unset evaluation interval with past lastEvaluated when compliant",
			futureResourceVersion,
			twelveHoursAgo,
			2,
			policyv1.EvaluationInterval{},
			policyv1.Compliant,
			true,
			false,
			nil,
			[]string{},
			lastEvaluatedTwelveHoursAgo,
		},
		{
			"Watch evaluation interval with past lastEvaluated when compliant",
			futureResourceVersion,
			twelveHoursAgo,
			2,
			policyv1.EvaluationInterval{Compliant: "watch", NonCompliant: "watch"},
			policyv1.Compliant,
			true,
			false,
			nil,
			[]string{},
			lastEvaluatedTwelveHoursAgo,
		},
		{
			"Unset evaluation interval with past lastEvaluated when noncompliant",
			futureResourceVersion,
			twelveHoursAgo,
			2,
			policyv1.EvaluationInterval{},
			policyv1.NonCompliant,
			true,
			false,
			nil,
			[]string{},
			lastEvaluatedTwelveHoursAgo,
		},
		{
			"Watch evaluation interval with past lastEvaluated when noncompliant",
			futureResourceVersion,
			twelveHoursAgo,
			2,
			policyv1.EvaluationInterval{NonCompliant: "watch"},
			policyv1.NonCompliant,
			true,
			false,
			nil,
			[]string{},
			lastEvaluatedTwelveHoursAgo,
		},
		{
			"Invalid evaluation interval when compliant",
			futureResourceVersion,
			inFuture,
			2,
			policyv1.EvaluationInterval{Compliant: "Do or do not. There is no try."},
			policyv1.Compliant,
			true,
			false,
			nil,
			[]string{},
			lastEvaluatedInFuture,
		},
		{
			"Invalid evaluation interval when noncompliant",
			futureResourceVersion,
			inFuture,
			2,
			policyv1.EvaluationInterval{NonCompliant: "Do or do not. There is no try."},
			policyv1.NonCompliant,
			true,
			false,
			nil,
			[]string{},
			lastEvaluatedInFuture,
		},
		{
			"Custom evaluation interval that hasn't past yet when compliant",
			futureResourceVersion,
			twelveSecsAgo,
			2,
			policyv1.EvaluationInterval{Compliant: "12h"},
			policyv1.Compliant,
			false,
			true,
			nil,
			[]string{},
			lastEvaluatedTwelveSecsAgo,
		},
		{
			"Custom evaluation interval that hasn't past yet when noncompliant",
			futureResourceVersion,
			twelveSecsAgo,
			2,
			policyv1.EvaluationInterval{NonCompliant: "12h"},
			policyv1.NonCompliant,
			false,
			true,
			nil,
			[]string{},
			lastEvaluatedTwelveSecsAgo,
		},
		{
			"Deletion timestamp is non nil",
			futureResourceVersion,
			inFuture,
			2,
			policyv1.EvaluationInterval{NonCompliant: "12h"},
			policyv1.NonCompliant,
			true,
			false,
			&metav1.Time{Time: time.Now()},
			[]string{},
			lastEvaluatedInFuture,
		},
		{
			"controller-runtime cache not yet synced",
			recentResourceVersion,
			twelveHoursAgo,
			2,
			policyv1.EvaluationInterval{NonCompliant: "12h"},
			policyv1.NonCompliant,
			false,
			true,
			nil,
			[]string{},
			lastEvaluatedInFuture,
		},
	}

	for _, test := range tests {
		t.Run(
			test.testDescription,
			func(t *testing.T) {
				t.Parallel()

				policyCopy := policy.DeepCopy()

				policyCopy.Status.LastEvaluated = test.lastEvaluated
				policyCopy.SetResourceVersion(test.resourceVersion)
				policyCopy.Status.LastEvaluatedGeneration = test.lastEvaluatedGeneration
				policyCopy.Spec.EvaluationInterval = test.evaluationInterval
				policyCopy.Status.ComplianceState = test.complianceState
				policyCopy.ObjectMeta.DeletionTimestamp = test.deletionTimestamp
				policyCopy.ObjectMeta.Finalizers = test.finalizers

				r := &ConfigurationPolicyReconciler{
					SelectorReconciler: &fakeSR{},
					lastEvaluatedCache: *test.lastEvaluatedCache, //nolint: govet
				}

				actual, actualDuration := r.shouldEvaluatePolicy(policyCopy)

				if actual != test.expected {
					t.Fatalf("expected %v but got %v", test.expected, actual)
				}

				if test.expectedPositiveDuration && actualDuration <= 0 {
					t.Fatalf("expected a positive duration but got %v", actualDuration.String())
				}

				if !test.expectedPositiveDuration && actualDuration > 0 {
					t.Fatalf("expected a zero duration but got %v", actualDuration.String())
				}
			},
		)
	}
}

type fakeSR struct{}

func (r *fakeSR) Get(_ string, _ string, _ policyv1.Target) ([]string, error) {
	return nil, nil
}

func (r *fakeSR) HasUpdate(_ string, _ string) bool {
	return false
}

func (r *fakeSR) Stop(_ string, _ string) {
}

func TestShouldHandleSingleKeyFalse(t *testing.T) {
	t.Parallel()

	var unstruct unstructured.Unstructured
	var unstructObj unstructured.Unstructured

	var update, skip bool

	type ExpectResult struct {
		key    string
		expect bool
	}

	type TestSingleKey struct {
		input        map[string]interface{}
		fromAPI      map[string]interface{}
		expectResult ExpectResult
	}

	tests := []TestSingleKey{
		{
			input: map[string]interface{}{
				"hostIPC":   false,
				"container": "test",
			},
			fromAPI: map[string]interface{}{
				"container": "test",
			},
			expectResult: ExpectResult{
				"hostIPC",
				false,
			},
		},
		{
			input: map[string]interface{}{
				"container": map[string]interface{}{
					"image":   "nginx1.7.9",
					"name":    "nginx",
					"hostIPC": false,
				},
			},
			fromAPI: map[string]interface{}{
				"container": map[string]interface{}{
					"image": "nginx1.7.9",
					"name":  "nginx",
				},
			},
			expectResult: ExpectResult{
				"container",
				false,
			},
		},
		{
			input: map[string]interface{}{
				"hostIPC":   true,
				"container": "test",
			},
			fromAPI: map[string]interface{}{
				"container": "test",
			},
			expectResult: ExpectResult{
				"hostIPC",
				true,
			},
		},
		{
			input: map[string]interface{}{
				"container": map[string]interface{}{
					"image":   "nginx1.7.9",
					"name":    "nginx",
					"hostIPC": true,
				},
			},
			fromAPI: map[string]interface{}{
				"container": map[string]interface{}{
					"image": "nginx1.7.9",
					"name":  "nginx",
				},
			},
			expectResult: ExpectResult{
				"container",
				true,
			},
		},
	}

	for _, test := range tests {
		unstruct.Object = test.input
		unstructObj.Object = test.fromAPI
		key := test.expectResult.key
		_, update, _, skip, _ = handleSingleKey(key, &unstruct, &unstructObj, "musthave", true)
		assert.Equal(t, update, test.expectResult.expect)
		assert.False(t, skip)
	}
}
