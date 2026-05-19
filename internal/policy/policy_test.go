package policy

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/skills-lock/skil-lock/internal/model"
)

func TestLoad_HappyPath(t *testing.T) {
	pol, err := Load("testdata/policy.yaml")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := model.Policy{
		Mode:            model.PolicyModeBlock,
		ProtectedPaths:  []string{".env", "secrets/**", "**/*.pem"},
		RequireApproval: []string{"shell_commands", "network_urls"},
		AllowedDomains:  []string{"api.openai.com", "*.github.com"},
	}
	if !reflect.DeepEqual(pol, want) {
		t.Fatalf("Load mismatch:\n got=%#v\nwant=%#v", pol, want)
	}
}

func TestLoad_MissingFile(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "does-not-exist.yaml"))
	if !errors.Is(err, ErrMissingPolicy) {
		t.Fatalf("want ErrMissingPolicy, got %v", err)
	}
}

func TestLoad_EmptyFileYieldsDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".skil-lock.yaml")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	pol, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(pol, Default()) {
		t.Fatalf("empty file should yield Default(); got %#v", pol)
	}
}

func TestLoad_ModeOmittedDefaultsToWarn(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".skil-lock.yaml")
	body := []byte("protected_paths:\n  - .env\n")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	pol, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if pol.Mode != model.PolicyModeWarn {
		t.Fatalf("want mode warn, got %q", pol.Mode)
	}
}

func TestLoad_InvalidMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".skil-lock.yaml")
	body := []byte("mode: nuke\n")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	_, err := Load(path)
	if !errors.Is(err, ErrInvalidMode) {
		t.Fatalf("want ErrInvalidMode, got %v", err)
	}
}

func TestLoad_InvalidRequireApproval(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".skil-lock.yaml")
	body := []byte("mode: warn\nrequire_approval:\n  - shell_commands\n  - definitely_not_a_category\n")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	_, err := Load(path)
	if !errors.Is(err, ErrInvalidRequireApproval) {
		t.Fatalf("want ErrInvalidRequireApproval, got %v", err)
	}
}

func TestLoad_UnknownTopLevelKeyRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".skil-lock.yaml")
	body := []byte("mode: warn\nallow_domains:\n  - example.com\n") // typo: allow vs allowed
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	_, err := Load(path)
	if err == nil {
		t.Fatal("want error from KnownFields, got nil")
	}
	if errors.Is(err, ErrInvalidMode) || errors.Is(err, ErrInvalidRequireApproval) {
		t.Fatalf("wrong error class: %v", err)
	}
}

func TestLoad_MalformedYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".skil-lock.yaml")
	if err := os.WriteFile(path, []byte("mode: [unterminated\n"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	_, err := Load(path)
	if err == nil {
		t.Fatal("want parse error, got nil")
	}
}

func TestDefault(t *testing.T) {
	d := Default()
	if d.Mode != model.PolicyModeWarn {
		t.Errorf("Default().Mode = %q, want %q", d.Mode, model.PolicyModeWarn)
	}
	if len(d.ProtectedPaths) != 0 || len(d.RequireApproval) != 0 || len(d.AllowedDomains) != 0 {
		t.Errorf("Default() should have empty slices, got %#v", d)
	}
}

func TestValidRequireApproval_AllBehaviorCategories(t *testing.T) {
	for _, cat := range []string{"shell_commands", "network_urls", "file_reads", "file_writes"} {
		if _, ok := validRequireApproval[cat]; !ok {
			t.Errorf("category %q should be valid for require_approval", cat)
		}
	}
	for _, cat := range []string{"allowed_tools", "bundled_scripts"} {
		if _, ok := validRequireApproval[cat]; ok {
			t.Errorf("category %q should NOT be valid (metadata, not capability)", cat)
		}
	}
}
