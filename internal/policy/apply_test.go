package policy

import (
	"testing"

	"github.com/skills-lock/skil-lock/internal/model"
)

func newDiff(entries ...model.DiffEntry) *model.Diff {
	return &model.Diff{Entries: entries}
}

func TestApply_NoPolicyNoChange(t *testing.T) {
	d := newDiff(model.DiffEntry{
		Skill: "x", Capability: "shell_commands", Change: model.ChangeAdded,
		Value: "curl", Severity: model.SeverityMedium,
	})
	Apply(d, Default())
	if d.Entries[0].Severity != model.SeverityMedium {
		t.Fatalf("default policy should not lift severity; got %q", d.Entries[0].Severity)
	}
}

func TestApply_RequireApprovalLiftsAddedToHigh(t *testing.T) {
	d := newDiff(model.DiffEntry{
		Skill: "x", Capability: "shell_commands", Change: model.ChangeAdded,
		Value: "curl", Severity: model.SeverityMedium,
	})
	pol := model.Policy{
		Mode:            model.PolicyModeBlock,
		RequireApproval: []string{"shell_commands"},
	}
	Apply(d, pol)
	if d.Entries[0].Severity != model.SeverityHigh {
		t.Fatalf("want SeverityHigh, got %q", d.Entries[0].Severity)
	}
	if d.Entries[0].Note == "" {
		t.Error("expected note explaining the lift")
	}
}

func TestApply_RequireApprovalIgnoresRemoved(t *testing.T) {
	d := newDiff(model.DiffEntry{
		Skill: "x", Capability: "shell_commands", Change: model.ChangeRemoved,
		Value: "curl", Severity: model.SeverityInfo,
	})
	Apply(d, model.Policy{RequireApproval: []string{"shell_commands"}})
	if d.Entries[0].Severity != model.SeverityInfo {
		t.Fatalf("removed entries must not be lifted; got %q", d.Entries[0].Severity)
	}
}

func TestApply_AllowedDomainsBlocksUnlistedHost(t *testing.T) {
	d := newDiff(model.DiffEntry{
		Skill: "x", Capability: "network_urls", Change: model.ChangeAdded,
		Value: "https://evil.com/exfil", Severity: model.SeverityMedium,
	})
	pol := model.Policy{AllowedDomains: []string{"api.github.com", "*.openai.com"}}
	Apply(d, pol)
	if d.Entries[0].Severity != model.SeverityHigh {
		t.Fatalf("unlisted host should lift to high; got %q", d.Entries[0].Severity)
	}
}

func TestApply_AllowedDomainsPermitsListedHost(t *testing.T) {
	d := newDiff(model.DiffEntry{
		Skill: "x", Capability: "network_urls", Change: model.ChangeAdded,
		Value: "https://api.github.com/repos/o/r/releases", Severity: model.SeverityMedium,
	})
	pol := model.Policy{AllowedDomains: []string{"api.github.com"}}
	Apply(d, pol)
	if d.Entries[0].Severity != model.SeverityMedium {
		t.Fatalf("listed host should NOT lift; got %q", d.Entries[0].Severity)
	}
}

func TestApply_AllowedDomainsGlobMatchesSubdomain(t *testing.T) {
	d := newDiff(model.DiffEntry{
		Skill: "x", Capability: "network_urls", Change: model.ChangeAdded,
		Value: "https://api.github.com/x", Severity: model.SeverityMedium,
	})
	pol := model.Policy{AllowedDomains: []string{"*.github.com"}}
	Apply(d, pol)
	if d.Entries[0].Severity != model.SeverityMedium {
		t.Fatalf("glob *.github.com should match api.github.com; got %q", d.Entries[0].Severity)
	}
}

func TestApply_AllowedDomainsEmptyDisablesRule(t *testing.T) {
	d := newDiff(model.DiffEntry{
		Skill: "x", Capability: "network_urls", Change: model.ChangeAdded,
		Value: "https://evil.com/x", Severity: model.SeverityMedium,
	})
	Apply(d, Default())
	if d.Entries[0].Severity != model.SeverityMedium {
		t.Fatalf("empty allow-list disables the rule; got %q", d.Entries[0].Severity)
	}
}

func TestApply_ProtectedPathsDoublestarUnder(t *testing.T) {
	d := newDiff(
		model.DiffEntry{Skill: "x", Capability: "file_reads", Change: model.ChangeAdded,
			Value: "secrets/db.yaml", Severity: model.SeverityLow},
		model.DiffEntry{Skill: "x", Capability: "file_writes", Change: model.ChangeAdded,
			Value: "secrets/nested/key.pem", Severity: model.SeverityLow},
	)
	pol := model.Policy{ProtectedPaths: []string{"secrets/**"}}
	Apply(d, pol)
	for i, e := range d.Entries {
		if e.Severity != model.SeverityHigh {
			t.Errorf("entry %d (%s) should be high; got %q", i, e.Value, e.Severity)
		}
	}
}

func TestApply_ProtectedPathsDoublestarSuffix(t *testing.T) {
	d := newDiff(model.DiffEntry{
		Skill: "x", Capability: "file_reads", Change: model.ChangeAdded,
		Value: "configs/sub/server.pem", Severity: model.SeverityLow,
	})
	pol := model.Policy{ProtectedPaths: []string{"**/*.pem"}}
	Apply(d, pol)
	if d.Entries[0].Severity != model.SeverityHigh {
		t.Fatalf("**/*.pem should match nested .pem; got %q", d.Entries[0].Severity)
	}
}

func TestApply_ProtectedPathsLiteral(t *testing.T) {
	d := newDiff(model.DiffEntry{
		Skill: "x", Capability: "file_reads", Change: model.ChangeAdded,
		Value: ".env", Severity: model.SeverityLow,
	})
	pol := model.Policy{ProtectedPaths: []string{".env"}}
	Apply(d, pol)
	if d.Entries[0].Severity != model.SeverityHigh {
		t.Fatalf("literal .env should match; got %q", d.Entries[0].Severity)
	}
}

func TestApply_ProtectedPathsIgnoresNonFileCapabilities(t *testing.T) {
	d := newDiff(model.DiffEntry{
		Skill: "x", Capability: "shell_commands", Change: model.ChangeAdded,
		Value: ".env", Severity: model.SeverityMedium,
	})
	pol := model.Policy{ProtectedPaths: []string{".env"}}
	Apply(d, pol)
	if d.Entries[0].Severity != model.SeverityMedium {
		t.Fatalf("protected_paths only applies to file_reads/writes; got %q", d.Entries[0].Severity)
	}
}

func TestApply_LiftOnlyRaises(t *testing.T) {
	d := newDiff(model.DiffEntry{
		Skill: "x", Capability: "shell_commands", Change: model.ChangeAdded,
		Value: "ls", Severity: model.SeverityHigh, Note: "manually raised",
	})
	Apply(d, model.Policy{RequireApproval: []string{"shell_commands"}})
	if d.Entries[0].Severity != model.SeverityHigh {
		t.Fatalf("severity should stay high; got %q", d.Entries[0].Severity)
	}
}

func TestApply_MultipleRulesAccumulateNotes(t *testing.T) {
	d := newDiff(model.DiffEntry{
		Skill: "x", Capability: "network_urls", Change: model.ChangeAdded,
		Value: "https://evil.com/x", Severity: model.SeverityMedium,
	})
	pol := model.Policy{
		RequireApproval: []string{"network_urls"},
		AllowedDomains:  []string{"api.github.com"},
	}
	Apply(d, pol)
	if d.Entries[0].Severity != model.SeverityHigh {
		t.Fatalf("want high, got %q", d.Entries[0].Severity)
	}
	if d.Entries[0].Note == "" || (!contains(d.Entries[0].Note, "require_approval") || !contains(d.Entries[0].Note, "allowed_domains")) {
		t.Errorf("note should record both rule firings, got %q", d.Entries[0].Note)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
