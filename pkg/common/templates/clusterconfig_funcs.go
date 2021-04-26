// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package templates

import (
	"context"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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

	var lookupErr error
	getObj, getErr := dclient.Get(context.TODO(), claimname, metav1.GetOptions{})
	if getErr == nil {
		result = getObj.UnstructuredContent()
	}
	lookupErr = getErr

	if lookupErr != nil {
		if apierrors.IsNotFound(lookupErr) {
			return "", nil
		}
	}

	spec := result["spec"].(map[string]interface{})
	if _, ok := spec["value"]; ok {
		return spec["value"].(string), lookupErr
	}
	return "", lookupErr
}
