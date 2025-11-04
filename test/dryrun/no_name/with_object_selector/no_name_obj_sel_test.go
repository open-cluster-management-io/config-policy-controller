package dryruntest

import (
	"embed"
	"testing"

	"open-cluster-management.io/config-policy-controller/test/dryrun"
)

var (
	//go:embed musthave_mixed_noncompliant/*
	musthaveMixedNoncompliant embed.FS
	//go:embed mustnothave_mixed_noncompliant/*
	mustnothaveMixedNoncompliant embed.FS
	//go:embed mustnothave_unmatched/*
	mustnothaveUnmatched embed.FS

	testCases = map[string]embed.FS{
		"Test only selected and incorrect objects are marked as violations": musthaveMixedNoncompliant,
		"Test only selected and matched objects are marked as violations":   mustnothaveMixedNoncompliant,
		"Test no matched objects for mustnothave is compliant":              mustnothaveUnmatched,
	}
)

func TestNoNameObjSelector(t *testing.T) {
	for name, testFiles := range testCases {
		t.Run(name, dryrun.Run(testFiles))
	}
}
