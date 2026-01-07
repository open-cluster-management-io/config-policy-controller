// Copyright (c) 2025 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package common

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	policyv1 "open-cluster-management.io/config-policy-controller/api/v1"
)

func TestFilter(t *testing.T) {
	t.Parallel()

	all := []string{
		"alfalfa", "barley", "cheese", "chicken", "default", "egg", "eggplant",
		"empty", "filet-mignon", "gourd", "ham", "iceberg-lettuce",
	}

	tests := map[string]struct {
		target       policyv1.Target
		controllerTI string
		expected     []string
	}{
		"A simple wildcard should include everything": {
			policyv1.Target{
				Include:              []policyv1.NonEmptyString{"*"},
				Exclude:              []policyv1.NonEmptyString{""},
				TerminatingInclusion: "Default",
			},
			"IfMatch",
			all,
		},
		"An empty target should select nothing": {
			policyv1.Target{
				Include:              []policyv1.NonEmptyString{""},
				Exclude:              []policyv1.NonEmptyString{""},
				TerminatingInclusion: "Default",
			},
			"IfMatch",
			[]string{},
		},
		"Multiple include wildcards should work": {
			policyv1.Target{
				Include:              []policyv1.NonEmptyString{"egg*", "*lettuce"},
				Exclude:              []policyv1.NonEmptyString{""},
				TerminatingInclusion: "Default",
			},
			"IfMatch",
			[]string{"egg", "eggplant", "iceberg-lettuce"},
		},
		"Multiple exclude wildcards should work": {
			policyv1.Target{
				Include:              []policyv1.NonEmptyString{"*"},
				Exclude:              []policyv1.NonEmptyString{"*a*", "ch*"},
				TerminatingInclusion: "Default",
			},
			"IfMatch",
			[]string{"egg", "empty", "filet-mignon", "gourd", "iceberg-lettuce"},
		},
		"Matching a label should work": {
			policyv1.Target{
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"food": "meat"},
				},
				Include:              []policyv1.NonEmptyString{"*"},
				Exclude:              []policyv1.NonEmptyString{""},
				TerminatingInclusion: "Default",
			},
			"IfMatch",
			[]string{"chicken", "filet-mignon", "ham"},
		},
		"Matching a NotIn label expression should work": {
			policyv1.Target{
				LabelSelector: &metav1.LabelSelector{
					MatchExpressions: []metav1.LabelSelectorRequirement{{
						Key:      "food",
						Operator: "NotIn",
						Values:   []string{"vegetable"},
					}},
				},
				Include:              []policyv1.NonEmptyString{"*"},
				Exclude:              []policyv1.NonEmptyString{""},
				TerminatingInclusion: "Default",
			},
			"IfMatch",
			[]string{"cheese", "chicken", "default", "egg", "empty", "filet-mignon", "ham"},
		},
		"The controller setting 'Drop' should be able to exclude terminating namespaces": {
			policyv1.Target{
				Include:              []policyv1.NonEmptyString{"*"},
				Exclude:              []policyv1.NonEmptyString{""},
				TerminatingInclusion: "Default",
			},
			"Drop",
			[]string{"barley", "cheese", "chicken", "default", "filet-mignon", "gourd", "ham"},
		},
		"The target setting should override when the controller default is 'Drop'": {
			policyv1.Target{
				Include:              []policyv1.NonEmptyString{"*"},
				Exclude:              []policyv1.NonEmptyString{""},
				TerminatingInclusion: "IfMatch",
			},
			"Drop",
			all,
		},
		"The target setting should override when the controller default is 'IfMatch'": {
			policyv1.Target{
				Include:              []policyv1.NonEmptyString{"*"},
				Exclude:              []policyv1.NonEmptyString{""},
				TerminatingInclusion: "Never",
			},
			"IfMatch",
			[]string{"barley", "cheese", "chicken", "default", "filet-mignon", "gourd", "ham"},
		},
	}

	for description, test := range tests {
		t.Run(
			description,
			func(t *testing.T) {
				t.Parallel()

				actual, err := filter(sampleNamespaceList(), test.target, test.controllerTI)
				if err != nil {
					t.Fatalf("Unexpected error occurred: %v", err)
				}

				assert.Equal(t, test.expected, actual)
			},
		)
	}
}

// Returns a pared-down NamespaceList with much of the metadata missing, but should contain enough
// information for some tests. Contains these namespaces:
//   - alfalfa:         food:vegetable, terminating
//   - barley:          food:vegetable
//   - cheese:          food:dairy
//   - chicken:         food:meat
//   - default:         (no labels)
//   - egg:             food:dairy,     terminating
//   - eggplant:        food:vegetable, terminating
//   - empty:           (no labels)     terminating
//   - filet-mignon:    food:meat
//   - gourd:           food:vegetable
//   - ham:             food:meat
//   - iceberg-lettuce: food:vegetable, terminating
func sampleNamespaceList() corev1.NamespaceList {
	past := metav1.Time{
		Time: time.Now().Add(-time.Minute),
	}

	return corev1.NamespaceList{Items: []corev1.Namespace{{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "alfalfa",
			DeletionTimestamp: &past,
			Labels: map[string]string{
				"food": "vegetable",
			},
		},
	}, {
		ObjectMeta: metav1.ObjectMeta{
			Name: "barley",
			Labels: map[string]string{
				"food": "vegetable",
			},
		},
	}, {
		ObjectMeta: metav1.ObjectMeta{
			Name: "cheese",
			Labels: map[string]string{
				"food": "dairy",
			},
		},
	}, {
		ObjectMeta: metav1.ObjectMeta{
			Name: "chicken",
			Labels: map[string]string{
				"food": "meat",
			},
		},
	}, {
		ObjectMeta: metav1.ObjectMeta{
			Name:   "default",
			Labels: map[string]string{},
		},
	}, {
		ObjectMeta: metav1.ObjectMeta{
			Name:              "egg",
			DeletionTimestamp: &past,
			Labels: map[string]string{
				"food": "dairy",
			},
		},
	}, {
		ObjectMeta: metav1.ObjectMeta{
			Name:              "eggplant",
			DeletionTimestamp: &past,
			Labels: map[string]string{
				"food": "vegetable",
			},
		},
	}, {
		ObjectMeta: metav1.ObjectMeta{
			Name:              "empty",
			DeletionTimestamp: &past,
		},
	}, {
		ObjectMeta: metav1.ObjectMeta{
			Name: "filet-mignon",
			Labels: map[string]string{
				"food": "meat",
			},
		},
	}, {
		ObjectMeta: metav1.ObjectMeta{
			Name: "gourd",
			Labels: map[string]string{
				"food": "vegetable",
			},
		},
	}, {
		ObjectMeta: metav1.ObjectMeta{
			Name: "ham",
			Labels: map[string]string{
				"food": "meat",
			},
		},
	}, {
		ObjectMeta: metav1.ObjectMeta{
			Name:              "iceberg-lettuce",
			DeletionTimestamp: &past,
			Labels: map[string]string{
				"food": "vegetable",
			},
		},
	}}}
}
