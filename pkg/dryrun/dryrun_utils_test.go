// Copyright Contributors to the Open Cluster Management project

package dryrun

import (
	"bytes"
	"embed"
	"io"
	"os"
	"path"
	"strings"
	"testing"

	"github.com/pmezard/go-difflib/difflib"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

//go:embed utils_testdata/*
var testUtilsFiles embed.FS

func TestStatusObj(t *testing.T) {
	entries, err := testUtilsFiles.ReadDir("utils_testdata")
	if err != nil {
		t.Fatal(err)
	}

	noTestsRun := true

	for _, entry := range entries {
		testName, found := strings.CutPrefix(entry.Name(), "test_")
		if !found || !entry.IsDir() {
			continue
		}

		t.Run(testName, compareStatusStableOutputTest(path.Join("utils_testdata", entry.Name())))

		noTestsRun = false
	}

	if noTestsRun {
		t.Fatal("No CLI tests were run")
	}
}

func compareStatusStableOutputTest(scenarioPath string) func(t *testing.T) {
	return func(t *testing.T) {
		cmd := &cobra.Command{}
		testOut := bytes.Buffer{}
		cmd.SetOut(&testOut)

		err := cmd.Execute()
		if err != nil {
			t.Fatal(err)
		}

		inputStatus := getFileMap(t, scenarioPath, "input.yaml")
		actualStatus := getFileMap(t, scenarioPath, "actual.yaml")

		if strings.HasSuffix(scenarioPath, "no_color") {
			compareStatus(cmd, inputStatus, actualStatus, true)
		} else {
			compareStatus(cmd, inputStatus, actualStatus, false)
		}

		wanted, err := testUtilsFiles.ReadFile(path.Join(scenarioPath, "output.txt"))
		if err != nil {
			t.Fatal(err)
		}

		got := testOut.Bytes()

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

func getFileMap(t *testing.T, scenarioPath, fileName string) map[string]interface{} {
	t.Helper()

	reader, err := os.Open(path.Join(scenarioPath, fileName))
	if err != nil {
		t.Fatal(err)
	}

	fileBytes, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}

	result := map[string]interface{}{}

	if err := yaml.Unmarshal(fileBytes, &result); err != nil {
		t.Fatal(err)
	}

	return result
}
