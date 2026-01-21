package controllers

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stolostron/go-template-utils/v7/pkg/templates"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/scheme"

	policyv1 "open-cluster-management.io/config-policy-controller/api/v1"
)

func TestFormatTemplateAnnotation(t *testing.T) {
	t.Parallel()

	policyTemplate := map[string]interface{}{
		"annotations": map[string]interface{}{
			"annotation1": "one!",
			"annotation2": "two!",
		},
		"labels": map[string]string{
			"label1": "yes",
			"label2": "no",
		},
	}

	policyTemplateFormatted := formatMetadata(policyTemplate)
	assert.Equal(t, policyTemplateFormatted["annotations"], policyTemplate["annotations"])
}

func TestFormatTemplateNullAnnotation(t *testing.T) {
	t.Parallel()

	policyTemplate := map[string]interface{}{
		"annotations": nil,
		"labels": map[string]string{
			"label1": "yes",
			"label2": "no",
		},
	}

	policyTemplateFormatted := formatMetadata(policyTemplate)
	assert.Nil(t, policyTemplateFormatted["annotations"])
}

func TestFormatTemplateStringAnnotation(t *testing.T) {
	t.Parallel()

	policyTemplate := map[string]interface{}{
		"annotations": "not-an-annotation",
		"labels": map[string]string{
			"label1": "yes",
			"label2": "no",
		},
	}

	policyTemplateFormatted := formatMetadata(policyTemplate)
	assert.Equal(t, "not-an-annotation", policyTemplateFormatted["annotations"])
}

func TestAddConditionToStatusNeverEvalInterval(t *testing.T) {
	t.Parallel()

	tests := []struct {
		compliancy         policyv1.ComplianceState
		evaluationInterval policyv1.EvaluationInterval
	}{
		{policyv1.Compliant, policyv1.EvaluationInterval{Compliant: "never"}},
		{policyv1.NonCompliant, policyv1.EvaluationInterval{NonCompliant: "never"}},
	}

	for _, test := range tests {
		t.Run(
			fmt.Sprintf("compliance=%s", test.compliancy),
			func(t *testing.T) {
				t.Parallel()

				policy := &policyv1.ConfigurationPolicy{
					Spec: policyv1.ConfigurationPolicySpec{
						EvaluationInterval: test.evaluationInterval,
					},
				}

				addConditionToStatus(policy, 0, test.compliancy == policyv1.Compliant, "Some reason", "Some message")

				details := policy.Status.CompliancyDetails
				assert.Len(t, details, 1)

				detail := details[0]
				conditions := detail.Conditions
				assert.Len(t, conditions, 1)

				condition := conditions[0]
				lowercaseCompliance := strings.ToLower(string(test.compliancy))
				expectedMsg := `Some message. This policy will not be evaluated again due to ` +
					fmt.Sprintf(`spec.evaluationInterval.%s being set to "never".`, lowercaseCompliance)

				assert.Equal(t, expectedMsg, condition.Message)
			},
		)
	}
}

func TestCheckFieldsAreEquivalentEmptyMap(t *testing.T) {
	oldObj := map[string]interface{}{
		"spec": map[string]interface{}{
			"storage": map[string]interface{}{
				"s3": map[string]interface{}{
					"bucket": "some-bucket",
				},
			},
		},
	}
	mergedObj := map[string]interface{}{
		"spec": map[string]interface{}{
			"storage": map[string]interface{}{
				"emptyDir": map[string]interface{}{},
			},
		},
	}

	check, _ := checkFieldsAreEquivalent(mergedObj, oldObj, false)
	assert.False(t, check)

	check, _ = checkFieldsAreEquivalent(mergedObj, oldObj, true)
	assert.True(t, check)
}

func TestCheckFieldsAreEquivalent(t *testing.T) {
	t.Parallel()

	oldObj := map[string]interface{}{
		"nonResourceURLs": []string{"/version", "/healthz"},
		"verbs":           []string{"get"},
	}
	mergedObj := map[string]interface{}{
		"nonResourceURLs": []string{"/version", "/healthz"},
		"verbs":           []string{"get"},
		"apiGroups":       []interface{}{},
		"resources":       []interface{}{},
	}

	check, _ := checkFieldsAreEquivalent(mergedObj, oldObj, false)
	assert.True(t, check)

	check, _ = checkFieldsAreEquivalent(mergedObj, oldObj, true)
	assert.True(t, check)

	check, _ = checkFieldsAreEquivalent(mergedObj, nil, true)
	assert.False(t, check)

	mergedObj = map[string]interface{}{
		"nonResourceURLs": []string{"/version", "/healthz"},
		"verbs":           []string{"post"},
		"apiGroups":       []interface{}{},
		"resources":       []interface{}{},
	}

	check, _ = checkFieldsAreEquivalent(mergedObj, oldObj, true)
	assert.False(t, check)
}

func TestDeeplyEquivalentString(t *testing.T) {
	t.Parallel()

	check, _ := deeplyEquivalent("", nil, true)
	assert.True(t, check)
	check, _ = deeplyEquivalent("", nil, false)
	assert.False(t, check)
	check, _ = deeplyEquivalent(nil, "", true)
	assert.True(t, check)
	check, _ = deeplyEquivalent(nil, "", false)
	assert.False(t, check)
}

func TestDeeplyEquivalentEmptyMap(t *testing.T) {
	t.Parallel()

	oldObj := map[string]interface{}{
		"cities": map[string]interface{}{},
	}
	mergedObj := map[string]interface{}{
		"cities": map[string]interface{}{
			"raleigh": map[string]interface{}{},
		},
	}

	check, _ := deeplyEquivalent(mergedObj, oldObj, true)
	assert.True(t, check)
	check, _ = deeplyEquivalent(mergedObj, oldObj, false)
	assert.False(t, check)
}

func TestGenerateDiff(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		existingObj  map[string]interface{}
		updatedObj   map[string]interface{}
		expectedDiff string
	}{
		"same object generates no diff": {
			existingObj: map[string]interface{}{
				"cities": map[string]interface{}{},
			},
			updatedObj: map[string]interface{}{
				"cities": map[string]interface{}{},
			},
		},
		"object with new child key": {
			existingObj: map[string]interface{}{
				"cities": map[string]interface{}{},
			},
			updatedObj: map[string]interface{}{
				"cities": map[string]interface{}{
					"raleigh": map[string]interface{}{},
				},
			},
			expectedDiff: `
@@ -1,2 +1,3 @@
-cities: {}
+cities:
+  raleigh: {}`,
		},
		"object with new key": {
			existingObj: map[string]interface{}{
				"cities": map[string]interface{}{},
			},
			updatedObj: map[string]interface{}{
				"cities": map[string]interface{}{},
				"states": map[string]interface{}{},
			},
			expectedDiff: `
@@ -1,2 +1,3 @@
 cities: {}
+states: {}`,
		},
		"array with added item": {
			existingObj: map[string]interface{}{
				"cities": []string{
					"Raleigh",
				},
			},
			updatedObj: map[string]interface{}{
				"cities": []string{
					"Raleigh",
					"Durham",
				},
			},
			expectedDiff: `
@@ -1,3 +1,4 @@
 cities:
 - Raleigh
+- Durham`,
		},
		"array with removed item": {
			existingObj: map[string]interface{}{
				"cities": []string{
					"Raleigh",
					"Durham",
				},
			},
			updatedObj: map[string]interface{}{
				"cities": []string{
					"Raleigh",
				},
			},
			expectedDiff: `
@@ -1,4 +1,3 @@
 cities:
 - Raleigh
-- Durham`,
		},
	}

	for testName, test := range tests {
		t.Run(testName, func(t *testing.T) {
			t.Parallel()

			existingObj := &unstructured.Unstructured{
				Object: test.existingObj,
			}
			updatedObj := &unstructured.Unstructured{
				Object: test.updatedObj,
			}

			diff, err := generateDiff(existingObj, updatedObj, false)
			if err != nil {
				t.Fatal(fmt.Errorf("Encountered unexpected error: %w", err))
			}

			// go-diff adds a trailing newline and whitespace, which gets
			// chomped when logging, so adding it here just for the test,
			// along with the common prefix
			if test.expectedDiff != "" {
				test.expectedDiff = "---  : existing\n+++  : updated" + test.expectedDiff + "\n \n"
			}

			assert.Equal(t, test.expectedDiff, diff)
		})
	}
}

func TestGetTemplateContext(t *testing.T) {
	t.Parallel()

	testObj := map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]any{
			"name": "test-configmap",
		},
	}

	tests := map[string]struct {
		obj      map[string]any
		name     string
		ns       string
		expected any
	}{
		"namespaced object with name and namespace": {
			obj:  testObj,
			name: "my-configmap",
			ns:   "default",
			expected: struct {
				Object          map[string]any
				ObjectNamespace string
				ObjectName      string
			}{
				Object:          testObj,
				ObjectNamespace: "default",
				ObjectName:      "my-configmap",
			},
		},
		"cluster-scoped object with name only": {
			obj:  testObj,
			name: "my-clusterrole",
			ns:   "",
			expected: struct {
				Object     map[string]any
				ObjectName string
			}{
				Object:     testObj,
				ObjectName: "my-clusterrole",
			},
		},
		"unnamed namespaced object with namespace only": {
			obj:  testObj,
			name: "",
			ns:   "kube-system",
			expected: struct {
				ObjectNamespace string
			}{
				ObjectNamespace: "kube-system",
			},
		},
		"no name and no namespace": {
			obj:      testObj,
			name:     "",
			ns:       "",
			expected: nil,
		},
		"empty object with name and namespace": {
			obj:  map[string]any{},
			name: "empty-obj",
			ns:   "test-ns",
			expected: struct {
				Object          map[string]any
				ObjectNamespace string
				ObjectName      string
			}{
				Object:          map[string]any{},
				ObjectNamespace: "test-ns",
				ObjectName:      "empty-obj",
			},
		},
		"nil object with name and namespace": {
			obj:  nil,
			name: "nil-obj",
			ns:   "test-ns",
			expected: struct {
				Object          map[string]any
				ObjectNamespace string
				ObjectName      string
			}{
				Object:          nil,
				ObjectNamespace: "test-ns",
				ObjectName:      "nil-obj",
			},
		},
	}

	for testName, test := range tests {
		t.Run(testName, func(t *testing.T) {
			t.Parallel()

			result := getTemplateContext(test.obj, test.name, test.ns)

			assert.Equal(t, test.expected, result)
		})
	}
}

func TestResolveGoTemplates(t *testing.T) {
	t.Parallel()

	// Create a fake dynamic client and template resolver for testing
	dynamicClient := fake.NewSimpleDynamicClient(scheme.Scheme)
	tmplResolver, err := templates.NewResolverWithClients(
		dynamicClient, nil, templates.Config{},
	)
	assert.NoError(t, err)

	tests := map[string]struct {
		rawObj           string
		templateContext  any
		expectSkipObject bool
		expectError      bool
		expectedResolved string
		errorContains    string
	}{
		"simple template without skipObject": {
			rawObj: `apiVersion: v1
kind: ConfigMap
metadata:
  name: test
  namespace: '{{ .ObjectNamespace }}'`,
			templateContext: struct {
				ObjectNamespace string
			}{
				ObjectNamespace: "test-namespace",
			},
			expectSkipObject: false,
			expectError:      false,
			expectedResolved: `{"apiVersion":"v1",` +
				`"kind":"ConfigMap",` +
				`"metadata":{` +
				`"name":"test",` +
				`"namespace":"test-namespace"}}`,
		},
		"template with skipObject no arguments": {
			rawObj: `apiVersion: v1
kind: ConfigMap
metadata:
  name: '{{ if true }}{{ skipObject }}{{ end }}test'`,
			templateContext:  nil,
			expectSkipObject: true,
			expectError:      false,
			expectedResolved: `{"apiVersion":"v1",` +
				`"kind":"ConfigMap",` +
				`"metadata":{"name":"test"}}`,
		},
		"template with skipObject true": {
			rawObj: `apiVersion: v1
kind: ConfigMap
metadata:
  name: '{{ skipObject true }}test'`,
			templateContext:  nil,
			expectSkipObject: true,
			expectError:      false,
			expectedResolved: `{"apiVersion":"v1",` +
				`"kind":"ConfigMap",` +
				`"metadata":{"name":"test"}}`,
		},
		"template with skipObject false": {
			rawObj: `apiVersion: v1
kind: ConfigMap
metadata:
  name: '{{ skipObject false }}test'`,
			templateContext:  nil,
			expectSkipObject: false,
			expectError:      false,
			expectedResolved: `{"apiVersion":"v1",` +
				`"kind":"ConfigMap",` +
				`"metadata":{"name":"test"}}`,
		},
		"template with skipObject invalid argument": {
			rawObj: `apiVersion: v1
kind: ConfigMap
metadata:
  name: '{{ skipObject "invalid" }}test'`,
			templateContext:  nil,
			expectSkipObject: false,
			expectError:      true,
			errorContains:    "expected boolean but received",
		},
		"template with skipObject multiple arguments": {
			rawObj: `apiVersion: v1
kind: ConfigMap
metadata:
  name: '{{ skipObject true false }}test'`,
			templateContext:  nil,
			expectSkipObject: false,
			expectError:      true,
			errorContains:    "expected one optional boolean argument but received 2 arguments",
		},
		"template without any templates": {
			rawObj: `apiVersion: v1
kind: ConfigMap
metadata:
  name: plain-configmap`,
			templateContext:  nil,
			expectSkipObject: false,
			expectError:      false,
			expectedResolved: `{"apiVersion":"v1",` +
				`"kind":"ConfigMap",` +
				`"metadata":{` +
				`"name":"plain-configmap"}}`,
		},
		"template with context variables": {
			rawObj: `apiVersion: v1
kind: Pod
metadata:
  name: '{{ .ObjectName }}'
  namespace: '{{ .ObjectNamespace }}'`,
			templateContext: struct {
				ObjectName      string
				ObjectNamespace string
			}{
				ObjectName:      "test-pod",
				ObjectNamespace: "test-namespace",
			},
			expectSkipObject: false,
			expectError:      false,
			expectedResolved: `{"apiVersion":"v1",` +
				`"kind":"Pod",` +
				`"metadata":{` +
				`"name":"test-pod",` +
				`"namespace":"test-namespace"}}`,
		},
	}

	for testName, test := range tests {
		t.Run(testName, func(t *testing.T) {
			t.Parallel()

			resolveOptions := &templates.ResolveOptions{}
			resolvedTemplate, skipObject, err := resolveGoTemplates(
				[]byte(test.rawObj),
				test.templateContext,
				tmplResolver,
				resolveOptions,
			)

			if test.expectError {
				assert.Error(t, err)

				if test.errorContains != "" {
					assert.Contains(t, err.Error(), test.errorContains)
				}
			} else {
				assert.NoError(t, err)
				assert.JSONEq(t, test.expectedResolved, string(resolvedTemplate.ResolvedJSON))
			}

			assert.Equal(t, test.expectSkipObject, skipObject)
		})
	}
}

func TestGetTemplateResolver_DenylistFunctions(t *testing.T) {
	tests := []struct {
		name             string
		denylist         []string
		expectedDenylist []string
	}{
		{
			name:             "no denylist",
			denylist:         []string{},
			expectedDenylist: []string{},
		},
		{
			name:             "single denylisted function",
			denylist:         []string{"add"},
			expectedDenylist: []string{"add"},
		},
		{
			name:             "multiple denylisted functions",
			denylist:         []string{"add1", "add"},
			expectedDenylist: []string{"add1", "add"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dynamicClient := fake.NewSimpleDynamicClient(scheme.Scheme)

			policy := &policyv1.ConfigurationPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-policy",
					Namespace: "default",
				},
				Spec: policyv1.ConfigurationPolicySpec{
					ObjectTemplates: []*policyv1.ObjectTemplate{
						{
							ComplianceType:   "musthave",
							ObjectDefinition: runtime.RawExtension{},
						},
					},
				},
			}

			r := &ConfigurationPolicyReconciler{
				TemplateFuncDenylist:   tt.denylist,
				TargetK8sDynamicClient: dynamicClient,
			}

			_, resolveOptions, err := r.getTemplateResolver(policy)

			assert.NoError(t, err)

			assert.Equal(t, tt.expectedDenylist, resolveOptions.DenylistFunctions)
		})
	}
}
