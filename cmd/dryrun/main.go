// Copyright Contributors to the Open Cluster Management project

package main

import (
	"os"

	"open-cluster-management.io/config-policy-controller/pkg/dryrun"
)

func main() {
	err := dryrun.Execute()
	if err != nil {
		os.Exit(1)
	}
}
