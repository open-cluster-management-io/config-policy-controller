// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package common

import (
	"fmt"
	"path/filepath"

	policyv1 "open-cluster-management.io/config-policy-controller/api/v1"
)

// Matches filters a slice of strings, and returns ones that match the selector
func Matches(
	namespaces []string,
	includeList []policyv1.NonEmptyString,
	excludeList []policyv1.NonEmptyString,
) ([]string, error) {
	matchingNamespaces := make([]string, 0)

	for _, namespace := range namespaces {
		// If the includeList is empty include all namespaces for the NamespaceSelector LabelSelector
		include := len(includeList) == 0

		for _, includePattern := range includeList {
			var err error

			include, err = filepath.Match(string(includePattern), namespace)
			if err != nil { // The only possible returned error is ErrBadPattern, when pattern is malformed.
				return matchingNamespaces, fmt.Errorf(
					"error parsing 'include' pattern '%s': %w", string(includePattern), err)
			}

			if include {
				break
			}
		}

		if !include {
			continue
		}

		exclude := false

		for _, excludePattern := range excludeList {
			var err error

			exclude, err = filepath.Match(string(excludePattern), namespace)
			if err != nil { // The only possible returned error is ErrBadPattern, when pattern is malformed.
				return matchingNamespaces, fmt.Errorf(
					"error parsing 'exclude' pattern '%s': %w", string(excludePattern), err)
			}

			if exclude {
				break
			}
		}

		if exclude {
			continue
		}

		matchingNamespaces = append(matchingNamespaces, namespace)
	}

	return matchingNamespaces, nil
}
