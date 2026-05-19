package diff

import (
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/skills-lock/skil-lock/internal/model"
)

// withFixedNow pins nowFunc to a deterministic time for the duration of a
// test so RenderMarkdown's snippet timestamps are reproducible.
func withFixedNow(t *testing.T, ts time.Time) {
	t.Helper()
	orig := nowFunc
	nowFunc = func() time.Time { return ts }
	t.Cleanup(func() { nowFunc = orig })
}

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

func TestRenderMarkdown_ReasonColumnSurfacesNotes(t *testing.T) {
	withFixedNow(t, time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC))
	d := model.Diff{
		BaselinePath: "old.lock",
		CurrentPath:  "cur.lock",
		Entries: []model.DiffEntry{
			{Skill: "pdf", Capability: "file_reads", Change: model.ChangeAdded,
				Value: "./.env", Severity: model.SeverityHigh, Note: "matches protected_paths"},
			{Skill: "pdf", Capability: "bundled_scripts", Change: model.ChangeAdded,
				Value: "scripts/run.sh", Severity: model.SeverityLow},
		},
	}
	md := RenderMarkdown(d, "")
	// Header has the new column.
	if !strings.Contains(md, "| Skill | Capability | Change | Detail | Reason |") {
		t.Errorf("header missing Reason column:\n%s", md)
	}
	// Row with a populated Note shows it in the Reason cell.
	if !strings.Contains(md, "| `./.env` | matches protected_paths |") {
		t.Errorf("note not rendered in its own column:\n%s", md)
	}
	// Row with no Note shows the em-dash placeholder.
	if !strings.Contains(md, "| `scripts/run.sh` | — |") {
		t.Errorf("missing em-dash placeholder for empty Note:\n%s", md)
	}
}

func TestRenderMarkdown_ApprovalsSnippetForBlockingDelta(t *testing.T) {
	withFixedNow(t, time.Date(2026, 5, 19, 12, 30, 45, 0, time.UTC))
	d := model.Diff{
		BaselinePath: "old.lock",
		CurrentPath:  "cur.lock",
		Entries: []model.DiffEntry{
			{Skill: "pdf-extractor", Capability: "shell_commands", Change: model.ChangeAdded,
				Value: "curl", Severity: model.SeverityHigh, Note: "matches require_approval"},
			{Skill: "pdf-extractor", Capability: "network_urls", Change: model.ChangeAdded,
				Value: "https://api.openai.com/v1/x", Severity: model.SeverityMedium},
			// Removed entries should never appear in the snippet — removing
			// behavior is security-positive, no approval needed.
			{Skill: "pdf-extractor", Capability: "shell_commands", Change: model.ChangeRemoved,
				Value: "wget", Severity: model.SeverityInfo},
		},
	}
	md := RenderMarkdown(d, "BLOCK: 2 of 3 entries at severity >= medium")

	if !strings.Contains(md, "**To approve, append to `.skil-lock-approvals.yaml`:**") {
		t.Fatalf("snippet header missing:\n%s", md)
	}
	if !strings.Contains(md, "```yaml") {
		t.Fatalf("snippet fence missing:\n%s", md)
	}
	if !strings.Contains(md, `added_shell_command: "curl"`) {
		t.Errorf("shell-add delta key wrong:\n%s", md)
	}
	if !strings.Contains(md, `added_network_url: "https://api.openai.com/v1/x"`) {
		t.Errorf("network-add delta key wrong:\n%s", md)
	}
	if strings.Contains(md, `removed_shell_command`) {
		t.Errorf("removals must not appear in snippet:\n%s", md)
	}
	if !strings.Contains(md, `reviewed_at: "2026-05-19T12:30:45Z"`) {
		t.Errorf("timestamp not pinned via nowFunc:\n%s", md)
	}
}

func TestRenderMarkdown_NoSnippetWhenNothingBlocking(t *testing.T) {
	withFixedNow(t, time.Date(2026, 5, 19, 0, 0, 0, 0, time.UTC))
	d := model.Diff{
		BaselinePath: "old.lock",
		CurrentPath:  "cur.lock",
		Entries: []model.DiffEntry{
			{Skill: "x", Capability: "allowed_tools", Change: model.ChangeAdded,
				Value: "Read", Severity: model.SeverityLow},
			{Skill: "x", Capability: "version", Change: model.ChangeModified,
				OldValue: "1.0.0", Value: "1.0.1", Severity: model.SeverityInfo},
		},
	}
	md := RenderMarkdown(d, "WARN")
	if strings.Contains(md, "To approve") || strings.Contains(md, "```yaml") {
		t.Errorf("snippet should not render below the threshold:\n%s", md)
	}
}

func TestRenderMarkdown_SnippetParsesAsYAML(t *testing.T) {
	withFixedNow(t, time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC))
	d := model.Diff{
		BaselinePath: "old.lock",
		CurrentPath:  "cur.lock",
		Entries: []model.DiffEntry{
			{Skill: "code-review", Capability: "shell_commands", Change: model.ChangeAdded,
				Value: `awk '/foo/ { print $1 }'`, Severity: model.SeverityHigh},
			{Skill: "pdf", Capability: "file_writes", Change: model.ChangeAdded,
				Value: `./output/"with quotes".txt`, Severity: model.SeverityMedium,
				Note: "matches require_approval"},
		},
	}
	md := RenderMarkdown(d, "BLOCK")

	// Extract the fenced yaml block. The snippet is always at the end of
	// the rendered output, so anchor on the opening fence.
	start := strings.Index(md, "```yaml\n")
	if start < 0 {
		t.Fatalf("no yaml fence in output:\n%s", md)
	}
	body := md[start+len("```yaml\n"):]
	end := strings.Index(body, "```")
	if end < 0 {
		t.Fatalf("no closing fence:\n%s", md)
	}
	yamlBody := body[:end]

	var parsed struct {
		SchemaVersion string `yaml:"schema_version"`
		Approvals     []struct {
			Skill      string            `yaml:"skill"`
			Delta      map[string]string `yaml:"delta"`
			Reviewer   string            `yaml:"reviewer"`
			ReviewedAt string            `yaml:"reviewed_at"`
			Reason     string            `yaml:"reason"`
		} `yaml:"approvals"`
	}
	if err := yaml.Unmarshal([]byte(yamlBody), &parsed); err != nil {
		t.Fatalf("snippet failed to parse as YAML: %v\nsnippet:\n%s", err, yamlBody)
	}
	if parsed.SchemaVersion != "0.1" {
		t.Errorf("schema_version: want 0.1, got %q", parsed.SchemaVersion)
	}
	if len(parsed.Approvals) != 2 {
		t.Fatalf("want 2 approval entries, got %d: %+v", len(parsed.Approvals), parsed.Approvals)
	}
	// Round-trip preserves the embedded single quotes + double quotes.
	first := parsed.Approvals[0]
	if first.Skill != "code-review" || first.Delta["added_shell_command"] != `awk '/foo/ { print $1 }'` {
		t.Errorf("awk delta did not round-trip cleanly: %+v", first)
	}
	second := parsed.Approvals[1]
	if second.Delta["added_file_write"] != `./output/"with quotes".txt` {
		t.Errorf("quoted path did not round-trip cleanly: %+v", second)
	}
}

func TestDeltaKey(t *testing.T) {
	cases := []struct {
		cap, want string
		change    model.ChangeType
	}{
		{"shell_commands", "added_shell_command", model.ChangeAdded},
		{"network_urls", "added_network_url", model.ChangeAdded},
		{"file_reads", "added_file_read", model.ChangeAdded},
		{"file_writes", "added_file_write", model.ChangeAdded},
		{"allowed_tools", "added_allowed_tool", model.ChangeAdded},
		{"bundled_scripts", "added_bundled_script", model.ChangeAdded},
		{"shell_commands", "removed_shell_command", model.ChangeRemoved},
		{"version", "modified_version", model.ChangeModified},
	}
	for _, c := range cases {
		if got := DeltaKey(c.cap, c.change); got != c.want {
			t.Errorf("DeltaKey(%q,%v) = %q, want %q", c.cap, c.change, got, c.want)
		}
	}
}
