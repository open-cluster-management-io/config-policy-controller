// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package common

import (
	"testing"

	"github.com/stretchr/testify/assert"

	policyv1 "open-cluster-management.io/config-policy-controller/api/v1"
)

func TestMatches(t *testing.T) {
	t.Parallel()

	list := []string{"Hello-World", "World-Hello", "Hello-World-Hello", "nothing", "exact"}

	tests := []struct {
		testDescription string
		include         []policyv1.NonEmptyString
		exclude         []policyv1.NonEmptyString
		expected        []string
		errMsg          string
	}{
		{
			"Filter for prefix",
			[]policyv1.NonEmptyString{"Hello*"},
			[]policyv1.NonEmptyString{},
			[]string{"Hello-World", "Hello-World-Hello"},
			"",
		},
		{
			"Filter for suffix",
			[]policyv1.NonEmptyString{"*Hello"},
			[]policyv1.NonEmptyString{},
			[]string{"World-Hello", "Hello-World-Hello"},
			"",
		},
		{
			"Filter for containing string",
			[]policyv1.NonEmptyString{"*Hello*"},
			[]policyv1.NonEmptyString{},
			[]string{"Hello-World", "World-Hello", "Hello-World-Hello"},
			"",
		},
		{
			"Filter for nonexistent containing string",
			[]policyv1.NonEmptyString{"*xxx*"},
			[]policyv1.NonEmptyString{},
			[]string{},
			"",
		},
		{
			"Filter for exact string",
			[]policyv1.NonEmptyString{"Hello-World"},
			[]policyv1.NonEmptyString{},
			[]string{"Hello-World"},
			"",
		},
		{
			"Filter for internal wildcards",
			[]policyv1.NonEmptyString{"*o*rld*"},
			[]policyv1.NonEmptyString{},
			[]string{"Hello-World", "World-Hello", "Hello-World-Hello"},
			"",
		},
		{
			"Malformed filter",
			[]policyv1.NonEmptyString{"*[*"},
			[]policyv1.NonEmptyString{},
			[]string{},
			"error parsing 'include' pattern '*[*': syntax error in pattern",
		},
	}

	for _, test := range tests {
		test := test

		t.Run(
			test.testDescription,
			func(t *testing.T) {
				t.Parallel()

				actual, err := Matches(list, test.include, test.exclude)
				if err != nil {
					if test.errMsg == "" {
						t.Fatalf("Encountered unexpected error: %v", err)
					} else {
						assert.EqualError(t, err, test.errMsg)
					}
				}

				assert.Equal(t, actual, test.expected)
			},
		)
	}
}
