// Copyright Contributors to the Open Cluster Management project

package main

import (
	"errors"
	"os"

	"open-cluster-management.io/config-policy-controller/pkg/dryrun"
)

func main() {
	err := dryrun.Execute()
	if err != nil {
		if errors.Is(err, dryrun.ErrNonCompliant) {
			os.Exit(2)
		}

		os.Exit(1)
	}
}
