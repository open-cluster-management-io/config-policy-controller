// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package templates

import (
	"context"
	"github.com/golang/glog"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// retrieve Spec value for the given clusterclaim
func fromClusterClaim(claimname string) (string, error) {
	result := map[string]interface{}{}

	dclient, dclientErr := getDynamicClient(
		"cluster.open-cluster-management.io/v1alpha1",
		"ClusterClaim",
		"",
	)
	if dclientErr != nil {
		return "", dclientErr
	}

	getObj, getErr := dclient.Get(context.TODO(), claimname, metav1.GetOptions{})
	if getErr != nil {
		glog.Errorf("Error retrieving clusterclaim : %v, %v", claimname, getErr)
		return "", getErr
	}

	result = getObj.UnstructuredContent()

	spec := result["spec"].(map[string]interface{})
	if _, ok := spec["value"]; ok {
		return spec["value"].(string), nil
	}
	return "", nil
}
