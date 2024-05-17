// Copyright Contributors to the Open Cluster Management project

package v1

import (
	"fmt"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
)

func TestRecordDiffWithDefault(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		apiVersion string
		kind       string
		recordDiff RecordDiff
		expected   RecordDiff
	}{
		"Namespace-recordDiff-unset": {
			apiVersion: "v1",
			kind:       "Namespace",
			expected:   RecordDiffInStatus,
		},
		"Namespace-recordDiff-set": {
			apiVersion: "v1",
			kind:       "Namespace",
			recordDiff: RecordDiffLog,
			expected:   RecordDiffLog,
		},
		"Configmap-recordDiff-unset": {
			apiVersion: "v1",
			kind:       "ConfigMap",
			expected:   RecordDiffCensored,
		},
		"Secret-recordDiff-unset": {
			apiVersion: "v1",
			kind:       "Secret",
			expected:   RecordDiffCensored,
		},
		"Secret-recordDiff-set": {
			apiVersion: "v1",
			kind:       "Secret",
			recordDiff: RecordDiffInStatus,
			expected:   RecordDiffInStatus,
		},
		"OAuthAccessToken-recordDiff-unset": {
			apiVersion: "oauth.openshift.io/v1",
			kind:       "OAuthAccessToken",
			expected:   RecordDiffCensored,
		},
		"OAuthAuthorizeTokens-recordDiff-unset": {
			apiVersion: "oauth.openshift.io/v1",
			kind:       "OAuthAuthorizeTokens",
			expected:   RecordDiffCensored,
		},
		"Route-recordDiff-unset": {
			apiVersion: "route.openshift.io/v1",
			kind:       "Route",
			expected:   RecordDiffCensored,
		},
	}

	for testName, test := range tests {
		test := test

		t.Run(
			testName,
			func(t *testing.T) {
				t.Parallel()

				objTemp := ObjectTemplate{
					ObjectDefinition: runtime.RawExtension{
						Raw: []byte(fmt.Sprintf(`{"apiVersion": "%s", "kind": "%s"}`, test.apiVersion, test.kind)),
					},
					RecordDiff: test.recordDiff,
				}

				if objTemp.RecordDiffWithDefault() != test.expected {
					t.Fatalf("Expected %s but got %s", test.expected, objTemp.RecordDiffWithDefault())
				}
			},
		)
	}
}
