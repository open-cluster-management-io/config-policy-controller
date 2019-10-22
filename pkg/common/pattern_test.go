// Licensed Materials - Property of IBM
// (c) Copyright IBM Corporation 2018, 2019. All Rights Reserved.
// Note to U.S. Government Users Restricted Rights:
// Use, duplication or disclosure restricted by GSA ADP Schedule
// Contract with IBM Corp.
package common

import (
	"reflect"
	"sort"
	"testing"
)

func TestIfMatch(t *testing.T) {

	tt := []struct {
		name    string
		include []string
		exclude []string
		result  bool
	}{
		{"test", []string{"*"}, []string{""}, true},
		{"test", []string{"t*"}, []string{"sss"}, true},
		{"test", []string{"*st"}, []string{"test2"}, true},
		{"test", []string{"test"}, []string{"atest"}, true},
		//{"test", []string{"t*t"}, []string{"eee"}, true},
		{"test", []string{"test1"}, []string{""}, false},
		{" test", []string{"test"}, []string{"test"}, false},
		{" test", []string{"test"}, []string{"test"}, false},
		{"test", []string{"test"}, []string{"te*"}, false},
		{"test", []string{"test"}, []string{"*st"}, false},
		{"test", []string{"test"}, []string{"*"}, false},
		{"test", []string{"test1", "te*"}, []string{""}, true},
		{"test", []string{"test1", "te*"}, []string{"tr*", "teft"}, true},
	}
	for _, row := range tt {
		result := IfMatch(row.name, row.include, row.exclude)
		if row.result != result {
			t.Errorf("IfMach returned %t instead of %t for name = %s, indelude = %v, exclude = %v\n",
				result, row.result, row.name, row.include, row.exclude)
		}
	}
}

func TestFindPattern(t *testing.T) {
	list := []string{"Hello-World", "World-Hello", "Hello-World-Hello", "nothing", "exact"}

	//testing PREFIX
	actualResult := FindPattern("Hello*", list)
	expectedResult := []string{"Hello-World", "Hello-World-Hello"}

	if !reflect.DeepEqual(actualResult, expectedResult) {
		t.Fatalf("Expected %s but got %s", expectedResult, actualResult)
	}

	//testing SUFFIX
	actualResult = FindPattern("*Hello", list)
	expectedResult = []string{"World-Hello", "Hello-World-Hello"}

	if !reflect.DeepEqual(actualResult, expectedResult) {
		t.Fatalf("Expected %s but got %s", expectedResult, actualResult)
	}

	//testing if it CONTAINS the pattern
	actualResult = FindPattern("*Hello*", list)
	expectedResult = []string{"Hello-World", "World-Hello", "Hello-World-Hello"}

	if !reflect.DeepEqual(actualResult, expectedResult) {
		t.Fatalf("Expected %s but got %s", expectedResult, actualResult)
	}

	//testing if it does NOT contain the pattern
	actualResult = FindPattern("*xxx*", list)
	expectedResult = []string{}

	if !reflect.DeepEqual(actualResult, expectedResult) {
		t.Fatalf("Expected %s but got %s", expectedResult, actualResult)
	}

	//testing if it  contains the EXACT pattern
	actualResult = FindPattern("Hello-World", list)
	expectedResult = []string{"Hello-World"}

	if !reflect.DeepEqual(actualResult, expectedResult) {
		t.Fatalf("Expected %s but got %s", expectedResult, actualResult)
	}

	//testing corner case
	actualResult = FindPattern("*ku*be", list)
	expectedResult = []string{}

	if !reflect.DeepEqual(actualResult, expectedResult) {
		t.Fatalf("Expected %s but got %s", expectedResult, actualResult)
	}
}

func TestDeduplicateItems(t *testing.T) {

	included := []string{"Hello-World", "World-Hello", "Hello-World-Hello", "nothing", "exact"}
	excluded := []string{"Hello-World", "Hello-World-Hello", "exact"}

	actualResult := DeduplicateItems(included, excluded)
	expectedResult := []string{"World-Hello", "nothing"}
	if len(actualResult) != len(expectedResult) {
		t.Fatalf("Expected %s but got %s", expectedResult, actualResult)
	} else {
		sort.Strings(expectedResult)
		sort.Strings(actualResult)
		if !reflect.DeepEqual(actualResult, expectedResult) {
			t.Fatalf("Expected %s but got %s", expectedResult, actualResult)
		}
	}
}
