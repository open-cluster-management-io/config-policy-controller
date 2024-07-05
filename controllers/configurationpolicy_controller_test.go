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
		Spec: &policyv1.ConfigurationPolicySpec{
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

func TestCompareSpecs(t *testing.T) {
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

	merged, err := compareSpecs(spec1, spec2, "mustonlyhave", true)
	if err != nil {
		t.Fatalf("compareSpecs: (%v)", err)
	}

	mergedExpected := map[string]interface{}{
		"containers": map[string]string{
			"image": "nginx1.7.9",
			"name":  "nginx",
		},
	}
	assert.Equal(t, reflect.DeepEqual(merged, mergedExpected), true)

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

	merged, err = compareSpecs(spec1, spec2, "musthave", true)
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

	for i := 0; i < 3; i++ {
		compliantDetails = append(compliantDetails, compliantDetail)
	}

	samplePolicy := getSamplePolicy()

	samplePolicyStatus := policyv1.ConfigurationPolicyStatus{
		ComplianceState:   "Compliant",
		CompliancyDetails: compliantDetails,
	}
	samplePolicy.Status = samplePolicyStatus
	policyInString := convertPolicyStatusToString(&samplePolicy)

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
	statusMsg := convertPolicyStatusToString(&samplePolicy)

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
		testName := testName
		test := test

		t.Run(testName, func(t *testing.T) {
			t.Parallel()

			actualMergedList := mergeArrays(test.desiredList, test.currentList, "musthave", true)
			assert.Equal(t, fmt.Sprintf("%+v", test.expectedList), fmt.Sprintf("%+v", actualMergedList))
			assert.True(t, checkListsMatch(test.expectedList, actualMergedList))
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
		testName := testName
		test := test

		t.Run(testName, func(t *testing.T) {
			t.Parallel()

			actualMergedList := mergeArrays(test.desiredList, test.currentList, "mustonlyhave", true)
			assert.Equal(t, fmt.Sprintf("%+v", test.expectedList), fmt.Sprintf("%+v", actualMergedList))
			assert.True(t, checkListsMatch(test.expectedList, actualMergedList))
		})
	}
}

func TestCheckListsMatch(t *testing.T) {
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

	assert.True(t, checkListsMatch(twoFullItems, twoFullItems))

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

	assert.True(t, checkListsMatch(twoFullItems, twoFullItemsDifferentOrder))
	assert.True(t, checkListsMatch(twoFullItemsDifferentOrder, twoFullItems))

	oneFullItem := []interface{}{
		map[string]interface{}{
			"a": "apple",
			"b": "boy",
		},
	}

	assert.False(t, checkListsMatch(twoFullItems, oneFullItem))
	assert.False(t, checkListsMatch(oneFullItem, twoFullItems))

	oneSmallItem := []interface{}{
		map[string]interface{}{
			"b": "boy",
		},
	}

	assert.False(t, checkListsMatch(twoFullItems, oneSmallItem))
	assert.False(t, checkListsMatch(oneSmallItem, twoFullItems))

	twoSmallItems := []interface{}{
		map[string]interface{}{
			"a": "apple",
		},
		map[string]interface{}{
			"c": "candy",
		},
	}

	assert.False(t, checkListsMatch(twoFullItems, twoSmallItems))
	assert.False(t, checkListsMatch(twoSmallItems, twoFullItems))

	oneSmallOneBig := []interface{}{
		map[string]interface{}{
			"a": "apple",
		},
		map[string]interface{}{
			"c": "candy",
			"d": "dog",
		},
	}

	assert.False(t, checkListsMatch(twoFullItems, oneSmallOneBig))
	assert.False(t, checkListsMatch(oneSmallOneBig, twoFullItems))

	oneBigOneSmall := []interface{}{
		map[string]interface{}{
			"a": "apple",
			"b": "boy",
		},
		map[string]interface{}{
			"c": "candy",
		},
	}

	assert.False(t, checkListsMatch(twoFullItems, oneBigOneSmall))
	assert.False(t, checkListsMatch(oneBigOneSmall, twoFullItems))
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

	assert.True(t, checkListsMatch(existingObject, mergedObject))
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

	desiredObj := unstructured.Unstructured{Object: policyObjDef}
	existingObjOrderOne := unstructured.Unstructured{Object: orderOneObj}
	existingObjOrderTwo := unstructured.Unstructured{Object: orderTwoObj}

	errormsg, updateNeeded, _, _ := handleSingleKey("status", desiredObj, &existingObjOrderOne, "musthave", true)
	if len(errormsg) != 0 {
		t.Error("Got unexpected error message", errormsg)
	}

	assert.False(t, updateNeeded)

	errormsg, updateNeeded, _, _ = handleSingleKey("status", desiredObj, &existingObjOrderTwo, "musthave", true)
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
	assert.True(t, related.Compliant == string(policyv1.Compliant))
	assert.True(t, related.Reason == "reason")
	assert.True(t, related.Object.APIVersion == scopedGVR.GroupVersion().String())
	assert.True(t, related.Object.Kind == "ConfigurationPolicy")
	assert.True(t, related.Object.Metadata.Name == name)
	assert.True(t, related.Object.Metadata.Namespace == namespace)

	// add the same object and make sure the existing one is overwritten
	reason = "new"
	compliant = false
	relatedList = addRelatedObjects(compliant, scopedGVR, "ConfigurationPolicy", namespace, []string{name}, reason, nil)
	related = relatedList[0]

	assert.True(t, len(relatedList) == 1)
	assert.True(t, related.Compliant == string(policyv1.NonCompliant))
	assert.True(t, related.Reason == "new")

	// add a new related object and make sure the entry is appended
	name = "bar"
	relatedList = append(
		relatedList,
		addRelatedObjects(compliant, scopedGVR, "ConfigurationPolicy", namespace, []string{name}, reason, nil)...,
	)

	assert.True(t, len(relatedList) == 2)

	related = relatedList[1]

	assert.True(t, related.Object.Metadata.Name == name)
}

func TestSortRelatedObjectsAndUpdate(t *testing.T) {
	r := &ConfigurationPolicyReconciler{}

	policy := &policyv1.ConfigurationPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: "default",
		},
		Spec: &policyv1.ConfigurationPolicySpec{
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

	empty := []policyv1.RelatedObject{}

	r.sortRelatedObjectsAndUpdate(policy, relatedList, empty, false, true)
	assert.True(t, relatedList[0].Object.Metadata.Name == "bar")

	// append another object named bar but also with namespace bar
	relatedList = append(relatedList, addRelatedObjects(
		true, scopedGVR, "ConfigurationPolicy", "bar", []string{name}, "reason", nil)...,
	)

	r.sortRelatedObjectsAndUpdate(policy, relatedList, empty, false, true)
	assert.True(t, relatedList[0].Object.Metadata.Namespace == "bar")

	// clear related objects and test sorting with no namespace
	scopedGVR.Namespaced = false
	name = "foo"
	relatedList = addRelatedObjects(true, scopedGVR, "ConfigurationPolicy", "", []string{name}, "reason", nil)
	name = "bar"
	relatedList = append(relatedList, addRelatedObjects(
		true, scopedGVR, "ConfigurationPolicy", "", []string{name}, "reason", nil)...,
	)

	r.sortRelatedObjectsAndUpdate(policy, relatedList, empty, false, true)
	assert.True(t, relatedList[0].Object.Metadata.Name == "bar")
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
			"secrets [bo-peep] found as specified in namespace toy-story4; secrets [buzz] found as specified in " +
				"namespace toy-story",
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
		test := test

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
	compType := "mustonlyhave"
	mdCompType := ""
	zeroValueEqualsNil := false

	throwSpecViolation, _, updateNeeded, statusMismatch := handleKeys(desiredObj, &existingObj,
		&existingObjCopy, compType, mdCompType, zeroValueEqualsNil)

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
		Spec: &policyv1.ConfigurationPolicySpec{},
	}

	// Add a 60 second buffer to avoid race conditions
	inFuture := time.Now().UTC().Add(60 * time.Second).Format(time.RFC3339)
	lastEvaluatedInFuture := &sync.Map{}
	lastEvaluatedInFuture.Store(policy.UID, inFuture)

	lastEvaluatedInvalid := &sync.Map{}
	lastEvaluatedInvalid.Store(policy.UID, "Do or do not. There is no try.")

	twelveSecsAgo := time.Now().UTC().Add(-12 * time.Second).Format(time.RFC3339)
	lastEvaluatedTwelveSecsAgo := &sync.Map{}
	lastEvaluatedTwelveSecsAgo.Store(policy.UID, twelveSecsAgo)

	twelveHoursAgo := time.Now().UTC().Add(-12 * time.Hour).Format(time.RFC3339)
	lastEvaluatedTwelveHoursAgo := &sync.Map{}
	lastEvaluatedTwelveHoursAgo.Store(policy.UID, twelveHoursAgo)

	tests := []struct {
		testDescription          string
		lastEvaluated            string
		lastEvaluatedGeneration  int64
		evaluationInterval       policyv1.EvaluationInterval
		complianceState          policyv1.ComplianceState
		expected                 bool
		expectedPositiveDuration bool
		deletionTimestamp        *metav1.Time
		cleanupImmediately       bool
		finalizers               []string
		lastEvaluatedCache       *sync.Map
	}{
		{
			"Just evaluated and the generation is unchanged",
			inFuture,
			2,
			policyv1.EvaluationInterval{Compliant: "10s", NonCompliant: "10s"},
			policyv1.Compliant,
			false,
			true,
			nil,
			false,
			[]string{},
			lastEvaluatedInFuture,
		},
		{
			"The generation has changed",
			inFuture,
			1,
			policyv1.EvaluationInterval{Compliant: "10s", NonCompliant: "10s"},
			policyv1.Compliant,
			true,
			false,
			nil,
			false,
			[]string{},
			lastEvaluatedInFuture,
		},
		{
			"lastEvaluated not set",
			"",
			2,
			policyv1.EvaluationInterval{Compliant: "10s", NonCompliant: "10s"},
			policyv1.Compliant,
			true,
			false,
			nil,
			false,
			[]string{},
			&sync.Map{},
		},
		{
			"Invalid lastEvaluated",
			"Do or do not. There is no try.",
			2,
			policyv1.EvaluationInterval{Compliant: "10s", NonCompliant: "10s"},
			policyv1.Compliant,
			true,
			false,
			nil,
			false,
			[]string{},
			lastEvaluatedInvalid,
		},
		{
			"Unknown compliance state",
			inFuture,
			2,
			policyv1.EvaluationInterval{Compliant: "10s", NonCompliant: "10s"},
			policyv1.UnknownCompliancy,
			true,
			false,
			nil,
			false,
			[]string{},
			lastEvaluatedInFuture,
		},
		{
			"Default evaluation interval with a past lastEvaluated when compliant",
			twelveSecsAgo,
			2,
			policyv1.EvaluationInterval{Compliant: "10s", NonCompliant: "10s"},
			policyv1.Compliant,
			true,
			false,
			nil,
			false,
			[]string{},
			lastEvaluatedTwelveSecsAgo,
		},
		{
			"Default evaluation interval with a past lastEvaluated when noncompliant",
			twelveSecsAgo,
			2,
			policyv1.EvaluationInterval{Compliant: "10s", NonCompliant: "10s"},
			policyv1.NonCompliant,
			true,
			false,
			nil,
			false,
			[]string{},
			lastEvaluatedTwelveSecsAgo,
		},
		{
			"Never evaluation interval with past lastEvaluated when compliant",
			twelveHoursAgo,
			2,
			policyv1.EvaluationInterval{Compliant: "never"},
			policyv1.Compliant,
			false,
			false,
			nil,
			false,
			[]string{},
			lastEvaluatedTwelveHoursAgo,
		},
		{
			"Never evaluation interval with past lastEvaluated when noncompliant",
			twelveHoursAgo,
			2,
			policyv1.EvaluationInterval{Compliant: "10s", NonCompliant: "never"},
			policyv1.NonCompliant,
			false,
			false,
			nil,
			false,
			[]string{},
			lastEvaluatedTwelveHoursAgo,
		},
		{
			"Unset evaluation interval with past lastEvaluated when compliant",
			twelveHoursAgo,
			2,
			policyv1.EvaluationInterval{},
			policyv1.Compliant,
			true,
			false,
			nil,
			false,
			[]string{},
			lastEvaluatedTwelveHoursAgo,
		},
		{
			"Watch evaluation interval with past lastEvaluated when compliant",
			twelveHoursAgo,
			2,
			policyv1.EvaluationInterval{Compliant: "watch", NonCompliant: "watch"},
			policyv1.Compliant,
			true,
			false,
			nil,
			false,
			[]string{},
			lastEvaluatedTwelveHoursAgo,
		},
		{
			"Unset evaluation interval with past lastEvaluated when noncompliant",
			twelveHoursAgo,
			2,
			policyv1.EvaluationInterval{},
			policyv1.NonCompliant,
			true,
			false,
			nil,
			false,
			[]string{},
			lastEvaluatedTwelveHoursAgo,
		},
		{
			"Watch evaluation interval with past lastEvaluated when noncompliant",
			twelveHoursAgo,
			2,
			policyv1.EvaluationInterval{NonCompliant: "watch"},
			policyv1.NonCompliant,
			true,
			false,
			nil,
			false,
			[]string{},
			lastEvaluatedTwelveHoursAgo,
		},
		{
			"Invalid evaluation interval when compliant",
			inFuture,
			2,
			policyv1.EvaluationInterval{Compliant: "Do or do not. There is no try."},
			policyv1.Compliant,
			true,
			false,
			nil,
			false,
			[]string{},
			lastEvaluatedInFuture,
		},
		{
			"Invalid evaluation interval when noncompliant",
			inFuture,
			2,
			policyv1.EvaluationInterval{NonCompliant: "Do or do not. There is no try."},
			policyv1.NonCompliant,
			true,
			false,
			nil,
			false,
			[]string{},
			lastEvaluatedInFuture,
		},
		{
			"Custom evaluation interval that hasn't past yet when compliant",
			twelveSecsAgo,
			2,
			policyv1.EvaluationInterval{Compliant: "12h"},
			policyv1.Compliant,
			false,
			true,
			nil,
			false,
			[]string{},
			lastEvaluatedTwelveSecsAgo,
		},
		{
			"Custom evaluation interval that hasn't past yet when noncompliant",
			twelveSecsAgo,
			2,
			policyv1.EvaluationInterval{NonCompliant: "12h"},
			policyv1.NonCompliant,
			false,
			true,
			nil,
			false,
			[]string{},
			lastEvaluatedTwelveSecsAgo,
		},
		{
			"Deletion timestamp is non nil",
			inFuture,
			2,
			policyv1.EvaluationInterval{NonCompliant: "12h"},
			policyv1.NonCompliant,
			true,
			false,
			&metav1.Time{Time: time.Now()},
			false,
			[]string{},
			lastEvaluatedInFuture,
		},
		{
			"Finalizer and the controller is being deleted",
			inFuture,
			2,
			policyv1.EvaluationInterval{NonCompliant: "12h"},
			policyv1.NonCompliant,
			true,
			false,
			&metav1.Time{Time: time.Now()},
			true,
			[]string{pruneObjectFinalizer},
			lastEvaluatedInFuture,
		},
		{
			"No finalizer and the controller is being deleted",
			inFuture,
			2,
			policyv1.EvaluationInterval{NonCompliant: "12h"},
			policyv1.NonCompliant,
			false,
			false,
			&metav1.Time{Time: time.Now()},
			true,
			[]string{},
			lastEvaluatedInFuture,
		},
		{
			"controller-runtime cache not yet synced",
			twelveHoursAgo,
			2,
			policyv1.EvaluationInterval{NonCompliant: "12h"},
			policyv1.NonCompliant,
			false,
			true,
			nil,
			false,
			[]string{},
			lastEvaluatedTwelveSecsAgo,
		},
	}

	for _, test := range tests {
		test := test

		t.Run(
			test.testDescription,
			func(t *testing.T) {
				t.Parallel()
				policy := policy.DeepCopy()

				policy.Status.LastEvaluated = test.lastEvaluated
				policy.Status.LastEvaluatedGeneration = test.lastEvaluatedGeneration
				policy.Spec.EvaluationInterval = test.evaluationInterval
				policy.Status.ComplianceState = test.complianceState
				policy.ObjectMeta.DeletionTimestamp = test.deletionTimestamp
				policy.ObjectMeta.Finalizers = test.finalizers

				r := &ConfigurationPolicyReconciler{
					SelectorReconciler: &fakeSR{},
					lastEvaluatedCache: *test.lastEvaluatedCache, //nolint: govet
				}

				actual, actualDuration := r.shouldEvaluatePolicy(policy, test.cleanupImmediately)

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
		_, update, _, skip = handleSingleKey(key, unstruct, &unstructObj, "musthave", true)
		assert.Equal(t, update, test.expectResult.expect)
		assert.False(t, skip)
	}
}
