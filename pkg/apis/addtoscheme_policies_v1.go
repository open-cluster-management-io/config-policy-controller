// Copyright (c) 2020 Red Hat, Inc.

package apis

import (
	v1 "github.com/open-cluster-management/config-policy-controller/pkg/apis/policy/v1"
)

func init() {
	// Register the types with the Scheme so the components can map objects to GroupVersionKinds and back
	AddToSchemes = append(AddToSchemes, v1.SchemeBuilder.AddToScheme)
}
