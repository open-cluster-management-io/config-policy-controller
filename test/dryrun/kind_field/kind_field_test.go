// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package dryruntest

import (
	"embed"
	"testing"

	"open-cluster-management.io/config-policy-controller/test/dryrun"
)

//go:embed pod_annotation_match/*
var podAnnotationMatch embed.FS

func TestPodAnnotationMatch(t *testing.T) {
	t.Run("Test pod annotation match", dryrun.Run(podAnnotationMatch))
}

//go:embed pod_annotation_mismatch/*
var podAnnotationMismatch embed.FS

func TestPodAnnotationMisMatch(t *testing.T) {
	t.Run("Test pod annotation mismatch", dryrun.Run(podAnnotationMismatch))
}

//go:embed pod_label_invalid/*
var podLabelInvalid embed.FS

func TestPodLabelInvalid(t *testing.T) {
	t.Run("Test invalid pod label in policy", dryrun.Run(podLabelInvalid))
}

//go:embed pod_annotation_invalid/*
var podAnnotationInvalid embed.FS

func TestPodAnnotationInvalid(t *testing.T) {
	t.Run("Test invalid pod annotation in policy", dryrun.Run(podAnnotationInvalid))
}
