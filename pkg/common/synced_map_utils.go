// Licensed Materials - Property of IBM
// (c) Copyright IBM Corporation 2018. All Rights Reserved.
// Note to U.S. Government Users Restricted Rights:
// Use, duplication or disclosure restricted by GSA ADP Schedule
// Contract with IBM Corp.

package common

import (
	"sync"
	policiesv1alpha1 "github.ibm.com/IBMPrivateCloud/multicloud-operators-policy-controller/pkg/apis/policies/v1alpha1"
)

//SyncedPolicyMap a thread safe map
type SyncedPolicyMap struct {
	PolicyMap map[string]*policiesv1alpha1.SamplePolicy
	//Mx for making the map thread safe
	Mx sync.RWMutex
}

//GetObject used for fetching objects from the synced map
func (spm *SyncedPolicyMap) GetObject(key string) (value *policiesv1alpha1.SamplePolicy, found bool) {

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
func (spm *SyncedPolicyMap) AddObject(key string, plc *policiesv1alpha1.SamplePolicy) {

	spm.Mx.Lock()
	defer spm.Mx.Unlock()
	//check if the map is initialized, if not initilize it
	if spm.PolicyMap == nil {
		spm.PolicyMap = make(map[string]*policiesv1alpha1.SamplePolicy)
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
	if _, ok := spm.PolicyMap[key]; ok {
		delete(spm.PolicyMap, key)
	}
}
