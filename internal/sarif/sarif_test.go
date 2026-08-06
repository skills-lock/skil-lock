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
	out, err := Render(model.Diff{}, emptyCurrent(), "0.1.1", Complete(1))
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
	out, err := Render(d, cur, "0.1.1", Complete(1))
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
	out, err := Render(d, emptyCurrent(), "0.1.1", Complete(1))
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
	out, err := Render(d, cur, "0.1.1", Complete(1))
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
	if r.Properties.Layer != "drift" {
		t.Errorf("properties.layer: %q (want drift — envelope discriminator, SPEC §14.3)", r.Properties.Layer)
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
	out, err := Render(d, currentWith("alpha", ".claude/skills/alpha/SKILL.md"), "0.1.1", Complete(1))
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
	out, err := Render(d, cur, "0.1.1", Complete(1))
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

func TestRender_ASTTaxonomyEmitted(t *testing.T) {
	out, err := Render(model.Diff{}, emptyCurrent(), "0.2.2", Complete(1))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	var doc document
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	tx := doc.Runs[0].Taxonomies
	if len(tx) != 1 {
		t.Fatalf("taxonomies: want 1, got %d", len(tx))
	}
	if tx[0].Name != "OWASP-AST10" {
		t.Errorf("taxonomy name: %q", tx[0].Name)
	}
	if !tx[0].IsComprehensive {
		t.Errorf("AST10 taxonomy should be comprehensive")
	}
	if len(tx[0].Taxa) != 10 {
		t.Fatalf("taxa: want 10, got %d", len(tx[0].Taxa))
	}
	for i, want := range []string{"AST01", "AST02", "AST03", "AST04", "AST05", "AST06", "AST07", "AST08", "AST09", "AST10"} {
		if tx[0].Taxa[i].ID != want {
			t.Errorf("taxa[%d].id = %q, want %q", i, tx[0].Taxa[i].ID, want)
		}
		if tx[0].Taxa[i].HelpURI == "" {
			t.Errorf("taxon %s missing helpUri", want)
		}
	}
}

func TestRender_ASTForCapability(t *testing.T) {
	cases := []struct {
		capability string
		want       []string
	}{
		{"shell_commands", []string{"AST03", "AST07"}},
		{"network_urls", []string{"AST03", "AST07"}},
		{"file_reads", []string{"AST03", "AST07"}},
		{"file_writes", []string{"AST03", "AST07"}},
		{"allowed_tools", []string{"AST04", "AST07"}},
		{"bundled_scripts", []string{"AST02", "AST07"}},
		{"made_up_thing", []string{"AST07"}},
	}
	for _, tc := range cases {
		got := astForCapability(tc.capability)
		if strings.Join(got, ",") != strings.Join(tc.want, ",") {
			t.Errorf("astForCapability(%q) = %v, want %v", tc.capability, got, tc.want)
		}
	}
}

func TestRender_RulesCarryASTRelationshipsAndTags(t *testing.T) {
	for _, r := range allRules() {
		if len(r.Relationships) == 0 {
			t.Errorf("rule %s has no AST relationships", r.ID)
			continue
		}
		// Every rule maps to AST07 (drift) plus a capability-specific risk.
		var sawDrift, sawTag bool
		for _, rel := range r.Relationships {
			if rel.Target.ToolComponent.Name != "OWASP-AST10" {
				t.Errorf("rule %s relationship targets %q, want OWASP-AST10", r.ID, rel.Target.ToolComponent.Name)
			}
			if rel.Target.ID == "AST07" {
				sawDrift = true
			}
		}
		if !sawDrift {
			t.Errorf("rule %s should reference AST07 (every delta is drift)", r.ID)
		}
		for _, tag := range r.Properties.Tags {
			if strings.HasPrefix(tag, "external/owasp-ast/ast") {
				sawTag = true
			}
		}
		if !sawTag {
			t.Errorf("rule %s missing external/owasp-ast/* tag", r.ID)
		}
	}
}

func TestRender_ResultsCarryASTTaxa(t *testing.T) {
	d := model.Diff{Entries: []model.DiffEntry{{
		Skill:      "alpha",
		Capability: "bundled_scripts",
		Change:     model.ChangeModified,
		Value:      "scripts/run.sh",
		Severity:   model.SeverityLow,
	}}}
	out, err := Render(d, currentWith("alpha", ".claude/skills/alpha/SKILL.md"), "0.2.2", Complete(1))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	var doc document
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	taxa := doc.Runs[0].Results[0].Taxa
	if len(taxa) != 2 {
		t.Fatalf("result taxa: want 2, got %d", len(taxa))
	}
	if taxa[0].ID != "AST02" || taxa[1].ID != "AST07" {
		t.Errorf("bundled_scripts taxa = %q/%q, want AST02/AST07", taxa[0].ID, taxa[1].ID)
	}
	if taxa[0].ToolComponent.Name != "OWASP-AST10" {
		t.Errorf("result taxon component: %q", taxa[0].ToolComponent.Name)
	}
}

func TestNormalizeSHA256(t *testing.T) {
	want := strings.Repeat("a", 64)
	for _, in := range []string{
		"sha256:" + strings.Repeat("a", 64),
		"sha256:" + strings.Repeat("A", 64),
		strings.Repeat("A", 64),
	} {
		if got := normalizeSHA256(in); got != want {
			t.Errorf("normalizeSHA256(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRender_TargetDigestEmitted(t *testing.T) {
	digest := strings.Repeat("a", 64)
	d := model.Diff{Entries: []model.DiffEntry{{
		Skill:      "alpha",
		Capability: "shell_commands",
		Change:     model.ChangeAdded,
		Value:      "curl",
		Severity:   model.SeverityMedium,
	}}}
	cur := model.NewLockfile("skil-lock test", time.Unix(0, 0))
	cur.Skills["alpha"] = model.LockEntry{
		Runtime:     model.RuntimeClaude,
		SourcePath:  ".claude/skills/alpha/SKILL.md",
		ContentHash: "sha256:" + digest,
	}
	out, err := Render(d, cur, "0.2.3", Complete(1))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	var doc document
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	arts := doc.Runs[0].Artifacts
	if len(arts) != 1 {
		t.Fatalf("artifacts: want 1, got %d", len(arts))
	}
	if arts[0].Location.URI != ".claude/skills/alpha/SKILL.md" {
		t.Errorf("artifact uri: %q", arts[0].Location.URI)
	}
	if arts[0].Hashes == nil || arts[0].Hashes.SHA256 != digest {
		t.Errorf("artifact sha-256: %+v", arts[0].Hashes)
	}
	loc := doc.Runs[0].Results[0].Locations[0].PhysicalLocation.ArtifactLocation
	if loc.Index == nil || *loc.Index != 0 {
		t.Errorf("result location index: %v, want 0", loc.Index)
	}
}

func TestRender_NoDigestWithoutContentHash(t *testing.T) {
	// A skill present in current but without a content hash (older 0.1
	// lockfile, or a test fixture) emits a location URI but no artifact.
	d := model.Diff{Entries: []model.DiffEntry{{
		Skill:      "alpha",
		Capability: "shell_commands",
		Change:     model.ChangeAdded,
		Value:      "curl",
		Severity:   model.SeverityMedium,
	}}}
	out, err := Render(d, currentWith("alpha", ".claude/skills/alpha/SKILL.md"), "0.2.3", Complete(1))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	var doc document
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	if len(doc.Runs[0].Artifacts) != 0 {
		t.Errorf("no artifacts expected without content hash, got %d", len(doc.Runs[0].Artifacts))
	}
	loc := doc.Runs[0].Results[0].Locations[0].PhysicalLocation.ArtifactLocation
	if loc.Index != nil {
		t.Errorf("no artifact index expected without artifact, got %d", *loc.Index)
	}
	if loc.URI != ".claude/skills/alpha/SKILL.md" {
		t.Errorf("location URI still expected: %q", loc.URI)
	}
}

func TestRender_DigestDedupedPerSkill(t *testing.T) {
	// Two findings on the same skill share one artifact entry.
	d := model.Diff{Entries: []model.DiffEntry{
		{Skill: "alpha", Capability: "shell_commands", Change: model.ChangeAdded, Value: "curl", Severity: model.SeverityMedium},
		{Skill: "alpha", Capability: "network_urls", Change: model.ChangeAdded, Value: "https://x.example", Severity: model.SeverityHigh},
	}}
	cur := model.NewLockfile("skil-lock test", time.Unix(0, 0))
	cur.Skills["alpha"] = model.LockEntry{
		Runtime:     model.RuntimeClaude,
		SourcePath:  ".claude/skills/alpha/SKILL.md",
		ContentHash: "sha256:" + strings.Repeat("b", 64),
	}
	out, err := Render(d, cur, "0.2.3", Complete(1))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	var doc document
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	if len(doc.Runs[0].Artifacts) != 1 {
		t.Errorf("artifacts: want 1 deduped, got %d", len(doc.Runs[0].Artifacts))
	}
	for i, r := range doc.Runs[0].Results {
		loc := r.Locations[0].PhysicalLocation.ArtifactLocation
		if loc.Index == nil || *loc.Index != 0 {
			t.Errorf("result %d index: %v, want 0", i, loc.Index)
		}
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

// --- completeness declaration (multi-scanner envelope RFC) ---

// decodeRun unmarshals a rendered document and returns its single run as
// a generic map, so tests assert on the emitted JSON rather than on the
// Go structs that produced it.
func decodeRun(t *testing.T, out []byte) map[string]any {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	runs, ok := doc["runs"].([]any)
	if !ok || len(runs) != 1 {
		t.Fatalf("expected exactly 1 run, got %v", doc["runs"])
	}
	run, ok := runs[0].(map[string]any)
	if !ok {
		t.Fatalf("run is not an object: %T", runs[0])
	}
	return run
}

func TestRender_CompleteRunDeclaresCompleteness(t *testing.T) {
	out, err := Render(model.Diff{}, emptyCurrent(), "0.2.4", Complete(3))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	run := decodeRun(t, out)

	// A clean run still declares its basis. Omitting the declaration on
	// the happy path is what makes silence ambiguous between "nothing
	// was bounded" and "something was and the tool didn't say".
	props, ok := run["properties"].(map[string]any)
	if !ok {
		t.Fatalf("complete run has no properties: %v", run["properties"])
	}
	comp, ok := props["completeness"].(map[string]any)
	if !ok {
		t.Fatalf("complete run has no completeness declaration: %v", props)
	}
	if got := comp["basis"]; got != "complete" {
		t.Errorf("basis = %v, want complete", got)
	}
	if got := comp["skillsDiscovered"]; got != float64(3) {
		t.Errorf("skillsDiscovered = %v, want 3", got)
	}
	if got := comp["skillsUnanalysed"]; got != float64(0) {
		t.Errorf("skillsUnanalysed = %v, want 0", got)
	}
	if got := comp["resultsBounded"]; got != false {
		t.Errorf("resultsBounded = %v, want false", got)
	}
	// appliedCap belongs to emitters that cap the results array.
	// SkilLock does not, and must not imply that it might.
	if _, present := comp["appliedCap"]; present {
		t.Error("completeness must not declare appliedCap: SkilLock never caps results")
	}
	if _, present := comp["droppedCount"]; present {
		t.Error("completeness must not declare droppedCount: no findings are withheld")
	}

	invs, ok := run["invocations"].([]any)
	if !ok || len(invs) != 1 {
		t.Fatalf("expected 1 invocation, got %v", run["invocations"])
	}
	inv := invs[0].(map[string]any)
	if got := inv["executionSuccessful"]; got != true {
		t.Errorf("executionSuccessful = %v, want true", got)
	}
	if _, present := inv["toolExecutionNotifications"]; present {
		t.Error("a complete run must not emit notifications")
	}
}

func TestRender_UnanalysedSkillIsVisibleInTheReport(t *testing.T) {
	// The regression this whole declaration exists for: in warn mode a
	// SKILL.md that fails to parse used to drop out of the scan with the
	// error going only to stderr, producing a report byte-identical to a
	// run where everything parsed.
	comp := Completeness{
		Discovered: 2,
		Analysed:   1,
		Unanalysed: []Unanalysed{{
			Path:   ".claude/skills/release-notes/SKILL.md",
			Reason: `SKILL.md frontmatter is missing a required field: "name"`,
		}},
	}
	out, err := Render(model.Diff{}, emptyCurrent(), "0.2.4", comp)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	clean, err := Render(model.Diff{}, emptyCurrent(), "0.2.4", Complete(1))
	if err != nil {
		t.Fatalf("Render clean: %v", err)
	}
	if string(out) == string(clean) {
		t.Fatal("a run that skipped a skill is byte-identical to a clean run")
	}

	run := decodeRun(t, out)
	props := run["properties"].(map[string]any)["completeness"].(map[string]any)
	if got := props["basis"]; got != "partial" {
		t.Errorf("basis = %v, want partial", got)
	}
	if got := props["skillsUnanalysed"]; got != float64(1) {
		t.Errorf("skillsUnanalysed = %v, want 1", got)
	}

	invs := run["invocations"].([]any)
	inv := invs[0].(map[string]any)
	// The analysis itself completed; it just covered less than it was
	// handed. executionSuccessful=false is reserved for a failed scan.
	if got := inv["executionSuccessful"]; got != true {
		t.Errorf("executionSuccessful = %v, want true", got)
	}
	notes, ok := inv["toolExecutionNotifications"].([]any)
	if !ok || len(notes) != 1 {
		t.Fatalf("expected 1 notification, got %v", inv["toolExecutionNotifications"])
	}
	note := notes[0].(map[string]any)
	if got := note["level"]; got != "warning" {
		// error would mean the run failed (SARIF §3.20.21).
		t.Errorf("level = %v, want warning", got)
	}
	np := note["properties"].(map[string]any)
	if got := np["skilLockKind"]; got != "skill-not-analysed" {
		t.Errorf("skilLockKind = %v", got)
	}
	if got := np["path"]; got != ".claude/skills/release-notes/SKILL.md" {
		t.Errorf("path = %v", got)
	}
	if msg, _ := note["message"].(map[string]any); msg != nil {
		if !strings.Contains(msg["text"].(string), "was not analysed") {
			t.Errorf("message does not name the failure: %v", msg["text"])
		}
	}
}

func TestRenderFailure_IsTheFailureChannel(t *testing.T) {
	out, err := RenderFailure("0.2.4", "read .claude/skills: permission denied")
	if err != nil {
		t.Fatalf("RenderFailure: %v", err)
	}
	run := decodeRun(t, out)

	invs := run["invocations"].([]any)
	inv := invs[0].(map[string]any)
	if got := inv["executionSuccessful"]; got != false {
		t.Errorf("executionSuccessful = %v, want false on a failed scan", got)
	}
	notes := inv["toolExecutionNotifications"].([]any)
	note := notes[0].(map[string]any)
	if got := note["level"]; got != "error" {
		t.Errorf("level = %v, want error", got)
	}
	if got := note["properties"].(map[string]any)["skilLockKind"]; got != "scan-failed" {
		t.Errorf("skilLockKind = %v, want scan-failed", got)
	}
	comp := run["properties"].(map[string]any)["completeness"].(map[string]any)
	if got := comp["basis"]; got != "not-analysed" {
		t.Errorf("basis = %v, want not-analysed", got)
	}
	// A failed run reports no findings — and must not be readable as a
	// clean one, which is what the error notification above prevents.
	if results, ok := run["results"].([]any); !ok || len(results) != 0 {
		t.Errorf("results = %v, want empty", run["results"])
	}
}
