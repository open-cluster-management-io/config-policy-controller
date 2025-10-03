// Copyright (c) 2025 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package e2e

import (
	"bytes"
	"errors"
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

type dryrunTestFiles struct {
	testPath          string
	inputNamespaces   []string
	inputResources    []string
	desiredStatusPath string
	outputPath        string
}

var _ = Describe("Testing dryrun CLI", Serial, func() {
	const case46TestDataPath = "../dryrun/"

	testFilesCache, err := findDryrunTestFiles(case46TestDataPath)
	Expect(err).ToNot(HaveOccurred())

	describeTableArgs := []any{func(testPath string) {
		files := testFilesCache[testPath]

		DeferCleanup(func() {
			for _, resource := range files.inputResources {
				By("Deleting input file " + resource)
				utils.KubectlDelete("-f", resource, "--wait")
			}

			for _, nsFilePath := range files.inputNamespaces {
				isManagedNs, err := containsManagedNamespace(nsFilePath)
				if isManagedNs || err != nil {
					continue
				}

				By("Deleting namespace from " + nsFilePath)
				utils.KubectlDelete("-f", nsFilePath, "--wait")
			}
		})

		Eventually(func(g Gomega) {
			for _, nsFilePath := range files.inputNamespaces {
				isManagedNs, err := containsManagedNamespace(nsFilePath)
				if isManagedNs || err != nil {
					continue
				}

				By("Creating namespace from " + nsFilePath)
				utils.Kubectl("apply", "-f", nsFilePath)
			}

			for _, resource := range files.inputResources {
				By("Applying input file " + resource)
				utils.Kubectl("apply", "-f", resource)
			}

			verifyDryrunOutput(g, files)
		}, defaultTimeoutSeconds, 1).Should(Succeed())
	}}

	// Generate Entry items dynamically for each test
	for testPath := range testFilesCache {
		relPath := strings.TrimPrefix(testPath, case46TestDataPath)
		relPath = strings.TrimPrefix(relPath, "/")
		describeTableArgs = append(describeTableArgs, Entry("Should handle "+relPath, testPath))
	}

	DescribeTable("When reading cluster resources with dryrun CLI", describeTableArgs...)
})

// findDryrunTestFiles discovers all test directories and their files in a single traversal
func findDryrunTestFiles(rootPath string) (map[string]dryrunTestFiles, error) {
	const (
		policyYAML                   = "policy.yaml"
		errOutputFile                = "error.txt"
		namespaceYamlPrefix          = "input_ns"
		defaultOutputFile            = "output.txt"
		outputFromClusterFile        = "output_from_cluster.txt"
		desiredStatusFile            = "desired_status.yaml"
		desiredStatusFromClusterFile = "desired_status_from_cluster.yaml"
	)

	result := make(map[string]dryrunTestFiles)

	err := filepath.WalkDir(rootPath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Skip if policy file not found
		if d.IsDir() || d.Name() != policyYAML {
			return nil
		}

		testDir := filepath.Dir(path)

		files, err := os.ReadDir(testDir)
		if err != nil {
			return err
		}

		var inputNamespaces []string
		var inputResources []string
		var desiredStatusPath string
		var outputPath string

		// Categorize all files in the test directory
		for _, file := range files {
			name := file.Name()
			fullPath := filepath.Join(testDir, name)

			// Handle input YAML files
			if strings.HasPrefix(name, "input") && strings.HasSuffix(name, ".yaml") {
				if strings.HasPrefix(name, namespaceYamlPrefix) {
					inputNamespaces = append(inputNamespaces, fullPath)
				} else {
					inputResources = append(inputResources, fullPath)
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

		result[testDir] = dryrunTestFiles{
			testPath:          testDir,
			inputNamespaces:   inputNamespaces,
			inputResources:    inputResources,
			desiredStatusPath: desiredStatusPath,
			outputPath:        outputPath,
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return result, nil
}

// verifyDryrunOutput executes the dryrun command and compares actual vs expected output
func verifyDryrunOutput(g Gomega, files dryrunTestFiles) {
	GinkgoHelper()

	const (
		policyYAML    = "policy.yaml"
		errOutputFile = "error.txt"
	)

	expectedBytes, err := os.ReadFile(files.outputPath)
	g.Expect(err).ToNot(HaveOccurred())

	expectedOutput := string(expectedBytes)
	wantedErr := filepath.Base(files.outputPath) == errOutputFile

	if wantedErr {
		// Match dry run error whitespace formatting for error files
		expectedOutput = strings.TrimSpace(strings.ReplaceAll(expectedOutput, "\n", " "))
	}

	By("Running dryrun command")
	var output bytes.Buffer

	cmd := (&dryrun.DryRunner{}).GetCmd()
	cmd.SetOut(&output)

	args := []string{"--from-cluster", "--policy", filepath.Join(files.testPath, policyYAML), "--no-colors"}
	if files.desiredStatusPath != "" {
		args = append(args, "--desired-status", files.desiredStatusPath)
	}

	cmd.SetArgs(args)
	err = cmd.Execute()
	actualOutput := output.String()

	if wantedErr {
		g.Expect(err).To(HaveOccurred())

		actualOutput = fmt.Sprintf("Error: %v", err.Error())
	} else if err != nil {
		g.Expect(err).To(MatchError(dryrun.ErrNonCompliant))

		actualOutput = normalizeDiffOutput(actualOutput)
	}

	g.Expect(actualOutput).To(Equal(expectedOutput))
}

// containsManagedNamespace checks if a namespace YAML file contains any managed namespaces
func containsManagedNamespace(nsFilePath string) (bool, error) {
	// managedNamespaces are namespaces that should not be deleted after this test case
	// because they persist across test suites or are system namespaces
	managedNamespaces := []string{
		"managed",
		"default",
	}

	hasPersistent := false
	err := listNamespacesInFile(nsFilePath, func(name string) error {
		if slices.Contains(managedNamespaces, name) {
			hasPersistent = true

			return errors.New("found persistent namespace")
		}

		return nil
	})

	// Ignore the early exit error
	if err != nil && hasPersistent {
		return true, nil
	}

	return hasPersistent, err
}

// normalizeDiffOutput removes metadata fields that should not be
// compared with diff output
func normalizeDiffOutput(dryrunOutput string) string {
	untrackedMetadata := []string{
		"resourceVersion:",
		"generatedName:",
		"creationTimestamp:",
		"deletionTimestamp:",
		"selfLink:",
		"uid:",
		"metadata.kubernetes.io/metadata.name:",
		"kubernetes.io/metadata.name:",
		"deletionGracePeriodSeconds:",
	}

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

// listNamespacesInFile reads a YAML file and calls the provided function for each namespace found
func listNamespacesInFile(nsFilePath string, fn func(name string) error) error {
	content, err := os.ReadFile(nsFilePath)
	if err != nil {
		return err
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
		if ok {
			if err := fn(name); err != nil {
				return err
			}
		}
	}

	return nil
}
