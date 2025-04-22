// Copyright Contributors to the Open Cluster Management project

package dryrun

import (
	"bytes"
	"embed"
	"errors"
	"io/fs"
	"path"
	"strings"
	"testing"

	"github.com/pmezard/go-difflib/difflib"
)

//go:embed testdata/*
var testfiles embed.FS

func TestCLI(t *testing.T) {
	noTestsRun := true

	err := fs.WalkDir(testfiles, ".", func(path string, file fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if file.IsDir() && strings.HasPrefix(file.Name(), "test_") {
			testName, _ := strings.CutPrefix(file.Name(), "test_")
			noTestsRun = false

			t.Run(testName, cliTest(path))
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if noTestsRun {
		t.Fatal("No CLI tests were run")
	}
}

func cliTest(scenarioPath string) func(t *testing.T) {
	return func(t *testing.T) {
		d := DryRunner{}
		cmd := d.GetCmd()
		testout := bytes.Buffer{}

		cmd.SetOut(&testout)

		scenarioFiles, err := testfiles.ReadDir(scenarioPath)
		if err != nil {
			t.Fatal(err)
		}

		args := []string{}

		for _, f := range scenarioFiles {
			if !strings.HasPrefix(f.Name(), "input_") {
				continue
			}

			if f.Name() == "input_stdin.yaml" {
				inp, err := testfiles.Open(path.Join(scenarioPath, f.Name()))
				if err != nil {
					t.Fatal(err)
				}

				cmd.SetIn(inp)

				args = append(args, "-")

				continue
			}

			args = append(args, path.Join(scenarioPath, f.Name()))
		}

		cmd.SetArgs(args)

		err = cmd.Flags().Set("policy", path.Join(scenarioPath, "policy.yaml"))
		if err != nil {
			t.Fatal(err)
		}

		desiredStatusPath := path.Join(scenarioPath, "desired_status.yaml")
		if PathExists(desiredStatusPath) {
			err = cmd.Flags().Set("desired-status", desiredStatusPath)
			if err != nil {
				t.Fatal(err)
			}
		}

		mappingsPath := path.Join(scenarioPath, "mappings.yaml")
		if PathExists(mappingsPath) {
			err = cmd.Flags().Set("mappings-file", mappingsPath)
			if err != nil {
				t.Fatal(err)
			}
		}

		err = cmd.Flags().Set("full-diffs", "true")
		if err != nil {
			t.Fatal(err)
		}

		err = cmd.Execute()
		if err != nil && !errors.Is(err, ErrNonCompliant) {
			t.Fatal(err)
		}

		wanted, err := testfiles.ReadFile(path.Join(scenarioPath, "output.txt"))
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
				FromFile: path.Join(scenarioPath, "output.txt"),
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
