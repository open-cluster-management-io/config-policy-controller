// Copyright Contributors to the Open Cluster Management project

package dryrun

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	policyv1 "open-cluster-management.io/config-policy-controller/api/v1"
)

const (
	Green  = "\033[32m"
	Red    = "\033[31m"
	Yellow = "\033[33m"
	Bold   = "\033[1m"
	Reset  = "\033[0m"
)

// Apply color if enabled
func applyColor(input, color string, noColors bool) string {
	if noColors {
		return input
	}

	return color + input + Reset
}

func boldColor(input string, noColors bool) string {
	return applyColor(input, Bold, noColors)
}

func successColor(input string, noColors bool) string {
	return applyColor(input, Green, noColors)
}

func errorColor(input string, noColors bool) string {
	return applyColor(input, Red, noColors)
}

func warningColor(input string, noColors bool) string {
	return applyColor(input, Yellow, noColors)
}

func compareStatus(cmd *cobra.Command,
	inputStatus map[string]interface{},
	resultStatus map[string]interface{},
	noColor bool,
) {
	cmd.Println("# Status compare:")

	isStatusMatch := compareStatusObj(cmd, true, "", inputStatus, resultStatus, noColor)

	if isStatusMatch {
		cmd.Println(successColor(boldColor(" Expected status matches the actual status", noColor), noColor))
	} else {
		cmd.Println(errorColor(boldColor(" Expected status does not match the actual status", noColor), noColor))
	}
}

// compareStatusObj compares the expected inputMap against the actual resultMap
// and prints success or error messages for mismatches. It supports nested structures,
// including maps and arrays, by recursively comparing their contents. The function
// updates isStatusMatch based on whether all expected values match the actual values.
func compareStatusObj(cmd *cobra.Command, isStatusMatch bool, parentPath string,
	inputMap, resultMap map[string]interface{}, noColor bool,
) bool {
	// Sort keys for consistency
	keys := make([]string, 0, len(inputMap))
	for k := range inputMap {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	for _, key := range keys {
		actualValue, exist := resultMap[key]
		value := inputMap[key]

		if !exist {
			errorNoKeyPrint(cmd, parentPath, key, noColor)

			isStatusMatch = false

			continue
		}

		path := parentPath + "." + key

		switch typedV := value.(type) {
		case string, int, int64, float64, bool:
			v := fmt.Sprintf("%v", typedV)

			av := fmt.Sprintf("%v", actualValue)

			if v != av {
				errorMatchPrint(cmd, path, v, av, noColor)

				isStatusMatch = false
			} else {
				successMatchPrint(cmd, path, v, av, noColor)
			}

		case []interface{}:
			// Handle array of objects and non-object arrays
			actualArray, ok := actualValue.([]interface{})
			if !ok {
				notArrayPrint(cmd, path, key, noColor)

				isStatusMatch = false

				continue
			}

			isStatusMatch = handleArrayInterface(cmd, typedV, actualArray, path, isStatusMatch, noColor)

		case map[string]interface{}:
			mapObj, ok := convertMapStringKey(typedV).(map[string]interface{})
			if !ok {
				failedParsePrint(cmd, path, noColor)

				isStatusMatch = false

				continue
			}

			mapActualObj, ok := convertMapStringKey(actualValue).(map[string]interface{})
			if !ok {
				failedTypeMissMatch(cmd, path, reflect.TypeOf(actualValue).String(), noColor)

				isStatusMatch = false

				continue
			}

			// Recursive call for nested maps
			isStatusMatch = compareStatusObj(cmd, isStatusMatch, path, mapObj, mapActualObj, noColor)

		default:
			typeOfTypeV := reflect.TypeOf(typedV).String()
			typeOfActual := reflect.TypeOf(actualValue).String()

			if typeOfTypeV != typeOfActual {
				failedTypeMissMatch(cmd, path, typeOfActual, noColor)

				continue
			}

			cmd.Println(errorColor(fmt.Sprintf("Unsupported type %T at path '%s' for key '%s'",
				typedV, path, key), noColor))
		}
	}

	return isStatusMatch
}

func handleArrayInterface(cmd *cobra.Command,
	typedV, actualArray []interface{}, path string,
	isStatusMatch bool, noColor bool,
) bool {
	switch typedV[0].(type) {
	case map[interface{}]interface{}, map[string]interface{}:
		allMatch := true
		// Check all input status are match
		for i, obj := range typedV {
			elementMatch := false

			mapObj, ok := convertMapStringKey(obj).(map[string]interface{})
			if !ok {
				failedParsePrint(cmd, path, noColor)

				continue
			}

			for _, actualObj := range actualArray {
				mapActualObj, ok := convertMapStringKey(actualObj).(map[string]interface{})
				if !ok {
					failedTypeMissMatch(cmd, path, reflect.TypeOf(actualObj).String(), noColor)

					continue
				}

				// If an matched element is found in the actual status, break the current loop.
				objPath := fmt.Sprintf("%s[%d]", path, i)
				if compareStatusObj(cmd, true, objPath, mapObj, mapActualObj, noColor) {
					elementMatch = true

					break
				}
			}

			if !elementMatch {
				allMatch = false

				errorMatchAnyArrayPrint(cmd, fmt.Sprintf("%s[%d]", path, i), noColor)

				break
			}

			successMatchArrayPrint(cmd, fmt.Sprintf("%s[%d]", path, i), noColor)
		}

		if !allMatch {
			errorMatchArrayPrint(cmd, path, noColor)

			isStatusMatch = false
		} else {
			successMatchArrayPrint(cmd, path, noColor)
		}
	default:
		// Simple comparison of arrays eg []string
		if reflect.DeepEqual(typedV, actualArray) {
			successMatchPrint(cmd, path, typedV, actualArray, noColor)
		} else {
			errorMatchPrint(cmd, path, typedV, actualArray, noColor)

			isStatusMatch = false
		}
	}

	return isStatusMatch
}

func failedTypeMissMatch(cmd *cobra.Command, path, typeOfActual string, noColors bool) {
	cmd.Println(errorColor(fmt.Sprintf(
		"The input status field '%s' is not the right type - expected '%s'", path, typeOfActual), noColors))
}

func failedParsePrint(cmd *cobra.Command, path string, noColors bool) {
	cmd.Println(errorColor("failed to parse, skip "+path, noColors))
}

func notArrayPrint(cmd *cobra.Command, from, path string, noColors bool) {
	cmd.Println(warningColor(fmt.Sprintf("skip %s, %s is not array,", path, from), noColors))
}

// skipPrintRegex matches deeper items like .relatedObjects[1].conditions which should not be printed
var skipPrintRegex = regexp.MustCompile(`\.\w+\[\d+\]\.\w`)

func errorNoKeyPrint(cmd *cobra.Command, path, key string, noColors bool) {
	if !skipPrintRegex.MatchString(path) {
		cmd.Println(errorColor(fmt.Sprintf("Key '%s' not found in result at path '%s'", key, path), noColors))
	}
}

func errorMatchPrint(cmd *cobra.Command, path string, inputVal, actualVal interface{}, noColors bool) {
	if !skipPrintRegex.MatchString(path) {
		cmd.Println(errorColor(fmt.Sprintf("%s: '%v' does not match '%v'", path, inputVal, actualVal), noColors))
	}
}

func errorMatchAnyArrayPrint(cmd *cobra.Command, path string, noColors bool) {
	if !skipPrintRegex.MatchString(path) {
		cmd.Println(errorColor(path+" does not match any elements", noColors))
	}
}

func errorMatchArrayPrint(cmd *cobra.Command, path string, noColors bool) {
	if !skipPrintRegex.MatchString(path) {
		cmd.Println(errorColor(path+" does not match the elements", noColors))
	}
}

func successMatchArrayPrint(cmd *cobra.Command, path string, noColors bool) {
	if !skipPrintRegex.MatchString(path) {
		cmd.Println(successColor((path + " matches"), noColors))
	}
}

func successMatchPrint(cmd *cobra.Command, path string, inputVal, actualVal interface{}, noColors bool) {
	if !skipPrintRegex.MatchString(path) {
		cmd.Println(successColor(fmt.Sprintf("%s: '%v' does match '%v'", path, inputVal, actualVal), noColors))
	}
}

func convertMapStringKey(i interface{}) interface{} {
	switch v := i.(type) {
	case map[interface{}]interface{}:
		newMap := make(map[string]interface{})

		for key, value := range v {
			strKey := fmt.Sprintf("%v", key) // Convert key to string
			newMap[strKey] = value
		}

		return newMap

	default:
		return i
	}
}

func toMap(obj policyv1.ConfigurationPolicyStatus) map[string]interface{} {
	cfgpolBytes, err := json.Marshal(obj)
	if err != nil {
		return nil
	}

	var statusMap map[string]interface{}

	if err := json.Unmarshal(cfgpolBytes, &statusMap); err != nil {
		return nil
	}

	return statusMap
}

func PathExists(path string) bool {
	f, err := os.Open(path) // #nosec G304 -- files accessed here are on the user's local system
	if err != nil {
		return false
	}

	err = f.Close()

	return err == nil
}

func addColorToDiff(bareDiff string, noColor bool) string {
	lines := strings.Split(bareDiff, "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---") {
			lines[i] = errorColor(line, noColor)

			continue
		}

		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			lines[i] = successColor(line, noColor)

			continue
		}
	}

	return strings.Join(lines, "\n")
}
