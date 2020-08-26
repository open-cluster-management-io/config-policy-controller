// Copyright (c) 2020 Red Hat, Inc.

package configurationpolicy

import (
	policyv1 "github.com/open-cluster-management/config-policy-controller/pkg/apis/policy/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// addRelatedObjects builds the list of kubernetes resources related to the policy.  The list contains
// details on whether the object is compliant or not compliant with the policy.  The results are updated in the
// policy's Status information.
func addRelatedObjects(policy *policyv1.ConfigurationPolicy, compliant bool, rsrc schema.GroupVersionResource,
	namespace string, namespaced bool, objNames []string,
	nameLinkMap map[string]string, reason string) (relatedObjects []policyv1.RelatedObject) {

	for _, name := range objNames {
		// Initialize the related object from the object handling
		var relatedObject policyv1.RelatedObject
		if compliant {
			relatedObject.Compliant = string(policyv1.Compliant)
		} else {
			relatedObject.Compliant = string(policyv1.NonCompliant)
		}

		relatedObject.Reason = reason

		var metadata policyv1.ObjectMetadata
		metadata.Name = name
		if namespaced {
			metadata.Namespace = namespace
		} else {
			metadata.Namespace = ""
		}
		selfLink, ok := nameLinkMap[name]
		if ok {
			metadata.SelfLink = selfLink
		} else {
			metadata.SelfLink = ""
		}
		relatedObject.Object.APIVersion = rsrc.GroupVersion().String()
		relatedObject.Object.Kind = rsrc.Resource
		relatedObject.Object.Metadata = metadata
		relatedObjects = updateRelatedObjectsStatus(relatedObjects, relatedObject)
	}
	return relatedObjects
}

// updateRelatedObjectsStatus adds or updates the RelatedObject in the policy status.
func updateRelatedObjectsStatus(list []policyv1.RelatedObject,
	relatedObject policyv1.RelatedObject) (result []policyv1.RelatedObject) {
	present := false
	for index, currentObject := range list {
		if currentObject.Object.APIVersion ==
			relatedObject.Object.APIVersion && currentObject.Object.Kind == relatedObject.Object.Kind {
			if currentObject.Object.Metadata.Name ==
				relatedObject.Object.Metadata.Name && currentObject.Object.Metadata.Namespace ==
				relatedObject.Object.Metadata.Namespace {
				present = true
				if currentObject.Compliant != relatedObject.Compliant {
					list[index] = relatedObject
				}
			}
		}
	}
	if !present {
		list = append(list, relatedObject)
	}
	return list
}
