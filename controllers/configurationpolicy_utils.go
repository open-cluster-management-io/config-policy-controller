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
	"github.com/pmezard/go-difflib/difflib"
	depclient "github.com/stolostron/kubernetes-dependency-watches/client"
	apiRes "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/json"
	"sigs.k8s.io/yaml"

	policyv1 "open-cluster-management.io/config-policy-controller/api/v1"
)

// addRelatedObjects builds the list of kubernetes resources related to the policy.  The list contains
// details on whether the object is compliant or not compliant with the policy.  The results are updated in the
// policy's Status information.
func addRelatedObjects(
	compliant bool,
	scopedGVR depclient.ScopedGVR,
	kind string,
	namespace string,
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

		if scopedGVR.Namespaced {
			metadata.Namespace = namespace
		} else {
			metadata.Namespace = ""
		}

		relatedObject.Object.APIVersion = scopedGVR.GroupVersion().String()
		relatedObject.Object.Kind = kind
		relatedObject.Object.Metadata = metadata
		relatedObjects = addOrUpdateRelatedObject(relatedObjects, relatedObject)
	}

	return relatedObjects
}

// addCondensedRelatedObjs does not include all of relatedObjs.
// The Name field is "-". The list of objects will be presented on the console.
func addCondensedRelatedObjs(
	scopedGVR depclient.ScopedGVR,
	compliant bool,
	kind string,
	namespace string,
	reason string,
) (relatedObjects []policyv1.RelatedObject) {
	metadata := policyv1.ObjectMetadata{Name: "-"}

	if scopedGVR.Namespaced {
		metadata.Namespace = namespace
	} else {
		metadata.Namespace = ""
	}

	// Initialize the related object from the object handling
	relatedObject := policyv1.RelatedObject{
		Reason: reason,
		Object: policyv1.ObjectResource{
			APIVersion: scopedGVR.GroupVersion().String(),
			Kind:       kind,
			Metadata:   metadata,
		},
	}

	if compliant {
		relatedObject.Compliant = string(policyv1.Compliant)
	} else {
		relatedObject.Compliant = string(policyv1.NonCompliant)
	}

	relatedObjects = append(relatedObjects, relatedObject)

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

// addOrUpdateRelatedObject adds or updates the RelatedObject in the given list
// and returns the resulting updated list
func addOrUpdateRelatedObject(
	list []policyv1.RelatedObject, relatedObject policyv1.RelatedObject,
) (result []policyv1.RelatedObject) {
	present := false

	for index, currentObject := range list {
		if currentObject.Object.APIVersion == relatedObject.Object.APIVersion &&
			currentObject.Object.Kind == relatedObject.Object.Kind &&
			currentObject.Object.Metadata.Name == relatedObject.Object.Metadata.Name &&
			currentObject.Object.Metadata.Namespace == relatedObject.Object.Metadata.Namespace {
			present = true

			if currentObject.Compliant != relatedObject.Compliant ||
				!reflect.DeepEqual(currentObject.Properties, relatedObject.Properties) {
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
func equalObjWithSort(mergedObj interface{}, oldObj interface{}, zeroValueEqualsNil bool) (areEqual bool) {
	switch mergedObj := mergedObj.(type) {
	case map[string]interface{}:
		if oldObjMap, ok := oldObj.(map[string]interface{}); ok {
			return checkFieldsWithSort(mergedObj, oldObjMap, zeroValueEqualsNil)
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
	default: // when mergedObj's type is string, int, bool, or nil
		if zeroValueEqualsNil {
			if oldObj == nil && mergedObj != nil {
				// compare the zero value of mergedObj's type to mergedObj
				ref := reflect.ValueOf(mergedObj)
				zero := reflect.Zero(ref.Type()).Interface()

				return fmt.Sprint(zero) == fmt.Sprint(mergedObj)
			}

			if mergedObj == nil && oldObj != nil {
				// compare the zero value of oldObj's type to oldObj
				ref := reflect.ValueOf(oldObj)
				zero := reflect.Zero(ref.Type()).Interface()

				return fmt.Sprint(zero) == fmt.Sprint(oldObj)
			}
		}

		return fmt.Sprint(mergedObj) == fmt.Sprint(oldObj)
	}
}

// checkFieldsWithSort is a check for maps that uses an arbitrary sort to ensure it is
// comparing the right values
func checkFieldsWithSort(
	mergedObj map[string]interface{}, oldObj map[string]interface{}, zeroValueEqualsNil bool,
) (matches bool) {
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
				if zeroValueEqualsNil && len(mVal) == 0 {
					break
				}

				return false
			}

			if !checkFieldsWithSort(mVal, oVal, zeroValueEqualsNil) {
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
				oVal, ok := oldObj[i]
				if !ok {
					return false
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

// sortAndSprint sorts any lists in the input, and formats the resulting object as a string
func sortAndSprint(item interface{}) string {
	switch item := item.(type) {
	case map[string]interface{}:
		sorted := make(map[string]string, len(item))

		for key, val := range item {
			sorted[key] = sortAndSprint(val)
		}

		return fmt.Sprintf("%v", sorted)
	case []interface{}:
		sorted := make([]string, len(item))

		for i, val := range item {
			sorted[i] = sortAndSprint(val)
		}

		sort.Slice(sorted, func(x, y int) bool {
			return sorted[x] < sorted[y]
		})

		return fmt.Sprintf("%v", sorted)
	default:
		return fmt.Sprintf("%v", item)
	}
}

// checkListsMatch is a generic list check that uses an arbitrary sort to ensure it is comparing the right values
func checkListsMatch(oldVal []interface{}, mergedVal []interface{}) (m bool) {
	if (oldVal == nil && mergedVal != nil) || (oldVal != nil && mergedVal == nil) {
		return false
	}

	if len(mergedVal) != len(oldVal) {
		return false
	}

	// Make copies of the lists, so we can sort them without mutating this function's inputs
	oVal := append([]interface{}{}, oldVal...)
	mVal := append([]interface{}{}, mergedVal...)

	sort.Slice(oVal, func(i, j int) bool {
		return sortAndSprint(oVal[i]) < sortAndSprint(oVal[j])
	})
	sort.Slice(mVal, func(x, y int) bool {
		return sortAndSprint(mVal[x]) < sortAndSprint(mVal[y])
	})

	for idx, oNestedVal := range oVal {
		switch oNestedVal := oNestedVal.(type) {
		case map[string]interface{}:
			// if list contains maps, recurse on those maps to check for a match
			if mVal, ok := mVal[idx].(map[string]interface{}); ok {
				if !checkFieldsWithSort(mVal, oNestedVal, true) {
					return false
				}

				continue
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
	merged, existing map[string]interface{}, keepSCC bool,
) (formattedMerged, formattedExisting map[string]interface{}) {
	formattedMerged = formatMetadata(merged)
	formattedExisting = formatMetadata(existing)

	if _, mergedHasLabels := formattedMerged["labels"]; !mergedHasLabels {
		delete(formattedExisting, "labels")
	}

	if _, mergedHasAnnos := formattedMerged["annotations"]; !mergedHasAnnos {
		delete(formattedExisting, "annotations")

		return formattedMerged, formattedExisting
	}

	if !keepSCC {
		return formattedMerged, formattedExisting
	}

	existingAnnos, ok := formattedExisting["annotations"].(map[string]interface{})
	if !ok {
		return formattedMerged, formattedExisting
	}

	mergedAnnos, ok := formattedMerged["annotations"].(map[string]interface{})
	if !ok {
		return formattedMerged, formattedExisting
	}

	// Copy existing SCC annotations to the merged metadata
	for key, val := range existingAnnos {
		if !strings.HasPrefix(key, "openshift.io/sa.scc.") {
			continue
		}

		if _, alreadyDefined := mergedAnnos[key]; !alreadyDefined {
			mergedAnnos[key] = val
		}
	}

	formattedMerged["annotations"] = mergedAnnos

	return formattedMerged, formattedExisting
}

// Format name of resource with its namespace (if it has one)
func identifierStr(names []string, namespace string) (nameStr string) {
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
	if namespace != "" {
		// Add a space if there are names
		if nameStr != "" {
			nameStr += " "
		}

		nameStr += "in namespace " + namespace
	}

	return nameStr
}

// createStatus generates the status reason and message for the object template after processing. resourceName indicates
// the name of the resource (e.g. namespaces), and not the kind (e.g. Namespace).
func createStatus(
	resourceName string, namespaceToEvent map[string]*objectTmplEvalResultWithEvent,
) (
	compliant bool, compliancyDetailsReason, compliancyDetailsMsg string,
) {
	reasonToNamespaceToEvent := map[string]map[string]*objectTmplEvalResultWithEvent{}
	compliant = true
	// If all objects are compliant, this only contains compliant events. If there is at least one noncompliant
	// object, then this will only contain noncompliant events.
	filteredNamespaceToEvent := map[string]*objectTmplEvalResultWithEvent{}

	for namespace, eventWithCtx := range namespaceToEvent {
		// If a noncompliant event is encountered, then reset the maps to only include noncompliant events.
		if compliant && !eventWithCtx.event.compliant {
			compliant = false
			filteredNamespaceToEvent = map[string]*objectTmplEvalResultWithEvent{}
			reasonToNamespaceToEvent = map[string]map[string]*objectTmplEvalResultWithEvent{}
		}

		if compliant != eventWithCtx.event.compliant {
			continue
		}

		filteredNamespaceToEvent[namespace] = eventWithCtx

		if _, ok := reasonToNamespaceToEvent[eventWithCtx.event.reason]; !ok {
			reasonToNamespaceToEvent[eventWithCtx.event.reason] = map[string]*objectTmplEvalResultWithEvent{}
		}

		reasonToNamespaceToEvent[eventWithCtx.event.reason][namespace] = eventWithCtx
	}

	// Create an order of the reasons so that the generated reason and compliance message is deterministic.
	orderedReasons := []string{
		reasonWantFoundExists,
		reasonWantFoundCreated,
		reasonUpdateSuccess,
		reasonDeleteSuccess,
		reasonWantFoundDNE,
		reasonWantFoundNoMatch,
		reasonWantNotFoundDNE,
		reasonWantNotFoundExists,
	}
	otherReasons := []string{}

	for reason := range reasonToNamespaceToEvent {
		found := false

		for _, orderedReason := range orderedReasons {
			if orderedReason == reason {
				found = true

				break
			}
		}

		if !found {
			otherReasons = append(otherReasons, reason)
		}
	}

	sort.Strings(otherReasons)
	orderedReasons = append(orderedReasons, otherReasons...)

	// The "reason" is more specific in the compliancyDetails section than in the relatedObjects section.
	// It may be worth using the same message in both eventually.
	for _, reason := range orderedReasons {
		namespaceToEvent, ok := reasonToNamespaceToEvent[reason]
		if !ok {
			continue
		}

		sortedNamespaces := make([]string, 0, len(namespaceToEvent))

		for ns := range namespaceToEvent {
			sortedNamespaces = append(sortedNamespaces, ns)
		}

		sort.Strings(sortedNamespaces)

		// If the object template was unnamed, then the object names can be different per namespace. If it was named,
		// all will be the same, but this accounts for both.
		sortedObjectNamesStrs := []string{}
		// Note that the namespace slices will be ordered based on how they are populated.
		objectNameStrsToNamespaces := map[string][]string{}

		for _, ns := range sortedNamespaces {
			namesStr := ""

			if len(namespaceToEvent[ns].result.objectNames) > 0 {
				namesStr = " [" + strings.Join(namespaceToEvent[ns].result.objectNames, ", ") + "]"
			}

			if _, ok := objectNameStrsToNamespaces[namesStr]; !ok {
				sortedObjectNamesStrs = append(sortedObjectNamesStrs, namesStr)
			}

			objectNameStrsToNamespaces[namesStr] = append(objectNameStrsToNamespaces[namesStr], ns)
		}

		sort.Strings(sortedObjectNamesStrs)

		// Process the object name strings in order to ensure a deterministic reason and message.
		for i, namesStr := range sortedObjectNamesStrs {
			if compliancyDetailsMsg != "" {
				compliancyDetailsMsg += "; "
			}

			var generatedReason, generatedMsg string

			switch reason {
			case reasonWantFoundExists:
				generatedReason = "K8s `must have` object already exists"
				generatedMsg = fmt.Sprintf("%s%s found as specified", resourceName, namesStr)
			case reasonWantFoundCreated:
				generatedReason = reasonWantFoundCreated
				generatedMsg = fmt.Sprintf("%s%s was created successfully", resourceName, namesStr)
			case reasonUpdateSuccess:
				generatedReason = reasonUpdateSuccess
				generatedMsg = fmt.Sprintf("%s%s was updated successfully", resourceName, namesStr)
			case reasonDeleteSuccess:
				generatedReason = reasonDeleteSuccess
				generatedMsg = fmt.Sprintf("%s%s was deleted successfully", resourceName, namesStr)
			case reasonWantFoundDNE:
				generatedReason = "K8s does not have a `must have` object"
				compliancyDetailsMsg += fmt.Sprintf("%s%s not found", resourceName, namesStr)
			case reasonWantFoundNoMatch:
				generatedReason = "K8s does not have a `must have` object"
				compliancyDetailsMsg += fmt.Sprintf("%s%s found but not as specified", resourceName, namesStr)
			case reasonWantNotFoundExists:
				generatedReason = "K8s has a `must not have` object"
				compliancyDetailsMsg += fmt.Sprintf("%s%s found", resourceName, namesStr)
			case reasonWantNotFoundDNE:
				generatedReason = "K8s `must not have` object already missing"
				compliancyDetailsMsg += fmt.Sprintf("%s%s missing as expected", resourceName, namesStr)
			default:
				// If it's not one of the above reasons, then skip consolidation. This is likely an error being
				// reported.
				if i == 0 {
					if compliancyDetailsReason != "" {
						compliancyDetailsReason += "; "
					}

					compliancyDetailsReason += reason
				}

				for j, ns := range objectNameStrsToNamespaces[namesStr] {
					if j != 0 {
						compliancyDetailsMsg += "; "
					}

					compliancyDetailsMsg += namespaceToEvent[ns].event.message
				}

				// Assume the included messages include the namespace.
				continue
			}

			// This prevents repeating the same reason for each unique object name list.
			if i == 0 {
				if compliancyDetailsReason != "" {
					compliancyDetailsReason += "; "
				}

				compliancyDetailsReason += generatedReason
			}

			compliancyDetailsMsg += generatedMsg

			// If it is namespaced, include the namespaces that were checked. A namespace of "" indicates
			// cluster scoped. This length check is not necessary but is added for additional safety in case the logic
			// above is changed.
			if len(objectNameStrsToNamespaces[namesStr]) > 0 && objectNameStrsToNamespaces[namesStr][0] != "" {
				if len(objectNameStrsToNamespaces[namesStr]) > 1 {
					compliancyDetailsMsg += fmt.Sprintf(
						" in namespaces: %s", strings.Join(objectNameStrsToNamespaces[namesStr], ", "),
					)
				} else {
					compliancyDetailsMsg += fmt.Sprintf(" in namespace %s", objectNameStrsToNamespaces[namesStr][0])
				}
			}
		}
	}

	return
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

func containRelated(related []policyv1.RelatedObject, input policyv1.RelatedObject) bool {
	// should compare name, APIVersion, Kind  and namespace
	for _, r := range related {
		if gocmp.Equal(r.Object, input.Object) {
			return true
		}
	}

	return false
}

// generateDiff takes two unstructured objects and returns the diff between the two embedded objects
func generateDiff(existingObj, updatedObj *unstructured.Unstructured) (string, error) {
	// Marshal YAML to []byte and parse object names for logging
	existingYAML, err := yaml.Marshal(existingObj.Object)
	if err != nil {
		return "", fmt.Errorf("failed to marshal existing object to YAML for diff: %w", err)
	}

	name := existingObj.GetName()

	if existingObj.GetNamespace() != "" {
		name = existingObj.GetNamespace() + "/" + name
	}

	updatedYAML, err := yaml.Marshal(updatedObj.Object)
	if err != nil {
		return "", fmt.Errorf("failed to marshal updated object to YAML for diff: %w", err)
	}

	// Set the diffing configuration
	// See https://pkg.go.dev/github.com/pmezard/go-difflib/difflib#UnifiedDiff
	unifiedDiff := difflib.UnifiedDiff{
		A:        difflib.SplitLines(string(existingYAML)),
		FromFile: name + " : existing",
		B:        difflib.SplitLines(string(updatedYAML)),
		ToFile:   name + " : updated",
		Context:  5,
	}

	// Generate and return the diff
	diff, err := difflib.GetUnifiedDiffString(unifiedDiff)
	if err != nil {
		return "", fmt.Errorf("failed to generate diff: %w", err)
	}

	splitDiff := strings.Split(diff, "\n")
	// Keep a maxmium of 50 lines of diff + 3 lines for the header
	if len(splitDiff) > 53 {
		diff = fmt.Sprintf(
			"# Truncated: showing 50/%d diff lines:\n%s", len(splitDiff), strings.Join(splitDiff[:53], "\n"),
		)
	}

	return diff, nil
}
