package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunHelpExitsSuccessfully(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := run([]string{"--help"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("unexpected exit code %d", exitCode)
	}

	if !strings.Contains(stdout.String(), "Usage of git-branch-prune:") {
		t.Fatalf("expected help output, got %q", stdout.String())
	}

	if stderr.Len() != 0 {
		t.Fatalf("expected empty stderr, got %q", stderr.String())
	}
}
