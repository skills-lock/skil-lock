package sarif

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/skills-lock/skil-lock/internal/model"
)

// emptyCurrent is a lockfile with no skills; used when a test only cares
// about the document skeleton.
func emptyCurrent() model.Lockfile {
	return model.NewLockfile("skil-lock test", time.Unix(0, 0))
}

// currentWith returns a lockfile populated with one skill whose source
// path is path. Used to assert physicalLocation resolution.
func currentWith(name, path string) model.Lockfile {
	lf := model.NewLockfile("skil-lock test", time.Unix(0, 0))
	lf.Skills[name] = model.LockEntry{
		Runtime:    model.RuntimeClaude,
		SourcePath: path,
	}
	return lf
}

func TestRender_EmptyDiff(t *testing.T) {
	out, err := Render(model.Diff{}, emptyCurrent(), "0.1.1")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	var doc document
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("output not valid JSON: %v\n%s", err, out)
	}
	if doc.Version != "2.1.0" {
		t.Errorf("version: want 2.1.0, got %q", doc.Version)
	}
	if doc.Schema == "" {
		t.Errorf("schema URI missing")
	}
	if len(doc.Runs) != 1 {
		t.Fatalf("runs: want 1, got %d", len(doc.Runs))
	}
	if doc.Runs[0].Tool.Driver.Name != "skil-lock" {
		t.Errorf("driver.name: %q", doc.Runs[0].Tool.Driver.Name)
	}
	if doc.Runs[0].Tool.Driver.Version != "0.1.1" {
		t.Errorf("driver.version: %q", doc.Runs[0].Tool.Driver.Version)
	}
	if len(doc.Runs[0].Tool.Driver.Rules) != 6 {
		t.Errorf("rule count: want 6, got %d", len(doc.Runs[0].Tool.Driver.Rules))
	}
	if len(doc.Runs[0].Results) != 0 {
		t.Errorf("results should be empty, got %d", len(doc.Runs[0].Results))
	}
}

func TestRender_RuleIDForCapability(t *testing.T) {
	cases := []struct {
		capability string
		wantRule   string
	}{
		{"shell_commands", "SKL-SHELL"},
		{"network_urls", "SKL-NETWORK"},
		{"file_reads", "SKL-FILE-READ"},
		{"file_writes", "SKL-FILE-WRITE"},
		{"allowed_tools", "SKL-TOOLS"},
		{"bundled_scripts", "SKL-SCRIPTS"},
		{"made_up_thing", "SKL-OTHER"},
	}
	for _, tc := range cases {
		if got := ruleIDFor(tc.capability); got != tc.wantRule {
			t.Errorf("ruleIDFor(%q) = %q, want %q", tc.capability, got, tc.wantRule)
		}
	}
}

func TestRender_SeverityMapping(t *testing.T) {
	cases := []struct {
		sev  model.Severity
		want string
	}{
		{model.SeverityHigh, "error"},
		{model.SeverityMedium, "warning"},
		{model.SeverityLow, "note"},
		{model.SeverityInfo, "note"},
		{model.Severity("garbage"), "note"},
	}
	for _, tc := range cases {
		if got := levelFor(tc.sev); got != tc.want {
			t.Errorf("levelFor(%q) = %q, want %q", tc.sev, got, tc.want)
		}
	}
}

func TestRender_LocationFromCurrentLockfile(t *testing.T) {
	d := model.Diff{Entries: []model.DiffEntry{{
		Skill:      "alpha",
		Capability: "shell_commands",
		Change:     model.ChangeAdded,
		Value:      "curl",
		Severity:   model.SeverityMedium,
	}}}
	cur := currentWith("alpha", ".claude/skills/alpha/SKILL.md")
	out, err := Render(d, cur, "0.1.1")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	var doc document
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	if len(doc.Runs[0].Results) != 1 {
		t.Fatalf("results: want 1, got %d", len(doc.Runs[0].Results))
	}
	r := doc.Runs[0].Results[0]
	if r.RuleID != "SKL-SHELL" {
		t.Errorf("rule: want SKL-SHELL, got %q", r.RuleID)
	}
	if r.Level != "warning" {
		t.Errorf("level: want warning, got %q", r.Level)
	}
	if len(r.Locations) != 1 {
		t.Fatalf("locations: want 1, got %d", len(r.Locations))
	}
	if r.Locations[0].PhysicalLocation.ArtifactLocation.URI != ".claude/skills/alpha/SKILL.md" {
		t.Errorf("location URI: %q", r.Locations[0].PhysicalLocation.ArtifactLocation.URI)
	}
}

func TestRender_RemovedSkillEmitsNoLocation(t *testing.T) {
	d := model.Diff{Entries: []model.DiffEntry{{
		Skill:      "ghost",
		Capability: "network_urls",
		Change:     model.ChangeRemoved,
		Value:      "https://gone.example",
		Severity:   model.SeverityInfo,
	}}}
	out, err := Render(d, emptyCurrent(), "0.1.1")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if strings.Contains(string(out), `"locations"`) {
		t.Errorf("removed skill should emit no locations field:\n%s", out)
	}
}

func TestRender_ResultPropertiesCarried(t *testing.T) {
	d := model.Diff{Entries: []model.DiffEntry{{
		Skill:      "alpha",
		Capability: "file_writes",
		Change:     model.ChangeAdded,
		Value:      "./.env",
		Severity:   model.SeverityHigh,
		Note:       "matches protected_paths",
	}}}
	cur := currentWith("alpha", ".claude/skills/alpha/SKILL.md")
	out, err := Render(d, cur, "0.1.1")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	var doc document
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	r := doc.Runs[0].Results[0]
	if r.RuleID != "SKL-FILE-WRITE" {
		t.Errorf("rule: %q", r.RuleID)
	}
	if r.Level != "error" {
		t.Errorf("level: %q", r.Level)
	}
	if r.Properties.Skill != "alpha" {
		t.Errorf("properties.skill: %q", r.Properties.Skill)
	}
	if r.Properties.Capability != "file_writes" {
		t.Errorf("properties.capability: %q", r.Properties.Capability)
	}
	if r.Properties.Change != "added" {
		t.Errorf("properties.change: %q", r.Properties.Change)
	}
	if r.Properties.Severity != "high" {
		t.Errorf("properties.severity: %q", r.Properties.Severity)
	}
	if r.Properties.Note != "matches protected_paths" {
		t.Errorf("properties.note: %q", r.Properties.Note)
	}
	if !strings.Contains(r.Message.Text, "protected_paths") {
		t.Errorf("message should carry note:\n%s", r.Message.Text)
	}
}

func TestRender_ModifiedShowsOldAndNew(t *testing.T) {
	d := model.Diff{Entries: []model.DiffEntry{{
		Skill:      "alpha",
		Capability: "shell_commands",
		Change:     model.ChangeModified,
		Value:      "curl",
		OldValue:   "wget",
		Severity:   model.SeverityLow,
	}}}
	out, err := Render(d, currentWith("alpha", ".claude/skills/alpha/SKILL.md"), "0.1.1")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	var doc document
	_ = json.Unmarshal(out, &doc)
	msg := doc.Runs[0].Results[0].Message.Text
	if !strings.Contains(msg, "wget") || !strings.Contains(msg, "curl") {
		t.Errorf("modified message should show old and new: %q", msg)
	}
	if !strings.Contains(msg, "→") {
		t.Errorf("modified message should use arrow separator: %q", msg)
	}
}

func TestRender_RoundTripJSON(t *testing.T) {
	d := model.Diff{Entries: []model.DiffEntry{
		{Skill: "alpha", Capability: "shell_commands", Change: model.ChangeAdded, Value: "curl", Severity: model.SeverityMedium},
		{Skill: "beta", Capability: "network_urls", Change: model.ChangeAdded, Value: "https://x.example", Severity: model.SeverityHigh, Note: "host not in allowed_domains"},
	}}
	cur := currentWith("alpha", ".claude/skills/alpha/SKILL.md")
	cur.Skills["beta"] = model.LockEntry{Runtime: model.RuntimeClaude, SourcePath: ".claude/skills/beta/SKILL.md"}
	out, err := Render(d, cur, "0.1.1")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	var doc document
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("round-trip Unmarshal: %v\n%s", err, out)
	}
	if len(doc.Runs[0].Results) != 2 {
		t.Errorf("results: want 2, got %d", len(doc.Runs[0].Results))
	}
}

func TestRender_AllRulesIncludeHelpURI(t *testing.T) {
	for _, r := range allRules() {
		if r.HelpURI == "" {
			t.Errorf("rule %s missing helpUri", r.ID)
		}
		if r.ShortDescription.Text == "" {
			t.Errorf("rule %s missing shortDescription", r.ID)
		}
		if r.FullDescription.Text == "" {
			t.Errorf("rule %s missing fullDescription", r.ID)
		}
		if len(r.Properties.Tags) == 0 {
			t.Errorf("rule %s missing tags", r.ID)
		}
	}
}
