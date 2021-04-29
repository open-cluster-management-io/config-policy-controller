// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package templates

import (
	"errors"
	"strings"
	"testing"
)

func TestFromSecret(t *testing.T) {
	testcases := []struct {
		inputNs        string
		inputCMname    string
		inputKey       string
		expectedResult string
		expectedErr    error
	}{
		{"testns", "testsecret", "secretkey1", "secretkey1Val", nil},                                            //green-path test
		{"testns", "testsecret", "secretkey2", "secretkey2Val", nil},                                            //green-path test
		{"testns", "idontexist", "secretkey1", "secretkey2Val", errors.New("secrets \"idontexist\" not found")}, //error : nonexistant secret
		{"testns", "testsecret", "blah", "", nil},                                                               //error : nonexistant key
	}

	for _, test := range testcases {

		val, err := fromSecret(test.inputNs, test.inputCMname, test.inputKey)

		if err != nil {
			if test.expectedErr == nil {
				t.Fatalf(err.Error())
			}
			if !strings.EqualFold(test.expectedErr.Error(), err.Error()) {
				t.Fatalf("expected err: %s got err: %s", test.expectedErr, err)
			}
		} else {
			if val != base64encode(test.expectedResult) {
				t.Fatalf("expected : %s , got : %s", base64encode(test.expectedResult), val)
			}
		}
	}
}

func TestFromConfigMap(t *testing.T) {
	testcases := []struct {
		inputNs        string
		inputCMname    string
		inputKey       string
		expectedResult string
		expectedErr    error
	}{
		{"testns", "testconfigmap", "cmkey1", "cmkey1Val", nil},
		{"testns", "testconfigmap", "cmkey2", "cmkey2Val", nil},
		{"testns", "idontexist", "cmkey1", "cmkey1Val", errors.New("configmaps \"idontexist\" not found")},
		{"testns", "testconfigmap", "idontexist", "", nil},
	}

	for _, test := range testcases {

		val, err := fromConfigMap(test.inputNs, test.inputCMname, test.inputKey)

		if err != nil {
			if test.expectedErr == nil {
				t.Fatalf(err.Error())
			}
			if !strings.EqualFold(test.expectedErr.Error(), err.Error()) {
				t.Fatalf("expected err: %s got err: %s", test.expectedErr, err)
			}
		} else {
			if val != test.expectedResult {
				t.Fatalf("expected : %s , got : %s", test.expectedResult, val)
			}
		}
	}
}
