package lockfile

import (
	"bytes"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/skills-lock/skil-lock/internal/model"
)

// -update regenerates testdata/skills.lock.golden from the current Save
// output. Run after intentional schema changes:
//
//	go test ./internal/lockfile -run TestSave_MatchesGolden -update
var updateGolden = flag.Bool("update", false, "regenerate golden files in testdata/")

// mockupFixture builds the three-skill lockfile that MOCKUPS.md §1
// renders by hand. The golden file is the byte-exact yaml.v3 output for
// this exact input; if the mockup and golden diverge, MOCKUPS.md is the
// aspirational draft and the golden is the wire-format contract.
func mockupFixture() model.Lockfile {
	t := time.Date(2026, 5, 16, 9, 14, 8, 0, time.UTC)
	lf := model.NewLockfile("skil-lock 0.1.0", t)
	lf.Skills["code-review"] = model.LockEntry{
		Runtime:     model.RuntimeClaude,
		SourcePath:  ".claude/skills/code-review/SKILL.md",
		Version:     "1.4.0",
		ContentHash: "sha256:7a3e9b1c4d2f8a6e0b5c1d9e2f3a4b5c6d7e8f9a0b1c2d3e4f5a6b7c8d9e0f1a",
		Behavior: model.Behavior{
			ShellCommands:  []string{"git"},
			NetworkURLs:    []string{},
			FileReads:      []string{"**/*.go", "**/*.py", "**/*.ts"},
			FileWrites:     []string{},
			AllowedTools:   []string{"Bash", "Read", "Grep"},
			BundledScripts: []string{"scripts/review.sh"},
		},
	}
	lf.Skills["pdf-extractor"] = model.LockEntry{
		Runtime:     model.RuntimeClaude,
		SourcePath:  ".claude/skills/pdf-extractor/SKILL.md",
		Version:     "1.2.0",
		ContentHash: "sha256:a1b2c3d4e5f6789012345678901234567890abcdef1234567890abcdef123456",
		Behavior: model.Behavior{
			ShellCommands:  []string{"pdftotext"},
			NetworkURLs:    []string{},
			FileReads:      []string{"./input/*.pdf"},
			FileWrites:     []string{"./output/*.txt"},
			AllowedTools:   []string{"Bash", "Read", "Write"},
			BundledScripts: []string{"scripts/extract.sh"},
		},
	}
	lf.Skills["release-notes"] = model.LockEntry{
		Runtime:     model.RuntimeCodex,
		SourcePath:  ".codex/skills/release-notes/SKILL.md",
		Version:     "0.3.1",
		ContentHash: "sha256:c0ffee0123456789abcdef0123456789abcdef0123456789abcdef0123456789",
		Behavior: model.Behavior{
			ShellCommands: []string{"git", "gh"},
			NetworkURLs: []string{
				"https://api.github.com/repos/*/releases",
				"https://api.github.com/repos/*/compare/*",
			},
			FileReads:      []string{"CHANGELOG.md"},
			FileWrites:     []string{"release-notes-*.md"},
			AllowedTools:   []string{"Bash", "Read", "Write"},
			BundledScripts: []string{"scripts/render-notes.sh"},
		},
	}
	return lf
}

func saveToTemp(t *testing.T, lf model.Lockfile) (path string, bytes []byte) {
	t.Helper()
	path = filepath.Join(t.TempDir(), "skills.lock")
	if err := Save(lf, path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	bytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	return path, bytes
}

func TestSave_DeterministicAcrossRuns(t *testing.T) {
	lf := mockupFixture()
	_, a := saveToTemp(t, lf)
	_, b := saveToTemp(t, lf)
	if !bytes.Equal(a, b) {
		t.Fatalf("Save is not deterministic; outputs differ (lens %d vs %d)", len(a), len(b))
	}
}

func TestSaveLoad_RoundTripIsByteStable(t *testing.T) {
	original := mockupFixture()
	path1, first := saveToTemp(t, original)

	reloaded, err := Load(path1)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	_, second := saveToTemp(t, reloaded)

	if !bytes.Equal(first, second) {
		t.Fatalf("round-trip not byte-stable.\nfirst (%d B):\n%s\nsecond (%d B):\n%s",
			len(first), first, len(second), second)
	}
}

func TestSave_MatchesGolden(t *testing.T) {
	_, got := saveToTemp(t, mockupFixture())
	goldenPath := filepath.Join("testdata", "skills.lock.golden")

	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatalf("mkdir testdata: %v", err)
		}
		if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		t.Logf("wrote %s (%d B)", goldenPath, len(got))
		return
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden %s: %v (regenerate with: go test ./internal/lockfile -run TestSave_MatchesGolden -update)", goldenPath, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("Save output does not match %s.\n--- got (%d B) ---\n%s\n--- want (%d B) ---\n%s",
			goldenPath, len(got), got, len(want), want)
	}
}

func TestLoad_RejectsFutureSchemaVersion(t *testing.T) {
	content := []byte(`schema_version: "0.99"
generated_at: 2026-05-16T09:14:08Z
generated_by: skil-lock 99.0.0
skills: {}
`)
	path := filepath.Join(t.TempDir(), "future.lock")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error loading future schema_version, got nil")
	}
	if !errors.Is(err, ErrUnsupportedSchema) {
		t.Errorf("expected errors.Is(err, ErrUnsupportedSchema), got %v", err)
	}
	if !strings.Contains(err.Error(), "0.99") {
		t.Errorf("error should include observed version %q; got %q", "0.99", err)
	}
}

func TestLoad_RejectsEmptySchemaVersion(t *testing.T) {
	content := []byte(`skills: {}
`)
	path := filepath.Join(t.TempDir(), "empty.lock")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if !errors.Is(err, ErrUnsupportedSchema) {
		t.Errorf("expected ErrUnsupportedSchema for empty schema_version; got %v", err)
	}
}

func TestLoad_FileNotFound(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "does-not-exist.lock"))
	if err == nil {
		t.Fatal("expected error loading missing file, got nil")
	}
	if errors.Is(err, ErrUnsupportedSchema) {
		t.Errorf("should not be ErrUnsupportedSchema; got %v", err)
	}
}

func TestLoad_MalformedYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.lock")
	if err := os.WriteFile(path, []byte("not: valid: yaml: [oops"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected parse error for malformed YAML, got nil")
	}
	if !strings.Contains(err.Error(), "parse") {
		t.Errorf("expected error to mention parse; got %q", err)
	}
}

func TestSave_RejectsWrongSchemaVersion(t *testing.T) {
	lf := model.NewLockfile("skil-lock test", time.Date(2026, 5, 16, 9, 14, 8, 0, time.UTC))
	lf.SchemaVersion = "0.99"
	err := Save(lf, filepath.Join(t.TempDir(), "wrong.lock"))
	if !errors.Is(err, ErrUnsupportedSchema) {
		t.Errorf("Save should refuse a wrong schema_version with ErrUnsupportedSchema; got %v", err)
	}
}

func TestSave_HeaderIsPresent(t *testing.T) {
	_, got := saveToTemp(t, mockupFixture())
	if !bytes.HasPrefix(got, []byte(Header)) {
		t.Errorf("Save output should start with the SkilLock comment header; got first 80 bytes:\n%s",
			got[:80])
	}
}

func TestSaveLoad_EmptyLockfile(t *testing.T) {
	original := model.NewLockfile("skil-lock test", time.Date(2026, 5, 16, 9, 14, 8, 0, time.UTC))
	path, _ := saveToTemp(t, original)
	reloaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load empty: %v", err)
	}
	if len(reloaded.Skills) != 0 {
		t.Errorf("expected empty Skills, got %d entries", len(reloaded.Skills))
	}
	if reloaded.SchemaVersion != model.SchemaVersionV01 {
		t.Errorf("schema version mismatch: %q vs %q", reloaded.SchemaVersion, model.SchemaVersionV01)
	}
	if !reloaded.GeneratedAt.Equal(original.GeneratedAt) {
		t.Errorf("generated_at round-trip lost the instant: %v vs %v",
			reloaded.GeneratedAt, original.GeneratedAt)
	}
}
