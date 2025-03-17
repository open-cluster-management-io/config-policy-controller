package controllers

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

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

func TestCheckFieldsWithSort(t *testing.T) {
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

	assert.True(t, checkFieldsWithSort(mergedObj, oldObj, false))
}

func TestCheckFieldsWithSortEmptyMap(t *testing.T) {
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

	assert.False(t, checkFieldsWithSort(mergedObj, oldObj, false))

	assert.True(t, checkFieldsWithSort(mergedObj, oldObj, true))
}

func TestEqualObjWithSort(t *testing.T) {
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

	assert.True(t, equalObjWithSort(mergedObj, oldObj, true))
	assert.False(t, equalObjWithSort(mergedObj, nil, true))

	oldObj = map[string]interface{}{
		"nonResourceURLs": []string{"/version", "/healthz"},
		"verbs":           []string{"get"},
	}
	mergedObj = map[string]interface{}{
		"nonResourceURLs": []string{"/version", "/healthz"},
		"verbs":           []string{"post"},
		"apiGroups":       []interface{}{},
		"resources":       []interface{}{},
	}

	assert.False(t, equalObjWithSort(mergedObj, oldObj, true))
}

func TestEqualObjWithSortString(t *testing.T) {
	t.Parallel()

	assert.True(t, equalObjWithSort("", nil, true))
	assert.False(t, equalObjWithSort("", nil, false))
	assert.True(t, equalObjWithSort(nil, "", true))
	assert.False(t, equalObjWithSort(nil, "", false))
}

func TestEqualObjWithSortEmptyMap(t *testing.T) {
	t.Parallel()

	oldObj := map[string]interface{}{
		"cities": map[string]interface{}{},
	}
	mergedObj := map[string]interface{}{
		"cities": map[string]interface{}{
			"raleigh": map[string]interface{}{},
		},
	}

	assert.True(t, equalObjWithSort(mergedObj, oldObj, true))
	assert.False(t, equalObjWithSort(mergedObj, oldObj, false))
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

			diff, err := generateDiff(existingObj, updatedObj)
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
