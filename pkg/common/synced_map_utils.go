// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project


package common

import (
	"sync"

	policiesv1 "github.com/open-cluster-management/config-policy-controller/pkg/apis/policy/v1"
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
