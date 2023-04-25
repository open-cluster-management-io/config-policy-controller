// Copyright (c) 2021 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package controllers

import (
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"

	gocmp "github.com/google/go-cmp/cmp"
	apiRes "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/json"

	policyv1 "open-cluster-management.io/config-policy-controller/api/v1"
)

// addRelatedObjects builds the list of kubernetes resources related to the policy.  The list contains
// details on whether the object is compliant or not compliant with the policy.  The results are updated in the
// policy's Status information.
func addRelatedObjects(
	compliant bool,
	rsrc schema.GroupVersionResource,
	kind string,
	namespace string,
	namespaced bool,
	objNames []string,
	reason string,
	creationInfo *policyv1.ObjectProperties,
) (relatedObjects []policyv1.RelatedObject) {
	for _, name := range objNames {
		// Initialize the related object from the object handling
		var relatedObject policyv1.RelatedObject
		if compliant {
			relatedObject.Compliant = string(policyv1.Compliant)
		} else {
			relatedObject.Compliant = string(policyv1.NonCompliant)
		}

		if creationInfo != nil {
			relatedObject.Properties = creationInfo
		}

		relatedObject.Reason = reason
		metadata := policyv1.ObjectMetadata{}
		metadata.Name = name

		if namespaced {
			metadata.Namespace = namespace
		} else {
			metadata.Namespace = ""
		}

		relatedObject.Object.APIVersion = rsrc.GroupVersion().String()
		relatedObject.Object.Kind = kind
		relatedObject.Object.Metadata = metadata
		relatedObjects = updateRelatedObjectsStatus(relatedObjects, relatedObject)
	}

	return relatedObjects
}

// unmarshalFromJSON unmarshals raw JSON data into an object
func unmarshalFromJSON(rawData []byte) (unstructured.Unstructured, error) {
	var unstruct unstructured.Unstructured

	if jsonErr := json.Unmarshal(rawData, &unstruct.Object); jsonErr != nil {
		log.Error(jsonErr, "Could not unmarshal data from JSON")

		return unstruct, jsonErr
	}

	return unstruct, nil
}

// updateRelatedObjectsStatus adds or updates the RelatedObject in the policy status.
func updateRelatedObjectsStatus(
	list []policyv1.RelatedObject, relatedObject policyv1.RelatedObject,
) (result []policyv1.RelatedObject) {
	present := false

	for index, currentObject := range list {
		if currentObject.Object.APIVersion == relatedObject.Object.APIVersion &&
			currentObject.Object.Kind == relatedObject.Object.Kind &&
			currentObject.Object.Metadata.Name == relatedObject.Object.Metadata.Name &&
			currentObject.Object.Metadata.Namespace == relatedObject.Object.Metadata.Namespace {
			present = true

			if currentObject.Compliant != relatedObject.Compliant {
				list[index] = relatedObject
			}
		}
	}

	if !present {
		list = append(list, relatedObject)
	}

	return list
}

// equalObjWithSort is a wrapper function that calls the correct function to check equality depending on what
// type the objects to compare are
func equalObjWithSort(mergedObj interface{}, oldObj interface{}) (areEqual bool) {
	switch mergedObj := mergedObj.(type) {
	case map[string]interface{}:
		if oldObjMap, ok := oldObj.(map[string]interface{}); ok {
			return checkFieldsWithSort(mergedObj, oldObjMap)
		}
		// this includes the case where oldObj is nil
		return false
	case []interface{}:
		if len(mergedObj) == 0 && oldObj == nil {
			return true
		}

		if oldObjList, ok := oldObj.([]interface{}); ok {
			return checkListsMatch(mergedObj, oldObjList)
		}

		return false
	default:
		// NOTE: when type is string, int, bool
		var oVal interface{}

		if oldObj == nil && mergedObj != nil {
			ref := reflect.ValueOf(mergedObj)
			oVal = reflect.Zero(ref.Type()).Interface()

			return fmt.Sprint(oVal) == fmt.Sprint(mergedObj)
		}

		if !reflect.DeepEqual(fmt.Sprint(mergedObj), fmt.Sprint(oldObj)) {
			return false
		}
	}

	return true
}

// checFieldsWithSort is a check for maps that uses an arbitrary sort to ensure it is
// comparing the right values
func checkFieldsWithSort(mergedObj map[string]interface{}, oldObj map[string]interface{}) (matches bool) {
	// needed to compare lists, since merge messes up the order
	if len(mergedObj) < len(oldObj) {
		return false
	}

	for i, mVal := range mergedObj {
		switch mVal := mVal.(type) {
		case map[string]interface{}:
			// if field is a map, recurse to check for a match
			oVal, ok := oldObj[i].(map[string]interface{})
			if !ok {
				if len(mVal) == 0 {
					break
				}

				return false
			}

			if !checkFieldsWithSort(mVal, oVal) {
				return false
			}
		case []map[string]interface{}:
			// if field is a list of maps, use checkListFieldsWithSort to check for a match
			oVal, ok := oldObj[i].([]map[string]interface{})
			if !ok || !checkListFieldsWithSort(mVal, oVal) {
				return false
			}
		case []interface{}:
			// if field is a generic list, sort and iterate through them to make sure each value matches
			oVal, ok := oldObj[i].([]interface{})
			if !ok {
				if len(mVal) == 0 {
					break
				}

				return false
			}

			if len(mVal) != len(oVal) || !checkListsMatch(oVal, mVal) {
				return false
			}
		case string:
			// extra check to see if value is a byte value
			mQty, err := apiRes.ParseQuantity(mVal)
			if err != nil {
				oVal := oldObj[i]
				if oVal == nil {
					oVal = ""
				}
				// An error indicates the value is a regular string, so check equality normally
				if fmt.Sprint(oVal) != fmt.Sprint(mVal) {
					return false
				}
			} else {
				// if the value is a quantity of bytes, convert original
				oVal, ok := oldObj[i].(string)
				if !ok {
					return false
				}

				oQty, err := apiRes.ParseQuantity(oVal)
				if err != nil || !oQty.Equal(mQty) {
					return false
				}
			}
		default:
			// if field is not an object, just do a basic compare to check for a match
			oVal := oldObj[i]
			// When oVal value omitted because of omitempty
			if oVal == nil && mVal != nil {
				ref := reflect.ValueOf(mVal)
				oVal = reflect.Zero(ref.Type()).Interface()
			}

			if fmt.Sprint(oVal) != fmt.Sprint(mVal) {
				return false
			}
		}
	}

	return true
}

// checkListFieldsWithSort is a check for lists of maps that uses an arbitrary sort to ensure it is
// comparing the right values
func checkListFieldsWithSort(mergedObj []map[string]interface{}, oldObj []map[string]interface{}) (matches bool) {
	sort.Slice(oldObj, func(i, j int) bool {
		return fmt.Sprintf("%v", oldObj[i]) < fmt.Sprintf("%v", oldObj[j])
	})
	sort.Slice(mergedObj, func(x, y int) bool {
		return fmt.Sprintf("%v", mergedObj[x]) < fmt.Sprintf("%v", mergedObj[y])
	})

	// needed to compare lists, since merge messes up the order
	for listIdx, mergedItem := range mergedObj {
		oldItem := oldObj[listIdx]

		for i, mVal := range mergedItem {
			switch mVal := mVal.(type) {
			case []interface{}:
				// if a map in the list contains a nested list, sort and check for equality
				if oVal, ok := oldItem[i].([]interface{}); ok {
					return len(mVal) == len(oVal) && checkListsMatch(oVal, mVal)
				}

				return false
			case map[string]interface{}:
				// if a map in the list contains another map, check fields for equality
				if oVal, ok := oldItem[i].(map[string]interface{}); ok {
					return len(mVal) == len(oVal) && checkFieldsWithSort(mVal, oVal)
				}

				return false
			case string:
				// extra check to see if value is a byte value
				mQty, err := apiRes.ParseQuantity(mVal)
				if err != nil {
					// An error indicates the value is a regular string, so check equality normally
					if fmt.Sprint(oldItem[i]) != fmt.Sprint(mVal) {
						return false
					}
				} else {
					// if the value is a quantity of bytes, convert original
					oVal, ok := oldItem[i].(string)
					if !ok {
						return false
					}

					oQty, err := apiRes.ParseQuantity(oVal)
					if err != nil || !oQty.Equal(mQty) {
						return false
					}
				}
			default:
				// if the field in the map is not an object, just do a generic check
				if fmt.Sprint(oldItem[i]) != fmt.Sprint(mVal) {
					return false
				}
			}
		}
	}

	return true
}

// checkListsMatch is a generic list check that uses an arbitrary sort to ensure it is comparing the right values
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

	if len(mVal) != len(oVal) {
		return false
	}

	for idx, oNestedVal := range oVal {
		switch oNestedVal := oNestedVal.(type) {
		case map[string]interface{}:
			// if list contains maps, recurse on those maps to check for a match
			if mVal, ok := mVal[idx].(map[string]interface{}); ok {
				return len(mVal) == len(oNestedVal) && checkFieldsWithSort(mVal, oNestedVal)
			}

			return false
		default:
			// otherwise, just do a generic check
			if fmt.Sprint(oNestedVal) != fmt.Sprint(mVal[idx]) {
				return false
			}
		}
	}

	return true
}

func filterUnwantedAnnotations(input map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{})

	for key, val := range input {
		// This could use a denylist if we need to filter more annotations in the future.
		if key != "kubectl.kubernetes.io/last-applied-configuration" {
			out[key] = val
		}
	}

	return out
}

// formatTemplate returns the value of the input key in a manner that the controller can use for comparisons.
func formatTemplate(unstruct unstructured.Unstructured, key string) (obj interface{}) {
	if key == "metadata" {
		metadata, ok := unstruct.Object[key].(map[string]interface{})
		if !ok {
			return metadata // it will just be empty
		}

		return formatMetadata(metadata)
	}

	return unstruct.Object[key]
}

// formatMetadata takes the input object metadata and returns a slimmed down version which just includes the "labels"
// and "annotations" values. Deny listed annotations are excluded. This allows the controller to compare only the
// metadata fields it supports.
func formatMetadata(metadata map[string]interface{}) (formatted map[string]interface{}) {
	md := map[string]interface{}{}

	if labels, ok := metadata["labels"]; ok {
		md["labels"] = labels
	}

	if annosTemp, ok := metadata["annotations"]; ok {
		if annos, ok := annosTemp.(map[string]interface{}); ok {
			md["annotations"] = filterUnwantedAnnotations(annos)
		} else {
			// When a non-map is provided, set the value directly
			md["annotations"] = annosTemp
		}
	}

	return md
}

func fmtMetadataForCompare(
	metadataTemp, metadataExisting map[string]interface{},
) (formatted, formattedExisting map[string]interface{}) {
	mdTemp := map[string]interface{}{}
	mdExisting := map[string]interface{}{}

	if labelsTemp, ok := metadataTemp["labels"]; ok {
		mdTemp["labels"] = labelsTemp

		if labelsExisting, ok := metadataExisting["labels"]; ok {
			mdExisting["labels"] = labelsExisting
		}
	}

	if annosTemp, ok := metadataTemp["annotations"]; ok {
		if annos, ok := annosTemp.(map[string]interface{}); ok {
			mdTemp["annotations"] = filterUnwantedAnnotations(annos)
		} else {
			mdTemp["annotations"] = annosTemp
		}

		if annosExisting, ok := metadataExisting["annotations"]; ok {
			if annos, ok := annosExisting.(map[string]interface{}); ok {
				mdExisting["annotations"] = filterUnwantedAnnotations(annos)
			} else {
				mdExisting["annotations"] = annosExisting
			}
		}
	}

	return mdTemp, mdExisting
}

// Format name of resource with its namespace (if it has one)
func identifierStr(names []string, namespace string, namespaced bool) (nameStr string) {
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

func createStatus(
	tmplID templateIdentifier,
	complianceObjects map[string]map[string]interface{},
	plc *policyv1.ConfigurationPolicy,
	compliant bool,
	objShouldExist bool,
) (update bool) {
	// Parse discovered resources
	foundIdentifiers := make(map[string]bool)
	sortedNamespaces := []string{}

	for n := range complianceObjects {
		sortedNamespaces = append(sortedNamespaces, n)
	}

	sort.Strings(sortedNamespaces)

	// Noncompliant with no resources -- return violation immediately
	if objShouldExist && !compliant && tmplID.desiredName == "" {
		message := fmt.Sprintf("No instances of `%v` found as specified", tmplID.kind)
		if tmplID.namespaced && len(sortedNamespaces) > 0 {
			message += fmt.Sprintf(" in namespaces: %v", strings.Join(sortedNamespaces, ", "))
		}

		return addConditionToStatus(plc, tmplID.index, false, "K8s does not have a `must have` object", message)
	}

	for _, ns := range sortedNamespaces {
		// if the assertion fails, `names` will effectively be an empty list, which is fine.
		names, _ := complianceObjects[ns]["names"].([]string)
		idStr := identifierStr(names, ns, tmplID.namespaced)

		if objShouldExist {
			if compliant {
				idStr += " found"
			} else if complianceObjects[ns]["reason"] == reasonWantFoundNoMatch {
				idStr += " found but not as specified"
			} else {
				idStr += " missing"
			}
		}

		foundIdentifiers[idStr] = true
	}

	niceNames := sortAndJoinKeys(foundIdentifiers, "; ")

	var reason, msg string

	if objShouldExist {
		if compliant {
			reason = "K8s `must have` object already exists"
			msg = fmt.Sprintf("%v %v as specified, therefore this Object template is compliant", tmplID.kind, niceNames)
		} else {
			reason = "K8s does not have a `must have` object"
			msg = fmt.Sprintf("%v not found: %v", tmplID.kind, niceNames)
		}
	} else {
		if compliant {
			reason = "K8s `must not have` object already missing"
			msg = fmt.Sprintf(
				"%v %v missing as expected, therefore this Object template is compliant", tmplID.kind, niceNames)
		} else {
			reason = "K8s has a `must not have` object"
			msg = fmt.Sprintf("%v found: %v", tmplID.kind, niceNames)
		}
	}

	return addConditionToStatus(plc, tmplID.index, compliant, reason, msg)
}

func sortAndJoinKeys(m map[string]bool, sep string) string {
	keys := make([]string, len(m))
	i := 0

	for key := range m {
		keys[i] = key
		i++
	}

	sort.Strings(keys)

	return strings.Join(keys, sep)
}

func objHasFinalizer(obj metav1.Object, finalizer string) bool {
	for _, existingFinalizer := range obj.GetFinalizers() {
		if existingFinalizer == finalizer {
			return true
		}
	}

	return false
}

func removeObjFinalizerPatch(obj metav1.Object, finalizer string) []byte {
	for i, existingFinalizer := range obj.GetFinalizers() {
		if existingFinalizer == finalizer {
			return []byte(`[{"op":"remove","path":"/metadata/finalizers/` + strconv.FormatInt(int64(i), 10) + `"}]`)
		}
	}

	return nil
}

func containRelated(arr []policyv1.RelatedObject, input policyv1.RelatedObject) bool {
	// should compare only object
	for _, r := range arr {
		if gocmp.Equal(r.Object, input.Object) {
			return true
		}
	}

	return false
}
