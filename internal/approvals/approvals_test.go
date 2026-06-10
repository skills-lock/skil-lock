package approvals

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/skills-lock/skil-lock/internal/model"
)

const validApprovalsFile = `schema_version: "0.1"
approvals:
  - skill: pdf-extractor
    delta:
      added_shell_command: "curl"
    reviewer: "reviewer@example.com"
    reviewed_at: "2026-05-19T13:30:00Z"
    reason: "Needed for fetching PDF test fixtures."
  - skill: code-review
    delta:
      added_file_read: "./.env"
    reviewer: "alice@example.com"
    reviewed_at: "2026-05-19T13:31:00Z"
    reason: "Reads dotenv to know which CI it is running in."
    expires_at: "2026-08-13T00:00:00Z"
`

func writeTemp(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	full := filepath.Join(dir, name)
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", full, err)
	}
	return full
}

func TestLoad_HappyPath(t *testing.T) {
	path := writeTemp(t, "approvals.yaml", validApprovalsFile)
	as, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(as) != 2 {
		t.Fatalf("want 2 approvals, got %d", len(as))
	}
	if as[0].Skill != "pdf-extractor" || as[0].Delta["added_shell_command"] != "curl" {
		t.Errorf("first approval shape: %+v", as[0])
	}
	if as[1].ExpiresAt == nil {
		t.Errorf("second approval should have ExpiresAt populated: %+v", as[1])
	}
}

func TestLoad_Missing(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "does-not-exist.yaml"))
	if !errors.Is(err, ErrMissingApprovals) {
		t.Fatalf("missing file should wrap ErrMissingApprovals, got %v", err)
	}
}

func TestLoad_UnsupportedSchema(t *testing.T) {
	src := `schema_version: "0.9"
approvals: []
`
	path := writeTemp(t, "approvals.yaml", src)
	_, err := Load(path)
	if !errors.Is(err, ErrUnsupportedSchema) {
		t.Fatalf("want ErrUnsupportedSchema, got %v", err)
	}
}

func TestLoad_MissingSchema(t *testing.T) {
	// No schema_version → treated as schema mismatch (empty != v0.1).
	src := `approvals:
  - skill: x
    delta:
      added_shell_command: "y"
    reviewer: "me"
    reviewed_at: "2026-05-19T00:00:00Z"
    reason: "why"
`
	path := writeTemp(t, "approvals.yaml", src)
	_, err := Load(path)
	if !errors.Is(err, ErrUnsupportedSchema) {
		t.Fatalf("want ErrUnsupportedSchema on missing tag, got %v", err)
	}
}

func TestLoad_RejectsUnknownField(t *testing.T) {
	src := `schema_version: "0.1"
approvals:
  - skill: x
    delta:
      added_shell_command: "y"
    reviewer: "me"
    reviewed_at: "2026-05-19T00:00:00Z"
    reason: "why"
    typo_field: "oops"
`
	path := writeTemp(t, "approvals.yaml", src)
	_, err := Load(path)
	if err == nil {
		t.Fatalf("unknown field should error")
	}
	if errors.Is(err, ErrMissingApprovals) || errors.Is(err, ErrUnsupportedSchema) {
		t.Errorf("expected a plain parse error for unknown field, got typed: %v", err)
	}
}

func TestLoad_ValidationErrors(t *testing.T) {
	cases := map[string]string{
		"missing skill": `schema_version: "0.1"
approvals:
  - delta: {added_shell_command: "y"}
    reviewer: "me"
    reviewed_at: "2026-05-19T00:00:00Z"
    reason: "why"
`,
		"missing reviewer": `schema_version: "0.1"
approvals:
  - skill: x
    delta: {added_shell_command: "y"}
    reviewed_at: "2026-05-19T00:00:00Z"
    reason: "why"
`,
		"missing reason": `schema_version: "0.1"
approvals:
  - skill: x
    delta: {added_shell_command: "y"}
    reviewer: "me"
    reviewed_at: "2026-05-19T00:00:00Z"
`,
		"empty delta": `schema_version: "0.1"
approvals:
  - skill: x
    delta: {}
    reviewer: "me"
    reviewed_at: "2026-05-19T00:00:00Z"
    reason: "why"
`,
		"multi-key delta": `schema_version: "0.1"
approvals:
  - skill: x
    delta:
      added_shell_command: "a"
      added_network_url: "b"
    reviewer: "me"
    reviewed_at: "2026-05-19T00:00:00Z"
    reason: "why"
`,
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			path := writeTemp(t, "approvals.yaml", src)
			_, err := Load(path)
			if !errors.Is(err, ErrInvalidApproval) {
				t.Fatalf("want ErrInvalidApproval, got %v", err)
			}
		})
	}
}

// makeDiff is the shared fixture for Filter tests: pdf-extractor added
// curl + a network URL; code-review added a protected file read.
func makeDiff() model.Diff {
	return model.Diff{
		BaselinePath: "skills.lock",
		CurrentPath:  "<wt>",
		Entries: []model.DiffEntry{
			{Skill: "pdf-extractor", Capability: "shell_commands", Change: model.ChangeAdded,
				Value: "curl", Severity: model.SeverityMedium},
			{Skill: "pdf-extractor", Capability: "network_urls", Change: model.ChangeAdded,
				Value: "https://example.com/x", Severity: model.SeverityMedium},
			{Skill: "code-review", Capability: "file_reads", Change: model.ChangeAdded,
				Value: "./.env", Severity: model.SeverityHigh, Note: "matches protected_paths"},
		},
	}
}

func TestFilter_DropsMatchingDelta(t *testing.T) {
	d := makeDiff()
	as := []Approval{
		{
			Skill:    "pdf-extractor",
			Delta:    map[string]string{"added_shell_command": "curl"},
			Reviewer: "me",
			Reason:   "test",
		},
	}
	filtered, applied, expired := Filter(d, as, time.Now(), 0)
	if len(filtered.Entries) != 2 {
		t.Errorf("want 2 entries remaining, got %d: %+v", len(filtered.Entries), filtered.Entries)
	}
	for _, e := range filtered.Entries {
		if e.Value == "curl" {
			t.Errorf("approved curl entry should be dropped: %+v", e)
		}
	}
	if len(applied) != 1 || applied[0].Reason != "test" {
		t.Errorf("applied: %+v", applied)
	}
	if len(expired) != 0 {
		t.Errorf("expired: %+v", expired)
	}
}

func TestFilter_PreservesBaselineAndCurrentPath(t *testing.T) {
	d := makeDiff()
	filtered, _, _ := Filter(d, nil, time.Now(), 0)
	if filtered.BaselinePath != d.BaselinePath || filtered.CurrentPath != d.CurrentPath {
		t.Errorf("Filter dropped path metadata: %+v", filtered)
	}
}

func TestFilter_NoMatchKeepsEverything(t *testing.T) {
	d := makeDiff()
	as := []Approval{
		{
			Skill:    "pdf-extractor",
			Delta:    map[string]string{"added_shell_command": "wget"}, // different value
			Reviewer: "me",
			Reason:   "test",
		},
	}
	filtered, applied, _ := Filter(d, as, time.Now(), 0)
	if len(filtered.Entries) != 3 {
		t.Errorf("non-matching approval should not drop anything: got %d entries", len(filtered.Entries))
	}
	if len(applied) != 0 {
		t.Errorf("applied should be empty: %+v", applied)
	}
}

func TestFilter_ExpiredKeepsEntryAndAnnotates(t *testing.T) {
	d := makeDiff()
	expiry := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	as := []Approval{
		{
			Skill:     "pdf-extractor",
			Delta:     map[string]string{"added_shell_command": "curl"},
			Reviewer:  "me",
			Reason:    "test",
			ExpiresAt: &expiry,
		},
	}
	now := time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC) // long past the expiry
	filtered, applied, expired := Filter(d, as, now, 0)
	if len(filtered.Entries) != 3 {
		t.Errorf("expired approval should NOT drop entry: %d entries", len(filtered.Entries))
	}
	if len(applied) != 0 {
		t.Errorf("expired approval should not appear in applied: %+v", applied)
	}
	if len(expired) != 1 {
		t.Errorf("want 1 expired, got %d", len(expired))
	}
	// curl entry should now carry the expiry annotation alongside any prior note.
	var foundCurl bool
	for _, e := range filtered.Entries {
		if e.Value == "curl" {
			foundCurl = true
			if !strings.Contains(e.Note, "approval expired 2026-01-01") {
				t.Errorf("curl entry note missing expiry annotation: %+v", e)
			}
		}
	}
	if !foundCurl {
		t.Error("curl entry disappeared from filtered diff")
	}
}

func TestFilter_LiveSupersedesExpiredForSameDelta(t *testing.T) {
	d := makeDiff()
	stale := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	as := []Approval{
		{
			Skill:     "pdf-extractor",
			Delta:     map[string]string{"added_shell_command": "curl"},
			Reviewer:  "old",
			Reason:    "stale",
			ExpiresAt: &stale,
		},
		{
			Skill:    "pdf-extractor",
			Delta:    map[string]string{"added_shell_command": "curl"},
			Reviewer: "new",
			Reason:   "renewed",
		},
	}
	now := time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC)
	filtered, applied, _ := Filter(d, as, now, 0)
	// curl is dropped because the renewed approval is live.
	for _, e := range filtered.Entries {
		if e.Value == "curl" {
			t.Errorf("renewed approval should have dropped curl entry: %+v", e)
		}
	}
	if len(applied) != 1 || applied[0].Reviewer != "new" {
		t.Errorf("renewed approval should win: %+v", applied)
	}
}

func TestFilter_ApprovalReportedOncePerMatch(t *testing.T) {
	// Same approval value would match only one entry in our fixture, so
	// craft a diff with two entries that share the same (skill, key, value)
	// shape — impossible in practice (Compare dedups), but the function
	// contract should still hold.
	d := model.Diff{
		Entries: []model.DiffEntry{
			{Skill: "x", Capability: "shell_commands", Change: model.ChangeAdded, Value: "curl"},
			{Skill: "x", Capability: "shell_commands", Change: model.ChangeAdded, Value: "curl"},
		},
	}
	as := []Approval{
		{Skill: "x", Delta: map[string]string{"added_shell_command": "curl"}, Reviewer: "r", Reason: "ok"},
	}
	_, applied, _ := Filter(d, as, time.Now(), 0)
	if len(applied) != 1 {
		t.Errorf("applied should be deduplicated, got %d", len(applied))
	}
}

// TestSnippetRoundTrip is the wedge: the snippet that internal/diff
// emits must parse back as a valid Approval and drop the matching
// diff entry without manual editing of structural fields (reviewer +
// reason placeholders are filled in by the human).
func TestSnippetRoundTrip(t *testing.T) {
	src := `schema_version: "0.1"
approvals:
  - skill: "pdf-extractor"
    delta:
      added_shell_command: "curl"
    reviewer: "me@example.com"
    reviewed_at: "2026-05-19T13:53:53Z"
    reason: "Filling in the reason."
`
	path := writeTemp(t, "approvals.yaml", src)
	as, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	d := model.Diff{
		Entries: []model.DiffEntry{
			{Skill: "pdf-extractor", Capability: "shell_commands", Change: model.ChangeAdded,
				Value: "curl", Severity: model.SeverityMedium},
		},
	}
	filtered, applied, _ := Filter(d, as, time.Date(2026, 5, 19, 14, 0, 0, 0, time.UTC), 0)
	if len(filtered.Entries) != 0 {
		t.Errorf("paste-back snippet should drop the curl entry: %+v", filtered)
	}
	if len(applied) != 1 {
		t.Errorf("paste-back snippet should mark the approval as applied: %+v", applied)
	}
}

func prApproval(pr int) []Approval {
	return []Approval{
		{
			Skill:    "pdf-extractor",
			Delta:    map[string]string{"added_shell_command": "curl"},
			Reviewer: "me",
			Reason:   "test",
			PR:       pr,
		},
	}
}

func TestFilter_PRScopedApproval_MatchesSamePR(t *testing.T) {
	filtered, applied, _ := Filter(makeDiff(), prApproval(42), time.Now(), 42)
	if len(filtered.Entries) != 2 {
		t.Errorf("want 2 entries remaining, got %d: %+v", len(filtered.Entries), filtered.Entries)
	}
	if len(applied) != 1 {
		t.Errorf("want 1 applied approval, got %d", len(applied))
	}
}

func TestFilter_PRScopedApproval_RejectsOtherPR(t *testing.T) {
	filtered, applied, _ := Filter(makeDiff(), prApproval(42), time.Now(), 99)
	if len(filtered.Entries) != 3 {
		t.Fatalf("want all 3 entries kept, got %d", len(filtered.Entries))
	}
	if len(applied) != 0 {
		t.Errorf("approval scoped to PR 42 must not apply in PR 99")
	}
	var note string
	for _, e := range filtered.Entries {
		if e.Value == "curl" {
			note = e.Note
		}
	}
	if note != "approval scoped to PR #42" {
		t.Errorf("resurfaced delta should explain the scope; got note %q", note)
	}
}

func TestFilter_PRScopedApproval_RejectsNoPRContext(t *testing.T) {
	filtered, applied, _ := Filter(makeDiff(), prApproval(42), time.Now(), 0)
	if len(filtered.Entries) != 3 || len(applied) != 0 {
		t.Errorf("PR-scoped approval must not match outside PR context: kept=%d applied=%d",
			len(filtered.Entries), len(applied))
	}
}

func TestFilter_StandingApproval_IgnoresPRContext(t *testing.T) {
	filtered, applied, _ := Filter(makeDiff(), prApproval(0), time.Now(), 99)
	if len(filtered.Entries) != 2 || len(applied) != 1 {
		t.Errorf("standing approval (pr absent) must match in any PR: kept=%d applied=%d",
			len(filtered.Entries), len(applied))
	}
}

func TestValidate_NegativePRRejected(t *testing.T) {
	a := prApproval(-1)[0]
	if err := validate(a); !errors.Is(err, ErrInvalidApproval) {
		t.Errorf("negative pr must be invalid, got %v", err)
	}
}
