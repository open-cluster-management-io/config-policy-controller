// Copyright (c) 2020 Red Hat, Inc.

package controller

import (
	configurationpolicy "github.com/open-cluster-management/config-policy-controller/pkg/controller/configurationpolicy"
)

func init() {
	// AddToManagerFuncs is a list of functions to create controllers and add them to a manager.
	AddToManagerFuncs = append(AddToManagerFuncs, configurationpolicy.Add)
}
