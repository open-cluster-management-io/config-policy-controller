// Copyright (c) 2025 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package e2e

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v3"

	"open-cluster-management.io/config-policy-controller/pkg/dryrun"
	"open-cluster-management.io/config-policy-controller/test/utils"
)

const (
	case46TestDataPath           = "../dryrun/"
	policyYAML                   = "policy.yaml"
	resourceYamlPrefix           = "input"
	namespaceYamlPrefix          = "input_ns"
	errOutputFile                = "error.txt"
	defaultOutputFile            = "output.txt"
	outputFromClusterFile        = "output_from_cluster.txt"
	desiredStatusFile            = "desired_status.yaml"
	desiredStatusFromClusterFile = "desired_status_from_cluster.yaml"
)

// persistentNamespaces are namespaces that should not be deleted after this test case
// because they persist across test suites or are system namespaces
var persistentNamespaces = []string{
	"managed",
	"default",
}

var untrackedMetadata = []string{
	"resourceVersion:",
	"generatedName:",
	"creationTimestamp:",
	"deletionTimestamp:",
	"selfLink:",
	"uid:",
	"metadata.kubernetes.io/metadata.name:",
	"kubernetes.io/metadata.name:",
}

var _ = Describe("Testing dryrun CLI", Ordered, func() {
	var testNamespaces []string

	BeforeAll(func() {
		// Setup test namespaces before creating any resources
		testNamespaces, err := findInputNamespaces(case46TestDataPath)
		Expect(err).ToNot(HaveOccurred())

		for _, nsFilePath := range testNamespaces {
			utils.Kubectl("apply", "-f", nsFilePath)
		}
	})

	AfterAll(func() {
		for _, nsFilePath := range testNamespaces {
			if shouldDelete, err := containsManagedNamespace(nsFilePath); shouldDelete && err == nil {
				utils.KubectlDelete("-f", nsFilePath, "--wait")
			}
		}
	})

	testScenarios := []struct {
		description string
		path        string
	}{
		{"Should handle context variables", "context_vars"},
		{"Should handle diffs", "diff"},
		{"Should handle pod annotations", "kind_field"},
		{"Should handle missing fields", "missing"},
		{"Should handle multiple objects", "multiple"},
		{"Should handle no name objects", "no_name"},
		{"Should handle object selector scenarios", "no_name/with_object_selector"},
		{"Should handle cluster-scoped no name", "no_name_clusterscope"},
		{"Should handle no namespace", "no_ns"},
		{"Should handle namespace selector", "ns_selector"},
		{"Should handle object template raw", "obj_tmpl_raw"},
	}

	for _, scenario := range testScenarios {
		It(scenario.description, func() {
			testDryRunScenarios(scenario.path)
		})
	}
})

// testDryRunScenarios runs all test scenarios in a given test category
func testDryRunScenarios(testCategory string) {
	testDirs, err := findTestDirectories(filepath.Join(case46TestDataPath, testCategory))
	Expect(err).ToNot(HaveOccurred())

	for _, testDir := range testDirs {
		By("Testing " + testCategory + "/" + testDir)
		testDryrunCommand(testCategory, testDir)
	}
}

// testDryrunCommand runs a dryrun test scenario
func testDryrunCommand(testCategory, testDir string) {
	testPath := filepath.Join(case46TestDataPath, testCategory, testDir)
	inputResourcePaths, desiredStatusPath, outputPath := findTestFiles(testPath)

	for _, resource := range inputResourcePaths {
		By("Applying input file " + resource)
		utils.Kubectl("apply", "-f", resource)
	}

	defer func() {
		for _, resource := range inputResourcePaths {
			By("Deleting input file " + resource)
			utils.KubectlDelete("-f", resource, "--wait")
		}
	}()

	expectedBytes, err := os.ReadFile(outputPath)
	Expect(err).ToNot(HaveOccurred())

	expectedOutput := string(expectedBytes)
	shouldFail := strings.HasSuffix(outputPath, errOutputFile)

	if shouldFail {
		// Match dry run error whitespace formatting for error files
		expectedOutput = strings.TrimSpace(strings.ReplaceAll(expectedOutput, "\n", " "))
	}

	By("Running dryrun command")
	Eventually(func() string {
		var output bytes.Buffer
		cmd := (&dryrun.DryRunner{}).GetCmd()
		cmd.SetOut(&output)

		args := []string{"--from-cluster", "--policy", filepath.Join(testPath, policyYAML), "--no-colors"}
		if desiredStatusPath != "" {
			args = append(args, "--desired-status", desiredStatusPath)
		}

		cmd.SetArgs(args)
		err := cmd.Execute()
		actualOutput := output.String()

		if shouldFail {
			Expect(err).To(HaveOccurred())

			return fmt.Sprintf("Error: %v", err.Error())
		} else if err != nil {
			Expect(err).To(MatchError(dryrun.ErrNonCompliant))

			return normalizeDiffOutput(actualOutput)
		}

		return actualOutput
	}, 5, 1).Should(Equal(expectedOutput))
}

// findTestFiles finds all test files in the given test path
func findTestFiles(testPath string) (inputResourcePaths []string, desiredStatusPath string, outputPath string) {
	files, err := os.ReadDir(testPath)
	if err != nil {
		return inputResourcePaths, desiredStatusPath, outputPath
	}

	for _, file := range files {
		name := file.Name()
		fullPath := filepath.Join(testPath, name)

		// Handle input YAML files (skip namespace files)
		if strings.HasPrefix(name, "input") && strings.HasSuffix(name, ".yaml") {
			if !strings.HasPrefix(name, namespaceYamlPrefix) {
				inputResourcePaths = append(inputResourcePaths, fullPath)
			}

			continue
		}

		// Handle output files (prefer cluster files when present)
		switch name {
		case errOutputFile, outputFromClusterFile:
			outputPath = fullPath
		case defaultOutputFile:
			if outputPath == "" {
				outputPath = fullPath
			}
		case desiredStatusFromClusterFile:
			desiredStatusPath = fullPath
		case desiredStatusFile:
			if desiredStatusPath == "" {
				desiredStatusPath = fullPath
			}
		}
	}

	return inputResourcePaths, desiredStatusPath, outputPath
}

// findInputNamespaces finds all namespace YAML files in the given root path
func findInputNamespaces(rootPath string) (nsFiles []string, err error) {
	err = filepath.WalkDir(rootPath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if !d.IsDir() && strings.HasPrefix(d.Name(), namespaceYamlPrefix) && strings.HasSuffix(d.Name(), ".yaml") {
			nsFiles = append(nsFiles, path)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return nsFiles, nil
}

// normalizeDiffOutput removes metadata fields that should not be
// compared with diff output
func normalizeDiffOutput(dryrunOutput string) string {
	lines := strings.Split(dryrunOutput, "\n")
	var result []string

	for _, line := range lines {
		content := strings.TrimSpace(line)
		shouldKeep := true

		for _, field := range untrackedMetadata {
			if strings.Contains(content, field) {
				shouldKeep = false

				break
			}
		}

		if shouldKeep {
			result = append(result, line)
		}
	}

	return strings.Join(result, "\n")
}

// containsManagedNamespace checks if a namespace YAML file contains any managed namespaces
func containsManagedNamespace(nsFilePath string) (bool, error) {
	content, err := os.ReadFile(nsFilePath)
	if err != nil {
		return false, err
	}

	for _, doc := range strings.Split(string(content), "---") {
		if doc = strings.TrimSpace(doc); doc == "" {
			continue
		}

		var obj map[string]any
		if yaml.Unmarshal([]byte(doc), &obj) != nil {
			continue
		}

		if obj["kind"] != "Namespace" {
			continue
		}

		metadata, ok := obj["metadata"].(map[string]any)
		if !ok {
			continue
		}

		name, ok := metadata["name"].(string)
		if ok && slices.Contains(persistentNamespaces, name) {
			return true, nil
		}
	}

	return false, nil
}

// findTestDirectories returns test directory names that contain required test files
func findTestDirectories(testPath string) ([]string, error) {
	entries, err := os.ReadDir(testPath)
	if err != nil {
		return nil, err
	}

	var testDirs []string

	for _, entry := range entries {
		if entry.IsDir() {
			if isTestDir, _ := isTestDirectory(filepath.Join(testPath, entry.Name())); isTestDir {
				testDirs = append(testDirs, entry.Name())
			}
		}
	}

	return testDirs, nil
}

// isTestDirectory checks if a directory contains policy.yaml and an output file
func isTestDirectory(dirPath string) (bool, error) {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return false, err
	}

	hasPolicyYaml, hasOutputFile := false, false

	for _, entry := range entries {
		if !entry.IsDir() {
			name := entry.Name()
			if name == policyYAML {
				hasPolicyYaml = true
			}

			if name == defaultOutputFile || name == outputFromClusterFile || name == errOutputFile {
				hasOutputFile = true
			}

			if hasPolicyYaml && hasOutputFile {
				return true, nil // Early return when both found
			}
		}
	}

	return hasPolicyYaml && hasOutputFile, nil
}
