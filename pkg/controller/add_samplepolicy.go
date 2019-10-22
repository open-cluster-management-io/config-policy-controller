package controller

import (
	"github.ibm.com/IBMPrivateCloud/multicloud-operators-policy-controller/pkg/controller/samplepolicy"
)

func init() {
	// AddToManagerFuncs is a list of functions to create controllers and add them to a manager.
	AddToManagerFuncs = append(AddToManagerFuncs, samplepolicy.Add)
}
