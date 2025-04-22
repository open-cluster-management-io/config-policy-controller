// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package dryrun

import (
	"bytes"
	"embed"
	"errors"
	"io/fs"
	"strings"
	"testing"

	"github.com/pmezard/go-difflib/difflib"

	"open-cluster-management.io/config-policy-controller/pkg/dryrun"
)

func Run(testFiles embed.FS) func(t *testing.T) {
	return func(t *testing.T) {
		t.Helper()

		d := dryrun.DryRunner{}
		cmd := d.GetCmd()
		testout := bytes.Buffer{}
		wanted := []byte{}
		wantedFile := ""

		cmd.SetOut(&testout)

		args := []string{"--no-colors"}

		err := fs.WalkDir(testFiles, ".", func(path string, file fs.DirEntry, err error) error {
			if err != nil {
				return err
			}

			if file.IsDir() {
				return nil
			}

			fileName := file.Name()

			switch fileName {
			case "policy.yaml":
				err := cmd.Flags().Set("policy", path)
				if err != nil {
					return err
				}

			case "desired_status.yaml":
				err = cmd.Flags().Set("desired-status", path)
				if err != nil {
					t.Fatal(err)
				}

			case "output.txt":
				wantedFile = path

				wanted, err = testFiles.ReadFile(path)
				if err != nil {
					return err
				}

			default:
				if strings.HasPrefix(fileName, "input") {
					args = append(args, path)
				}
			}

			return nil
		})
		if err != nil {
			t.Fatal(err)
		}

		cmd.SetArgs(args)

		err = cmd.Execute()
		if err != nil && !errors.Is(err, dryrun.ErrNonCompliant) {
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
				FromFile: wantedFile,
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
