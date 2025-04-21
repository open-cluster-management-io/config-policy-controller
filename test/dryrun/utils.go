// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package dryrun

import (
	"bytes"
	"embed"
	"errors"
	"path"
	"strings"
	"testing"

	"github.com/pmezard/go-difflib/difflib"

	"open-cluster-management.io/config-policy-controller/pkg/dryrun"
)

func Run(testFiles embed.FS, filePath string) func(t *testing.T) {
	return func(t *testing.T) {
		t.Helper()

		scenarioFiles, err := testFiles.ReadDir(filePath)
		if err != nil {
			t.Fatal(err)
		}

		d := dryrun.DryRunner{}
		cmd := d.GetCmd()
		testout := bytes.Buffer{}

		cmd.SetOut(&testout)

		args := []string{"--no-colors"}

		for _, f := range scenarioFiles {
			if strings.HasPrefix(f.Name(), "input") {
				args = append(args, path.Join(filePath, f.Name()))
			}
		}

		cmd.SetArgs(args)

		err = cmd.Flags().Set("policy", path.Join(filePath, "policy.yaml"))
		if err != nil {
			t.Fatal(err)
		}

		desiredStatusPath := path.Join(filePath, "desired_status.yaml")
		if dryrun.PathExists(desiredStatusPath) {
			err = cmd.Flags().Set("desired-status", desiredStatusPath)
			if err != nil {
				t.Fatal(err)
			}
		}

		err = cmd.Execute()
		if err != nil && !errors.Is(err, dryrun.ErrNonCompliant) {
			t.Fatal(err)
		}

		wanted, err := testFiles.ReadFile(path.Join(filePath, "output.txt"))
		if err != nil {
			t.Fatal(err)
		}

		got := testout.Bytes()

		if !bytes.Equal(wanted, got) {
			if testing.Verbose() {
				t.Log("\nWanted:\n" + string(wanted))
				t.Log("\nGot:\n" + string(got))
			}

			unifiedDiff := difflib.UnifiedDiff{
				A:        difflib.SplitLines(string(wanted)),
				FromFile: path.Join(filePath, "output.txt"),
				B:        difflib.SplitLines(string(got)),
				ToFile:   "actual output",
				Context:  5,
			}

			diff, err := difflib.GetUnifiedDiffString(unifiedDiff)
			if err != nil {
				t.Fatal(err)
			}

			t.Fatalf("Mismatch in resolved output; diff:\n%v", diff)
		}
	}
}
