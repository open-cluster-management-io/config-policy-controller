// Copyright (c) 2020 Red Hat, Inc.

package common

import (
	"math"
	"strings"

	"github.com/golang/glog"
	policyv1 "github.com/open-cluster-management/config-policy-controller/pkg/apis/policy/v1"
)

// IfMatch check matches
func IfMatch(name string, included, excluded []string) bool {
	all := []string{name}
	return len(MatchNames(all, included, excluded)) > 0
}

// MatchNames matches names
func MatchNames(all, included, excluded []string) []string {
	glog.V(6).Infof("MatchNames all = %v, included = %v, excluded = %v", all, included, excluded)
	//list of included
	includedNames := []string{}
	for _, value := range included {
		found := FindPattern(value, all)
		includedNames = append(includedNames, found...)
	}
	glog.V(6).Infof("MatchNames all = %v includedNames = %v", all, includedNames)
	//then get the list of excluded
	excludedNames := []string{}
	for _, value := range excluded {
		found := FindPattern(value, all)
		excludedNames = append(excludedNames, found...)
	}
	glog.V(6).Infof("MatchNames all = %v excludedNames = %v", all, excludedNames)
	//then get the list of deduplicated
	finalList := DeduplicateItems(includedNames, excludedNames)
	glog.V(6).Infof("MatchNames all = %v return  = %v", all, finalList)
	return finalList
}

// FindPattern finds patterns
func FindPattern(pattern string, list []string) (result []string) {
	//if pattern = "*" => all namespaces are included
	if pattern == "*" {
		return list
	}
	found := []string{}
	//if the pattern has NO "*" => do an exact search
	if !strings.Contains(pattern, "*") {
		for _, value := range list {
			if pattern == value {
				found = append(found, value)
			}
		}
		return found
	}

	// if there is a * something, we need to figure out where: it can be a leading, ending or leading and ending
	if strings.LastIndex(pattern, "*") == 0 {
		// check for has suffix of pattern - *
		substring := strings.TrimPrefix(pattern, "*")
		for _, value := range list {
			if strings.HasSuffix(value, substring) {
				found = append(found, value)
			}
		}
		return found
	}
	if strings.Index(pattern, "*") == len(pattern)-1 {
		// check for has prefix of pattern - *
		substring := strings.TrimSuffix(pattern, "*")
		for _, value := range list {
			if strings.HasPrefix(value, substring) {
				found = append(found, value)
			}
		}
		return found
	}

	if strings.LastIndex(pattern, "*") == len(pattern)-1 && strings.Index(pattern, "*") == 0 {
		substring := strings.TrimPrefix(pattern, "*")
		substring = strings.TrimSuffix(substring, "*")
		for _, value := range list {
			if strings.Contains(value, substring) {
				found = append(found, value)
			}
		}
		return found
	}

	return found
}

// DeduplicateItems does the dedup
func DeduplicateItems(included []string, excluded []string) (res []string) {
	encountered := map[string]bool{}
	result := []string{}
	for _, inc := range included {
		encountered[inc] = true
	}
	for _, excl := range excluded {
		if encountered[excl] {
			delete(encountered, excl)
		}
	}
	for key := range encountered {
		result = append(result, key)
	}
	return result
}

//ToFixed returns a float with a certain precision
func ToFixed(num float64, precision int) float64 {
	output := math.Pow(10, float64(precision))
	return float64(Round(num*output)) / output
}

//Round rounds the value
func Round(num float64) int {
	return int(num + math.Copysign(0.5, num))
}

// ExtractNamespaceLabel to find out the cluster-namespace from the label
func ExtractNamespaceLabel(instance *policyv1.ConfigurationPolicy) string {
	if instance.ObjectMeta.Labels == nil {
		return ""
	}
	if _, ok := instance.ObjectMeta.Labels["cluster-namespace"]; ok {
		return instance.ObjectMeta.Labels["cluster-namespace"]
	}
	return ""
}
