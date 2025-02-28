package dryruntest

import (
	"embed"
	"testing"

	"open-cluster-management.io/config-policy-controller/test/dryrun"
)

//go:embed musthave_mixed_noncompliant/*
var musthaveMixedNoncompliant embed.FS

func TestMusthaveMixedNonCompliant(t *testing.T) {
	t.Run("Test only selected and incorrect objects are marked as violations",
		dryrun.Run(musthaveMixedNoncompliant, "musthave_mixed_noncompliant"))
}

//go:embed mustnothave_mixed_noncompliant/*
var mustnothaveMixedNoncompliant embed.FS

func TestMustnothaveMixedNonCompliant(t *testing.T) {
	t.Run("Test only selected and matched objects are marked as violations",
		dryrun.Run(mustnothaveMixedNoncompliant, "mustnothave_mixed_noncompliant"))
}
