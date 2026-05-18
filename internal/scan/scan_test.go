package scan

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/skills-lock/skil-lock/internal/model"
)

// mkSkill is a tiny helper that writes a SKILL.md plus optional bundled
// files under root/<runtime>/skills/<name>/.
func mkSkill(t *testing.T, root, runtimeDir, name, frontmatter, body string, files map[string]string) {
	t.Helper()
	dir := filepath.Join(root, runtimeDir, "skills", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\n" + frontmatter + "---\n" + body
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	for rel, contents := range files {
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRepo_FindsClaudeAndCodexSkills(t *testing.T) {
	root := t.TempDir()

	mkSkill(t, root, ".claude", "pdf-extractor",
		"name: pdf-extractor\nversion: 1.2.0\nallowed-tools: [Bash, Read, Write]\n",
		"# Extract\n\n```bash\npdftotext input.pdf out.txt\ncurl https://example.com/sample.pdf -o ./input/sample.pdf\n```\n",
		map[string]string{
			"scripts/extract.sh": "#!/usr/bin/env bash\npdftotext \"$1\" \"$2\"\n",
		},
	)
	mkSkill(t, root, ".codex", "release-notes",
		"name: release-notes\nversion: 0.3.1\n",
		"# Notes\n\n```sh\ngh release create v1\n```\n",
		nil,
	)

	rep, err := Repo(root)
	if err != nil {
		t.Fatalf("Repo: %v", err)
	}
	if len(rep.Errors) != 0 {
		t.Errorf("unexpected scan errors: %+v", rep.Errors)
	}
	if len(rep.Skills) != 2 {
		t.Fatalf("expected 2 skills, got %d (%+v)", len(rep.Skills), rep.Skills)
	}

	byName := map[string]Result{}
	for _, s := range rep.Skills {
		byName[s.Identity.Name] = s
	}

	pdf := byName["pdf-extractor"]
	if pdf.Identity.Runtime != model.RuntimeClaude {
		t.Errorf("pdf runtime: %q", pdf.Identity.Runtime)
	}
	if !strings.HasSuffix(pdf.Identity.SourcePath, ".claude/skills/pdf-extractor/SKILL.md") {
		t.Errorf("pdf source path: %q", pdf.Identity.SourcePath)
	}
	if !contains(pdf.Behavior.ShellCommands, "pdftotext") {
		t.Errorf("pdf shell missing pdftotext: %v", pdf.Behavior.ShellCommands)
	}
	if !contains(pdf.Behavior.NetworkURLs, "https://example.com/sample.pdf") {
		t.Errorf("pdf URLs missing example.com: %v", pdf.Behavior.NetworkURLs)
	}
	if !contains(pdf.Behavior.BundledScripts, "scripts/extract.sh") {
		t.Errorf("pdf bundled scripts: %v", pdf.Behavior.BundledScripts)
	}

	rel := byName["release-notes"]
	if rel.Identity.Runtime != model.RuntimeCodex {
		t.Errorf("rel runtime: %q", rel.Identity.Runtime)
	}
	if !contains(rel.Behavior.ShellCommands, "gh") {
		t.Errorf("rel shell: %v", rel.Behavior.ShellCommands)
	}
}

func TestRepo_RecordsParseErrorsButKeepsGoing(t *testing.T) {
	root := t.TempDir()

	// One good skill.
	mkSkill(t, root, ".claude", "ok",
		"name: ok\nversion: 1.0.0\n",
		"# ok\n",
		nil,
	)
	// One malformed skill (missing version).
	mkSkill(t, root, ".claude", "broken",
		"name: broken\n",
		"# broken\n",
		nil,
	)

	rep, err := Repo(root)
	if err != nil {
		t.Fatalf("Repo: %v", err)
	}
	if len(rep.Skills) != 1 || rep.Skills[0].Identity.Name != "ok" {
		t.Errorf("good skill not returned: %+v", rep.Skills)
	}
	if len(rep.Errors) != 1 {
		t.Fatalf("expected 1 error, got %+v", rep.Errors)
	}
	if !strings.Contains(rep.Errors[0].Path, "broken") {
		t.Errorf("error should reference broken skill: %+v", rep.Errors[0])
	}
}

func TestRepo_EmptyRepoNoError(t *testing.T) {
	rep, err := Repo(t.TempDir())
	if err != nil {
		t.Fatalf("Repo: %v", err)
	}
	if len(rep.Skills) != 0 || len(rep.Errors) != 0 {
		t.Errorf("expected empty report: %+v", rep)
	}
}

func TestRepo_ReturnsErrorOnNonExistentPath(t *testing.T) {
	_, err := Repo(filepath.Join(t.TempDir(), "does-not-exist"))
	if err == nil {
		t.Fatal("expected error for missing path")
	}
}

func TestInventories_SummarisesCounts(t *testing.T) {
	r := Report{
		Skills: []Result{
			{
				Identity: model.Identity{Name: "x", Runtime: model.RuntimeClaude, Version: "1.0"},
				Behavior: model.Behavior{
					ShellCommands: []string{"a", "b"},
					NetworkURLs:   []string{"https://x"},
					FileReads:     []string{"r1"},
					FileWrites:    []string{"w1", "w2"},
				},
			},
		},
	}
	inv := Inventories(r)
	want := []Inventory{
		{
			Name: "x", Runtime: model.RuntimeClaude, Version: "1.0",
			NumShell: 2, NumURLs: 1, NumPaths: 3,
		},
	}
	if !reflect.DeepEqual(inv, want) {
		t.Errorf("Inventories: want %+v\ngot  %+v", want, inv)
	}
}

func TestRepo_BehaviorSlicesAreNonNil(t *testing.T) {
	root := t.TempDir()
	mkSkill(t, root, ".claude", "empty",
		"name: empty\nversion: 1.0.0\n",
		"# Nothing here.\n",
		nil,
	)
	rep, err := Repo(root)
	if err != nil {
		t.Fatalf("Repo: %v", err)
	}
	if len(rep.Skills) != 1 {
		t.Fatalf("want 1 skill, got %d", len(rep.Skills))
	}
	b := rep.Skills[0].Behavior
	if b.ShellCommands == nil || b.NetworkURLs == nil || b.FileReads == nil ||
		b.FileWrites == nil || b.AllowedTools == nil || b.BundledScripts == nil {
		t.Errorf("all behavior slices must be initialised non-nil: %+v", b)
	}
}

func contains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
