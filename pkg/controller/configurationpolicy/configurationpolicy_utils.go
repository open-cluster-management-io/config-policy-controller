// Copyright (c) 2021 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package configurationpolicy

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	policyv1 "github.com/open-cluster-management/config-policy-controller/pkg/apis/policy/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// addRelatedObjects builds the list of kubernetes resources related to the policy.  The list contains
// details on whether the object is compliant or not compliant with the policy.  The results are updated in the
// policy's Status information.
func addRelatedObjects(policy *policyv1.ConfigurationPolicy, compliant bool, rsrc schema.GroupVersionResource,
	namespace string, namespaced bool, objNames []string, reason string) (relatedObjects []policyv1.RelatedObject) {

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

//equalObjWithSort is a wrapper function that calls the correct function to check equality depending on what
//type the objects to compare are
func equalObjWithSort(mergedObj interface{}, oldObj interface{}) (areEqual bool) {
	switch mergedObj := mergedObj.(type) {
	case (map[string]interface{}):
		if oldObj == nil || !checkFieldsWithSort(mergedObj, oldObj.(map[string]interface{})) {
			return false
		}
	case ([]interface{}):
		if oldObj == nil || !checkListsMatch(mergedObj, oldObj.([]interface{})) {
			return false
		}
	default:
		if !reflect.DeepEqual(fmt.Sprint(mergedObj), fmt.Sprint(oldObj)) {
			return false
		}
	}
	return true
}

//checFieldsWithSort is a check for maps that uses an arbitrary sort to ensure it is
//comparing the right values
func checkFieldsWithSort(mergedObj map[string]interface{}, oldObj map[string]interface{}) (matches bool) {
	//needed to compare lists, since merge messes up the order
	if len(mergedObj) < len(oldObj) {
		return false
	}
	match := true
	for i, mVal := range mergedObj {
		switch mVal := mVal.(type) {
		case (map[string]interface{}):
			//if field is a map, recurse to check for a match
			oVal, ok := oldObj[i].(map[string]interface{})
			if !ok {
				match = false
				break
			} else if !checkFieldsWithSort(mVal, oVal) {
				match = false
			}
		case ([]map[string]interface{}):
			//if field is a list of maps, use checkListFieldsWithSort to check for a match
			oVal, ok := oldObj[i].([]map[string]interface{})
			if !ok {
				match = false
				break
			} else if !checkListFieldsWithSort(mVal, oVal) {
				match = false
			}
		case ([]interface{}):
			//if field is a generic list, sort and iterate through them to make sure each value matches
			oVal, ok := oldObj[i].([]interface{})
			if !ok {
				match = false
				break
			}
			if len(mVal) != len(oVal) {
				match = false
			} else {
				if !checkListsMatch(oVal, mVal) {
					match = false
				}
			}
		default:
			//if field is not an object, just do a basic compare to check for a match
			oVal := oldObj[i]
			if fmt.Sprint(oVal) != fmt.Sprint(mVal) {
				match = false
			}
		}
	}
	return match
}

//checkListFieldsWithSort is a check for lists of maps that uses an arbitrary sort to ensure it is
//comparing the right values
func checkListFieldsWithSort(mergedObj []map[string]interface{}, oldObj []map[string]interface{}) (matches bool) {
	sort.Slice(oldObj, func(i, j int) bool {
		return fmt.Sprintf("%v", oldObj[i]) < fmt.Sprintf("%v", oldObj[j])
	})
	sort.Slice(mergedObj, func(x, y int) bool {
		return fmt.Sprintf("%v", mergedObj[x]) < fmt.Sprintf("%v", mergedObj[y])
	})

	//needed to compare lists, since merge messes up the order
	match := true
	for listIdx, mergedItem := range mergedObj {
		oldItem := oldObj[listIdx]
		for i, mVal := range mergedItem {
			switch mVal := mVal.(type) {
			case ([]interface{}):
				//if a map in the list contains a nested list, sort and check for equality
				oVal, ok := oldItem[i].([]interface{})
				if !ok {
					match = false
					break
				}
				if len(mVal) != len(oVal) {
					match = false
				} else {
					if !checkListsMatch(oVal, mVal) {
						match = false
					}
				}
			case (map[string]interface{}):
				//if a map in the list contains another map, check fields for equality
				if !checkFieldsWithSort(mVal, oldItem[i].(map[string]interface{})) {
					match = false
				}
			default:
				//if the field in the map is not an object, just do a generic check
				oVal := oldItem[i]
				if fmt.Sprint(oVal) != fmt.Sprint(mVal) {
					match = false
				}
			}
		}
	}
	return match
}

//checkListsMatch is a generic list check that uses an arbitrary sort to ensure it is comparing the right values
func checkListsMatch(oldVal []interface{}, mergedVal []interface{}) (m bool) {
	oVal := append([]interface{}{}, oldVal...)
	mVal := append([]interface{}{}, mergedVal...)
	if (oVal == nil && mVal != nil) || (oVal != nil && mVal == nil) {
		return false
	}
	sort.Slice(oVal, func(i, j int) bool {
		return fmt.Sprintf("%v", oVal[i]) < fmt.Sprintf("%v", oVal[j])
	})
	sort.Slice(mVal, func(x, y int) bool {
		return fmt.Sprintf("%v", mVal[x]) < fmt.Sprintf("%v", mVal[y])
	})
	match := true
	if len(mVal) != len(oVal) {
		return false
	}
	for idx, oNestedVal := range oVal {
		switch oNestedVal := oNestedVal.(type) {
		case (map[string]interface{}):
			//if list contains maps, recurse on those maps to check for a match
			if !checkFieldsWithSort(mVal[idx].(map[string]interface{}), oNestedVal) {
				match = false
			}
		default:
			//otherwise, just do a generic check
			if fmt.Sprint(oNestedVal) != fmt.Sprint(mVal[idx]) {
				match = false
			}
		}
	}
	return match
}

func stringInSlice(a string, list []string) bool {
	for _, b := range list {
		if b == a {
			return true
		}
	}
	return false
}

func isDenylisted(key string) (result bool) {
	denylist := []string{"apiVersion", "kind"}
	for _, val := range denylist {
		if key == val {
			return true
		}
	}
	return false
}

func isAutogenerated(key string) (result bool) {
	denylist := []string{"kubectl.kubernetes.io/last-applied-configuration"}
	for _, val := range denylist {
		if key == val {
			return true
		}
	}
	return false
}

func formatTemplate(unstruct unstructured.Unstructured, key string) (obj interface{}) {
	if key == "metadata" {
		metadata := unstruct.Object[key].(map[string]interface{})
		return formatMetadata(metadata)
	}
	return unstruct.Object[key]
}

func formatMetadata(metadata map[string]interface{}) (formatted map[string]interface{}) {
	md := map[string]interface{}{}
	if labels, ok := metadata["labels"]; ok {
		md["labels"] = labels
	}
	if annos, ok := metadata["annotations"]; ok {
		noAutogenerated := map[string]interface{}{}
		for key := range annos.(map[string]interface{}) {
			if !isAutogenerated(key) {
				noAutogenerated[key] = annos.(map[string]interface{})[key]
			}
		}
		if len(noAutogenerated) > 0 {
			md["annotations"] = noAutogenerated
		}
	}
	return md
}

// Format name of resource with its namespace (if it has one)
func createResourceNameStr(names []string, namespace string, namespaced bool) (nameStr string) {
	sort.Strings(names)
	nameStr = "["
	for i, name := range names {
		nameStr += name
		if i != len(names)-1 {
			nameStr += ", "
		}
	}
	nameStr += "]"
	// No names found--return empty string instead
	if nameStr == "[]" {
		nameStr = ""
	}
	// Add namespace
	if namespaced {
		// Add a space if there are names
		if nameStr != "" {
			nameStr += " "
		}
		nameStr += "in namespace " + namespace
	}
	return nameStr
}

//createMustHaveStatus generates a status for a musthave/mustonlyhave policy
func createMustHaveStatus(desiredName string, kind string, complianceObjects map[string]map[string]interface{},
	namespaced bool, plc *policyv1.ConfigurationPolicy, indx int, compliant bool) (update bool) {
	// Parse discovered resources
	nameList := []string{}
	sortedNamespaces := []string{}
	for n := range complianceObjects {
		sortedNamespaces = append(sortedNamespaces, n)
	}
	sort.Strings(sortedNamespaces)

	// Noncompliant with no resources -- return violation immediately
	if !compliant && desiredName == "" {
		message := fmt.Sprintf("No instances of `%v` found as specified", kind)
		if len(sortedNamespaces) > 0 {
			message = fmt.Sprintf("No instances of `%v` found as specified in namespaces: %v",
				kind, strings.Join(sortedNamespaces, ", "))
		}
		return createViolation(plc, indx, "K8s does not have a `must have` object", message)
	}

	for i := range sortedNamespaces {
		ns := sortedNamespaces[i]
		names := complianceObjects[ns]["names"].([]string)
		nameStr := createResourceNameStr(names, ns, namespaced)
		if compliant {
			nameStr += " found"
		} else {
			if complianceObjects[ns]["reason"] == reasonWantFoundNoMatch {
				nameStr += " found but not as specified"
			} else {
				nameStr += " missing"
			}
		}
		if !stringInSlice(nameStr, nameList) {
			nameList = append(nameList, nameStr)
		}
	}
	names := strings.Join(nameList, "; ")
	// Compliant -- return notification
	if compliant {
		message := fmt.Sprintf("%v %v as specified, therefore this Object template is compliant", kind, names)
		return createNotification(plc, indx, "K8s `must have` object already exists", message)
	}
	// Noncompliant -- return violation
	message := fmt.Sprintf("%v not found: %v", kind, names)
	return createViolation(plc, indx, "K8s does not have a `must have` object", message)
}

//createMustNotHaveStatus generates a status for a mustnothave policy
func createMustNotHaveStatus(kind string, complianceObjects map[string]map[string]interface{},
	namespaced bool, plc *policyv1.ConfigurationPolicy, indx int, compliant bool) (update bool) {
	nameList := []string{}
	sortedNamespaces := []string{}
	for n := range complianceObjects {
		sortedNamespaces = append(sortedNamespaces, n)
	}
	sort.Strings(sortedNamespaces)
	for i := range sortedNamespaces {
		ns := sortedNamespaces[i]
		names := complianceObjects[ns]["names"].([]string)
		nameStr := createResourceNameStr(names, ns, namespaced)
		if !stringInSlice(nameStr, nameList) {
			nameList = append(nameList, nameStr)
		}
	}
	names := strings.Join(nameList, "; ")
	// Compliant -- return notification
	if compliant {
		message := fmt.Sprintf("%v %v missing as expected, therefore this Object template is compliant", kind, names)
		return createNotification(plc, indx, "K8s `must not have` object already missing", message)
	}
	// Noncompliant -- return violation
	message := fmt.Sprintf("%v found: %v", kind, names)
	return createViolation(plc, indx, "K8s has a `must not have` object", message)
}
