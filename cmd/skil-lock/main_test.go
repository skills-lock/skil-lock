package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestVersionCommand(t *testing.T) {
	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"version"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("version command failed: %v", err)
	}

	got := out.String()
	for _, want := range []string{"skil-lock", "commit", "built"} {
		if !strings.Contains(got, want) {
			t.Errorf("version output missing %q; got %q", want, got)
		}
	}
}

func TestRootHelpShowsBranding(t *testing.T) {
	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("help failed: %v", err)
	}

	got := out.String()
	for _, want := range []string{"SkilLock", "skills.lock", "Claude Code"} {
		if !strings.Contains(got, want) {
			t.Errorf("root help missing %q; got %q", want, got)
		}
	}
}
