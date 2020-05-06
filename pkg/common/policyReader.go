// Copyright 2019 The Kubernetes Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package common

import (
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
		instance, err := res.Get(name, metav1.GetOptions{})
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
		instance, err := res.Get(name, metav1.GetOptions{})
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
