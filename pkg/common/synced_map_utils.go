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
	"sync"

	policiesv1 "github.com/open-cluster-management/config-policy-controller/pkg/apis/policies/v1"
)

//SyncedPolicyMap a thread safe map
type SyncedPolicyMap struct {
	PolicyMap map[string]*policiesv1.ConfigurationPolicy
	//Mx for making the map thread safe
	Mx sync.RWMutex
}

//GetObject used for fetching objects from the synced map
func (spm *SyncedPolicyMap) GetObject(key string) (value *policiesv1.ConfigurationPolicy, found bool) {
	spm.Mx.Lock()
	defer spm.Mx.Unlock()
	//check if the map is initialized, if not initilize it
	if spm.PolicyMap == nil {
		return nil, false
	}
	if val, ok := spm.PolicyMap[key]; ok {
		return val, true
	}
	return nil, false
}

// AddObject safely add to map
func (spm *SyncedPolicyMap) AddObject(key string, plc *policiesv1.ConfigurationPolicy) {
	spm.Mx.Lock()
	defer spm.Mx.Unlock()
	//check if the map is initialized, if not initilize it
	if spm.PolicyMap == nil {
		spm.PolicyMap = make(map[string]*policiesv1.ConfigurationPolicy)
	}
	spm.PolicyMap[key] = plc
}

// RemoveObject safely remove from map
func (spm *SyncedPolicyMap) RemoveObject(key string) {
	spm.Mx.Lock()
	defer spm.Mx.Unlock()
	//check if the map is initialized, if not return
	if spm.PolicyMap == nil {
		return
	}
	delete(spm.PolicyMap, key)
}
