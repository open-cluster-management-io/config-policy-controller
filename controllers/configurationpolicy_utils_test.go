package controllers

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

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
	assert.Equal(t, policyTemplateFormatted["annotations"], "not-an-annotation")
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
		test := test
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
				assert.Equal(t, len(details), 1)

				detail := details[0]
				conditions := detail.Conditions
				assert.Equal(t, len(conditions), 1)

				condition := conditions[0]
				lowercaseCompliance := strings.ToLower(string(test.compliancy))
				expectedMsg := `Some message. This policy will not be evaluated again due to ` +
					fmt.Sprintf(`spec.evaluationInterval.%s being set to "never".`, lowercaseCompliance)

				assert.Equal(t, condition.Message, expectedMsg)
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

	assert.True(t, checkFieldsWithSort(mergedObj, oldObj))
}
