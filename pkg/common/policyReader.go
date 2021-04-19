// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package common

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/golang/glog"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

func getTheObject(namespaced bool, namespace string, name string,
	rsrc schema.GroupVersionResource, dclient dynamic.Interface) (*unstructured.Unstructured, error) {
	if !namespaced {
		res := dclient.Resource(rsrc)
		instance, err := res.Get(context.TODO(), name, metav1.GetOptions{})
		if err != nil {
			if errors.IsNotFound(err) {
				glog.V(6).Infof("response to retrieve a non namespaced object `%v` from the api-server: %v", name, err)
				return nil, nil
			}
			glog.Errorf("object `%v` cannot be retrieved from the api server\n", name)
		} else {
			glog.V(6).Infof("object `%v` retrieved from the api server\n", name)
			return instance, err
		}
	} else {
		res := dclient.Resource(rsrc).Namespace(namespace)
		instance, err := res.Get(context.TODO(), name, metav1.GetOptions{})
		if err != nil {
			if errors.IsNotFound(err) {
				glog.V(6).Infof("response to retrieve a namespaced object `%v` from the api-server: %v", name, err)
				return instance, err
			}
			glog.Errorf("object `%v` cannot be retrieved from the api server\n", name)
		} else {
			glog.V(3).Infof("object `%v` retrieved from the api server\n", name)
			return instance, err
		}
	}
	return nil, nil
}

func joinStr(strs ...string) string {
	var result string
	if strs[0] == "" {
		return strs[len(strs)-1]
	}
	for _, str := range strs {
		result += str
	}
	return result
}
