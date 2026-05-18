package diff

import (
	"strings"
	"testing"
	"time"

	"github.com/skills-lock/skil-lock/internal/model"
)

func emptyLockfile() model.Lockfile {
	return model.NewLockfile("test", time.Date(2026, 5, 18, 0, 0, 0, 0, time.UTC))
}

func TestCompare_NewSkillIsAllAdditions(t *testing.T) {
	old := emptyLockfile()
	cur := emptyLockfile()
	cur.Skills["x"] = model.LockEntry{
		Runtime:     model.RuntimeClaude,
		Version:     "1.0.0",
		ContentHash: "sha256:aa",
		Behavior: model.Behavior{
			ShellCommands: []string{"git"},
			NetworkURLs:   []string{},
			FileReads:     []string{},
			FileWrites:    []string{},
			AllowedTools:  []string{"Bash"},
			BundledScripts: []string{},
		},
	}
	d := Compare(old, cur, "old.lock", "cur.lock")

	if len(d.Entries) == 0 {
		t.Fatal("expected entries for new skill")
	}
	for _, e := range d.Entries {
		if e.Skill != "x" || e.Change != model.ChangeAdded {
			t.Errorf("unexpected entry: %+v", e)
		}
	}
	hasShellAdd := false
	for _, e := range d.Entries {
		if e.Capability == "shell_commands" && e.Value == "git" {
			hasShellAdd = true
			if e.Severity != model.SeverityMedium {
				t.Errorf("shell-add severity: want medium, got %q", e.Severity)
			}
		}
	}
	if !hasShellAdd {
		t.Errorf("missing shell_commands=git addition: %+v", d.Entries)
	}
}

func TestCompare_RemovedSkillEmitsRemovedEntries(t *testing.T) {
	old := emptyLockfile()
	old.Skills["x"] = model.LockEntry{
		Version: "1.0.0",
		Behavior: model.Behavior{
			ShellCommands: []string{"git"},
		},
	}
	cur := emptyLockfile()

	d := Compare(old, cur, "old.lock", "cur.lock")
	if len(d.Entries) == 0 {
		t.Fatal("expected removal entries")
	}
	for _, e := range d.Entries {
		if e.Change != model.ChangeRemoved {
			t.Errorf("non-removal in removed-skill diff: %+v", e)
		}
	}
}

func TestCompare_ModifiedSkill_AddAndRemove(t *testing.T) {
	old := emptyLockfile()
	old.Skills["x"] = model.LockEntry{
		Version:     "1.0.0",
		ContentHash: "sha256:aa",
		Behavior: model.Behavior{
			ShellCommands: []string{"git"},
			NetworkURLs:   []string{"https://example.com"},
		},
	}
	cur := emptyLockfile()
	cur.Skills["x"] = model.LockEntry{
		Version:     "1.0.0",
		ContentHash: "sha256:aa",
		Behavior: model.Behavior{
			ShellCommands: []string{"git", "curl"},
			NetworkURLs:   []string{},
		},
	}
	d := Compare(old, cur, "old.lock", "cur.lock")

	var added, removed int
	for _, e := range d.Entries {
		switch e.Change {
		case model.ChangeAdded:
			added++
			if e.Value != "curl" {
				t.Errorf("added value: want curl, got %q", e.Value)
			}
		case model.ChangeRemoved:
			removed++
			if e.Value != "https://example.com" {
				t.Errorf("removed value: want example.com, got %q", e.Value)
			}
		}
	}
	if added != 1 || removed != 1 {
		t.Errorf("want 1 added + 1 removed; got added=%d removed=%d entries=%+v", added, removed, d.Entries)
	}
}

func TestCompare_VersionChange(t *testing.T) {
	old := emptyLockfile()
	old.Skills["x"] = model.LockEntry{Version: "1.0.0", ContentHash: "sha256:aa"}
	cur := emptyLockfile()
	cur.Skills["x"] = model.LockEntry{Version: "1.1.0", ContentHash: "sha256:aa"}

	d := Compare(old, cur, "old", "cur")
	found := false
	for _, e := range d.Entries {
		if e.Capability == "version" && e.Change == model.ChangeModified {
			found = true
			if e.OldValue != "1.0.0" || e.Value != "1.1.0" {
				t.Errorf("version delta: %+v", e)
			}
		}
	}
	if !found {
		t.Errorf("expected version modification entry: %+v", d.Entries)
	}
}

func TestCompare_HashOnlyDriftIsInfo(t *testing.T) {
	old := emptyLockfile()
	old.Skills["x"] = model.LockEntry{Version: "1.0.0", ContentHash: "sha256:aa"}
	cur := emptyLockfile()
	cur.Skills["x"] = model.LockEntry{Version: "1.0.0", ContentHash: "sha256:bb"}

	d := Compare(old, cur, "old", "cur")
	if len(d.Entries) != 1 {
		t.Fatalf("want 1 entry, got %+v", d.Entries)
	}
	e := d.Entries[0]
	if e.Capability != "content_hash" || e.Change != model.ChangeModified {
		t.Errorf("want hash modification; got %+v", e)
	}
	if e.Severity != model.SeverityInfo {
		t.Errorf("hash-only drift should be info; got %q", e.Severity)
	}
}

func TestCompare_IdenticalLockfilesEmptyDiff(t *testing.T) {
	old := emptyLockfile()
	old.Skills["x"] = model.LockEntry{
		Version:     "1.0.0",
		ContentHash: "sha256:aa",
		Behavior:    model.Behavior{ShellCommands: []string{"git"}},
	}
	cur := old
	d := Compare(old, cur, "a", "b")
	if len(d.Entries) != 0 {
		t.Errorf("identical lockfiles should produce empty diff; got %+v", d.Entries)
	}
}

func TestRenderMarkdown_HappyPath(t *testing.T) {
	d := model.Diff{
		BaselinePath: "old.lock",
		CurrentPath:  "cur.lock",
		Entries: []model.DiffEntry{
			{Skill: "x", Capability: "shell_commands", Change: model.ChangeAdded, Value: "curl", Severity: model.SeverityMedium},
			{Skill: "x", Capability: "network_urls", Change: model.ChangeRemoved, Value: "https://old.example"},
			{Skill: "y", Capability: "file_reads", Change: model.ChangeAdded, Value: "./.env", Note: "matches protected_paths"},
		},
	}
	md := RenderMarkdown(d, "blocked")
	if !strings.Contains(md, "capability delta") {
		t.Errorf("missing header: %s", md)
	}
	if !strings.Contains(md, "`curl`") {
		t.Errorf("missing curl: %s", md)
	}
	if !strings.Contains(md, "matches protected_paths") {
		t.Errorf("missing note: %s", md)
	}
	if !strings.Contains(md, "**Verdict:** blocked") {
		t.Errorf("missing verdict: %s", md)
	}
}

func TestRenderMarkdown_NoDeltas(t *testing.T) {
	d := model.Diff{BaselinePath: "old", CurrentPath: "new"}
	md := RenderMarkdown(d, "")
	if !strings.Contains(md, "no capability deltas") {
		t.Errorf("empty diff message wrong: %s", md)
	}
}

func TestCompare_DeterministicOrder(t *testing.T) {
	old := emptyLockfile()
	cur := emptyLockfile()
	cur.Skills["b-second"] = model.LockEntry{
		Behavior: model.Behavior{ShellCommands: []string{"z", "a"}},
	}
	cur.Skills["a-first"] = model.LockEntry{
		Behavior: model.Behavior{NetworkURLs: []string{"https://b", "https://a"}},
	}

	d1 := Compare(old, cur, "old", "cur")
	d2 := Compare(old, cur, "old", "cur")
	if len(d1.Entries) != len(d2.Entries) {
		t.Fatalf("entry counts differ: %d vs %d", len(d1.Entries), len(d2.Entries))
	}
	for i := range d1.Entries {
		if d1.Entries[i] != d2.Entries[i] {
			t.Errorf("entry %d differs: %+v vs %+v", i, d1.Entries[i], d2.Entries[i])
		}
	}
	// First skill should be a-first, capability network_urls, value a before b.
	if d1.Entries[0].Skill != "a-first" || d1.Entries[0].Value != "https://a" {
		t.Errorf("sort order: first entry %+v", d1.Entries[0])
	}
}
