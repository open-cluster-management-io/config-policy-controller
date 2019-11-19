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
	"encoding/json"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/golang/glog"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/restmapper"
)

// GetGenericObject returns a generic object information from the k8s API server
func GetGenericObject(data []byte, namespace string) (unstructured.Unstructured, error) {
	var unstruct unstructured.Unstructured
	namespaced := true
	dd := (*KubeClient).Discovery()
	apigroups, err := restmapper.GetAPIGroupResources(dd)
	if err != nil {
		glog.Fatal(err)
	}

	restmapper := restmapper.NewDiscoveryRESTMapper(apigroups)

	glog.V(9).Infof("reading raw object: %v", string(data))
	versions := &runtime.VersionedObjects{}
	_, gvk, err := unstructured.UnstructuredJSONScheme.Decode(data, nil, versions)

	if err != nil {
		decodeErr := fmt.Sprintf("Decoding error, please check your policy file! error = `%v`", err)
		glog.Errorf(decodeErr)
		return unstruct, err
	}
	mapping, err := restmapper.RESTMapping(gvk.GroupKind(), gvk.Version)

	if err != nil {
		glog.Errorf("mapping error from raw object: `%v`", err)
		return unstruct, err
	}
	glog.V(9).Infof("mapping found from raw object: %v", mapping)

	restconfig := KubeConfig
	restconfig.GroupVersion = &schema.GroupVersion{
		Group:   mapping.GroupVersionKind.Group,
		Version: mapping.GroupVersionKind.Version,
	}
	dclient, err := dynamic.NewForConfig(restconfig)
	if err != nil {
		glog.Fatal(err)
	}

	_, apiresourcelist, err := dd.ServerGroupsAndResources()
	if err != nil {
		glog.Fatal(err)
	}

	rsrc := mapping.Resource
	for _, apiresourcegroup := range apiresourcelist {
		if apiresourcegroup.GroupVersion == joinStr(mapping.GroupVersionKind.Group, "/", mapping.GroupVersionKind.Version) {
			for _, apiresource := range apiresourcegroup.APIResources {
				if apiresource.Name == mapping.Resource.Resource && apiresource.Kind == mapping.GroupVersionKind.Kind {
					rsrc = mapping.Resource
					namespaced = apiresource.Namespaced
					glog.V(7).Infof("is raw object namespaced? %v", namespaced)
				}
			}
		}
	}

	unstruct.Object = make(map[string]interface{})
	var blob interface{}
	if err = json.Unmarshal(data, &blob); err != nil {
		glog.Fatal(err)
	}
	unstruct.Object = blob.(map[string]interface{}) //set object to the content of the blob after Unmarshalling

	name := ""
	if md, ok := unstruct.Object["metadata"]; ok {
		metadata := md.(map[string]interface{})
		if objectName, ok := metadata["name"]; ok {
			name = objectName.(string)
		}
	}

	instance, err := getTheObject(namespaced, namespace, name, rsrc, dclient)
	if err != nil {
		fmt.Println(err)
	}
	return *instance, err
}

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
