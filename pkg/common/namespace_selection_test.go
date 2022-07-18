// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package common

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	testclient "k8s.io/client-go/kubernetes/fake"

	policyv1 "open-cluster-management.io/config-policy-controller/api/v1"
)

func TestGetSelectedNamespaces(t *testing.T) {
	// Initialize controller client to return namespaces for test
	simpleClient := testclient.NewSimpleClientset()

	// Initialize set of namespaces for the test
	for i := 1; i <= 5; i++ {
		idx := fmt.Sprint(i)

		namespace := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test" + idx,
				Labels: map[string]string{
					"label" + idx: "works",
					"all":         "namespaces",
					"thisns":      "is_" + idx,
				},
			},
		}

		_, err := simpleClient.CoreV1().Namespaces().Create(context.TODO(), namespace, metav1.CreateOptions{})
		if err != nil {
			t.Fatalf("Encountered unexpected error: %v", err)
		}
	}

	// Set of testcases to run
	tests := map[string]struct {
		selector policyv1.Target
		expected []string
		errMsg   string
	}{
		"Empty selector": {policyv1.Target{}, []string{"test1", "test2", "test3", "test4", "test5"}, ""},
		// Logic to return no namespaces here is left to the calling function because we need
		// `include` to still operate when a `LabelSelector` is not given
		"MatchLabel and MatchExpression set to nil": {
			policyv1.Target{MatchLabels: nil, MatchExpressions: nil},
			[]string{"test1", "test2", "test3", "test4", "test5"},
			"",
		},
		"Include with empty string": {
			policyv1.Target{Include: []policyv1.NonEmptyString{""}},
			[]string{},
			"",
		},
		"Include with exact string": {
			policyv1.Target{Include: []policyv1.NonEmptyString{"test1"}},
			[]string{"test1"},
			"",
		},
		"Include with * wildcard": {
			policyv1.Target{Include: []policyv1.NonEmptyString{"*"}},
			[]string{"test1", "test2", "test3", "test4", "test5"},
			"",
		},
		"Include with ? wildcard": {
			policyv1.Target{Include: []policyv1.NonEmptyString{"t?st?"}},
			[]string{"test1", "test2", "test3", "test4", "test5"},
			"",
		},
		"Include with [] character class": {
			policyv1.Target{Include: []policyv1.NonEmptyString{"test[3-5]"}},
			[]string{"test3", "test4", "test5"},
			"",
		},
		"Include with [^] character class": {
			policyv1.Target{Include: []policyv1.NonEmptyString{"test[^3-5]"}},
			[]string{"test1", "test2"},
			"",
		},
		"Exclude with exact string": {
			policyv1.Target{Exclude: []policyv1.NonEmptyString{"test1"}},
			[]string{"test2", "test3", "test4", "test5"},
			"",
		},
		"Exclude with * wildcard": {
			policyv1.Target{Exclude: []policyv1.NonEmptyString{"*"}},
			[]string{},
			"",
		},
		"Exclude with ? wildcard": {
			policyv1.Target{Exclude: []policyv1.NonEmptyString{"t?st?"}},
			[]string{},
			"",
		},
		"Exclude with [] character class": {
			policyv1.Target{Exclude: []policyv1.NonEmptyString{"test[3-5]"}},
			[]string{"test1", "test2"},
			"",
		},
		"Exclude with [^] character class": {
			policyv1.Target{Exclude: []policyv1.NonEmptyString{"test[^3-5]"}},
			[]string{"test3", "test4", "test5"},
			"",
		},
		"Include and Exclude together": {
			policyv1.Target{
				Include: []policyv1.NonEmptyString{"test[1-3]"},
				Exclude: []policyv1.NonEmptyString{"test[3-5]"},
			},
			[]string{"test1", "test2"},
			"",
		},
		"Include and Exclude with MatchLabels": {
			policyv1.Target{
				Include: []policyv1.NonEmptyString{"test[1-3]"},
				Exclude: []policyv1.NonEmptyString{"test[3-5]"},
				MatchLabels: &map[string]string{
					"label2": "works",
				},
			},
			[]string{"test2"},
			"",
		},
		"Exclude with MatchExpressions": {
			policyv1.Target{
				Exclude: []policyv1.NonEmptyString{"test[4-5]"},
				MatchExpressions: &[]metav1.LabelSelectorRequirement{{
					Key:      "thisns",
					Operator: "NotIn",
					Values:   []string{"is_2"},
				}},
			},
			[]string{"test1", "test3"},
			"",
		},
		"Invalid MatchExpressions": {
			policyv1.Target{
				MatchExpressions: &[]metav1.LabelSelectorRequirement{
					{
						Key:      "thisns",
						Operator: "In",
					},
				},
			},
			[]string{},
			"error parsing namespace LabelSelector: " +
				"values: Invalid value: []string(nil): for 'in', 'notin' operators, values set can't be empty",
		},
		"Invalid Include": {
			policyv1.Target{
				Include: []policyv1.NonEmptyString{"test["},
			},
			[]string{},
			"error parsing 'include' pattern 'test[': syntax error in pattern",
		},
		"Invalid Exclude": {
			policyv1.Target{
				Exclude: []policyv1.NonEmptyString{"test["},
			},
			[]string{},
			"error parsing 'exclude' pattern 'test[': syntax error in pattern",
		},
	}

	for name, test := range tests {
		name := name
		test := test

		t.Run(
			name,
			func(t *testing.T) {
				actual, err := GetSelectedNamespaces(simpleClient, test.selector)
				if err != nil {
					if test.errMsg == "" {
						t.Fatalf("Encountered unexpected error: %v", err)
					} else {
						assert.EqualError(t, err, test.errMsg)
					}
				}

				assert.Equal(t, test.expected, actual)
			},
		)
	}
}

func TestGetAllNamespaces(t *testing.T) {
	// Initialize controller client to return namespaces for test
	simpleClient := testclient.NewSimpleClientset()

	// Initialize set of namespaces for the test
	for i := 1; i <= 5; i++ {
		idx := fmt.Sprint(i)

		namespace := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test" + idx,
				Labels: map[string]string{
					"label" + idx: "works",
					"all":         "namespaces",
					"thisns":      "is_" + idx,
				},
			},
		}

		_, err := simpleClient.CoreV1().Namespaces().Create(context.TODO(), namespace, metav1.CreateOptions{})
		if err != nil {
			t.Fatalf("Encountered unexpected error: %v", err)
		}
	}

	// Set of testcases to run
	tests := map[string]struct {
		labelSelector metav1.LabelSelector
		expected      []string
		errMsg        string
	}{
		"Empty label selector": {metav1.LabelSelector{}, []string{"test1", "test2", "test3", "test4", "test5"}, ""},
		"MatchLabels for label that exists on all namespaces": {
			metav1.LabelSelector{MatchLabels: map[string]string{
				"all": "namespaces",
			}},
			[]string{"test1", "test2", "test3", "test4", "test5"},
			"",
		},
		"MatchLabels for label that exists on one namespace": {
			metav1.LabelSelector{MatchLabels: map[string]string{
				"label3": "works",
			}},
			[]string{"test3"},
			"",
		},
		"MatchLabels array with match": {
			metav1.LabelSelector{MatchLabels: map[string]string{
				"label3": "works",
				"all":    "namespaces",
			}},
			[]string{"test3"},
			"",
		},
		"MatchLabels array with no match": {
			metav1.LabelSelector{MatchLabels: map[string]string{
				"label3": "works",
				"label4": "works",
			}},
			nil,
			"",
		},
		"MatchExpressions for label key that exists on all namespaces": {
			metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{
				{
					Key:      "thisns",
					Operator: "Exists",
				},
			}},
			[]string{"test1", "test2", "test3", "test4", "test5"},
			"",
		},
		"MatchExpressions for label value that exists on all namespaces": {
			metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{
				{
					Key:      "all",
					Operator: "In",
					Values:   []string{"namespaces"},
				},
			}},
			[]string{"test1", "test2", "test3", "test4", "test5"},
			"",
		},
		"MatchExpressions filtering by values": {
			metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{
				{
					Key:      "thisns",
					Operator: "Exists",
				}, {
					Key:      "thisns",
					Operator: "NotIn",
					Values:   []string{"is_2", "is_5"},
				},
			}},
			[]string{"test1", "test3", "test4"},
			"",
		},
		"MatchExpressions not valid": {
			metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{
				{
					Key:      "thisns",
					Operator: "In",
				},
			}},
			nil,
			"error parsing namespace LabelSelector: " +
				"values: Invalid value: []string(nil): for 'in', 'notin' operators, values set can't be empty",
		},
		"MatchExpressions and MatchLabels": {
			metav1.LabelSelector{
				MatchLabels: map[string]string{
					"label4": "works",
				},
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{
						Key:      "thisns",
						Operator: "Exists",
					}, {
						Key:      "thisns",
						Operator: "NotIn",
						Values:   []string{"is_2", "is_5"},
					},
				},
			},
			[]string{"test4"},
			"",
		},
	}

	for name, test := range tests {
		name := name
		test := test

		t.Run(
			name,
			func(t *testing.T) {
				actual, err := GetAllNamespaces(simpleClient, test.labelSelector)
				if err != nil {
					if test.errMsg == "" {
						t.Fatalf("Encountered unexpected error: %v", err)
					} else {
						assert.EqualError(t, err, test.errMsg)
					}
				}

				assert.Equal(t, test.expected, actual)
			},
		)
	}
}
