// Package diff compares two lockfiles and renders the capability
// delta as a model.Diff. Rendering to PR-comment markdown lives in the
// renderer subpackage (or for v1 simplicity, here next to Compare).
package diff

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/skills-lock/skil-lock/internal/model"
)

// nowFunc is the clock for time-stamping the approvals-snippet placeholder.
// Overridable in tests so RenderMarkdown output is deterministic.
var nowFunc = time.Now

// SnippetThreshold is the severity at or above which RenderMarkdown emits
// the copy-paste approvals snippet. It matches the cmd-level blocking
// threshold so the snippet only appears when CI would (or could, if mode
// flipped to block) actually fail the build.
const SnippetThreshold = model.SeverityMedium

// Capabilities are the six behavior categories the diff compares. The
// order is also the rendering order — most security-relevant first so
// the PR comment leads with the worst delta.
var Capabilities = []struct {
	Key   string
	Label string
}{
	{Key: "shell_commands", Label: "shell_commands"},
	{Key: "network_urls", Label: "network_urls"},
	{Key: "file_reads", Label: "file_reads"},
	{Key: "file_writes", Label: "file_writes"},
	{Key: "allowed_tools", Label: "allowed_tools"},
	{Key: "bundled_scripts", Label: "bundled_scripts"},
}

// Compare returns the capability delta between an old (baseline) and
// new (current) lockfile. Both files are taken as already loaded; the
// caller wires the file I/O. The result is sorted deterministically
// for stable PR-comment output.
func Compare(old, new model.Lockfile, baselinePath, currentPath string) model.Diff {
	out := model.Diff{
		BaselinePath: baselinePath,
		CurrentPath:  currentPath,
	}

	names := mergedSkillNames(old, new)
	for _, name := range names {
		oldEntry, oldExists := old.Skills[name]
		newEntry, newExists := new.Skills[name]

		switch {
		case !oldExists && newExists:
			// New skill added — every populated capability is an addition.
			for _, cap := range Capabilities {
				for _, v := range valuesFor(newEntry.Behavior, cap.Key) {
					out.Entries = append(out.Entries, model.DiffEntry{
						Skill:      name,
						Capability: cap.Key,
						Change:     model.ChangeAdded,
						Value:      v,
						Severity:   defaultSeverity(cap.Key, model.ChangeAdded),
						Note:       "skill is new",
					})
				}
			}
		case oldExists && !newExists:
			for _, cap := range Capabilities {
				for _, v := range valuesFor(oldEntry.Behavior, cap.Key) {
					out.Entries = append(out.Entries, model.DiffEntry{
						Skill:      name,
						Capability: cap.Key,
						Change:     model.ChangeRemoved,
						Value:      v,
						Severity:   model.SeverityInfo,
						Note:       "skill removed",
					})
				}
			}
		case oldExists && newExists:
			diffEntry(name, oldEntry, newEntry, &out)
		}
	}

	sortEntries(out.Entries)
	return out
}

// diffEntry compares the behavior of one skill that exists in both
// lockfiles and appends added/removed/modified DiffEntry rows.
func diffEntry(name string, oldEntry, newEntry model.LockEntry, out *model.Diff) {
	for _, cap := range Capabilities {
		oldVals := setOf(valuesFor(oldEntry.Behavior, cap.Key))
		newVals := setOf(valuesFor(newEntry.Behavior, cap.Key))

		for v := range newVals {
			if _, ok := oldVals[v]; !ok {
				out.Entries = append(out.Entries, model.DiffEntry{
					Skill:      name,
					Capability: cap.Key,
					Change:     model.ChangeAdded,
					Value:      v,
					Severity:   defaultSeverity(cap.Key, model.ChangeAdded),
				})
			}
		}
		for v := range oldVals {
			if _, ok := newVals[v]; !ok {
				out.Entries = append(out.Entries, model.DiffEntry{
					Skill:      name,
					Capability: cap.Key,
					Change:     model.ChangeRemoved,
					Value:      v,
					Severity:   model.SeverityInfo,
				})
			}
		}
	}

	// Content hash drift without any behavior delta is informational.
	if oldEntry.ContentHash != newEntry.ContentHash {
		hasBehaviorDelta := false
		for _, e := range out.Entries {
			if e.Skill == name && e.Capability != "content_hash" {
				hasBehaviorDelta = true
				break
			}
		}
		if !hasBehaviorDelta {
			out.Entries = append(out.Entries, model.DiffEntry{
				Skill:      name,
				Capability: "content_hash",
				Change:     model.ChangeModified,
				Value:      newEntry.ContentHash,
				OldValue:   oldEntry.ContentHash,
				Severity:   model.SeverityInfo,
				Note:       "hash changed but no behavior delta",
			})
		}
	}

	if oldEntry.Version != newEntry.Version {
		out.Entries = append(out.Entries, model.DiffEntry{
			Skill:      name,
			Capability: "version",
			Change:     model.ChangeModified,
			Value:      newEntry.Version,
			OldValue:   oldEntry.Version,
			Severity:   model.SeverityInfo,
		})
	}
}

// valuesFor pulls one capability's slice out of a Behavior by key.
// Keeping this dispatch in one place means adding a capability is one
// switch case plus an entry in Capabilities — no Go reflection.
func valuesFor(b model.Behavior, key string) []string {
	switch key {
	case "shell_commands":
		return b.ShellCommands
	case "network_urls":
		return b.NetworkURLs
	case "file_reads":
		return b.FileReads
	case "file_writes":
		return b.FileWrites
	case "allowed_tools":
		return b.AllowedTools
	case "bundled_scripts":
		return b.BundledScripts
	}
	return nil
}

// defaultSeverity assigns the v1 severity scheme: shell additions and
// network additions are medium; everything else is low. The policy
// layer (T2.x) will lift this to high when a value intersects
// protected_paths.
func defaultSeverity(capability string, change model.ChangeType) model.Severity {
	if change != model.ChangeAdded {
		return model.SeverityInfo
	}
	switch capability {
	case "shell_commands", "network_urls":
		return model.SeverityMedium
	case "file_writes":
		return model.SeverityLow
	}
	return model.SeverityLow
}

func mergedSkillNames(a, b model.Lockfile) []string {
	set := map[string]struct{}{}
	for k := range a.Skills {
		set[k] = struct{}{}
	}
	for k := range b.Skills {
		set[k] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func setOf(xs []string) map[string]struct{} {
	m := make(map[string]struct{}, len(xs))
	for _, x := range xs {
		m[x] = struct{}{}
	}
	return m
}

// sortEntries orders entries by (Skill, Capability, Change, Value) so
// the diff output is deterministic across runs.
func sortEntries(es []model.DiffEntry) {
	sort.Slice(es, func(i, j int) bool {
		if es[i].Skill != es[j].Skill {
			return es[i].Skill < es[j].Skill
		}
		if es[i].Capability != es[j].Capability {
			return capabilityOrder(es[i].Capability) < capabilityOrder(es[j].Capability)
		}
		if es[i].Change != es[j].Change {
			return changeOrder(es[i].Change) < changeOrder(es[j].Change)
		}
		return es[i].Value < es[j].Value
	})
}

func capabilityOrder(key string) int {
	for i, c := range Capabilities {
		if c.Key == key {
			return i
		}
	}
	return len(Capabilities) + 1
}

func changeOrder(c model.ChangeType) int {
	switch c {
	case model.ChangeAdded:
		return 0
	case model.ChangeModified:
		return 1
	case model.ChangeRemoved:
		return 2
	}
	return 3
}

// RenderMarkdown formats a diff for a PR comment. The shape follows
// MOCKUPS.md / PRODUCT.md §13: short header, capability table with a
// per-row Reason column (sourced from DiffEntry.Note, which policy.Apply
// populates with rule-fired explanations), then a verdict line, then a
// copy-paste .skil-lock-approvals.yaml snippet when any added entry is
// at severity >= SnippetThreshold. The snippet is the wedge versus
// exit-code-only scanners (Mondoo, SkillFortify): a reviewer can approve
// a delta inline with one paste.
//
// If verdict is empty, no verdict line is rendered — the policy layer
// is responsible for deciding pass / warn / block. RenderMarkdown is
// pure formatting.
func RenderMarkdown(d model.Diff, verdict string) string {
	if len(d.Entries) == 0 {
		return fmt.Sprintf("### SkilLock — no capability deltas\n\nBaseline `%s` matches current `%s`.\n",
			d.BaselinePath, d.CurrentPath)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "### SkilLock — capability delta\n\n")
	fmt.Fprintf(&b, "Comparing `%s` (baseline) vs `%s` (current).\n\n", d.BaselinePath, d.CurrentPath)
	fmt.Fprintf(&b, "| Skill | Capability | Change | Detail | Reason |\n")
	fmt.Fprintf(&b, "|---|---|---|---|---|\n")
	for _, e := range d.Entries {
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s |\n",
			e.Skill, e.Capability, changeMarker(e.Change), renderDetail(e), renderReason(e))
	}
	if verdict != "" {
		fmt.Fprintf(&b, "\n**Verdict:** %s\n", verdict)
	}
	if snippet := renderApprovalsSnippet(d); snippet != "" {
		fmt.Fprint(&b, snippet)
	}
	return b.String()
}

// renderDetail formats the Value column. The note that previously rode in
// parens here now lives in its own Reason column.
func renderDetail(e model.DiffEntry) string {
	switch e.Change {
	case model.ChangeAdded, model.ChangeRemoved:
		return fmt.Sprintf("`%s`", e.Value)
	case model.ChangeModified:
		return fmt.Sprintf("`%s` → `%s`", e.OldValue, e.Value)
	}
	return e.Value
}

// renderReason surfaces DiffEntry.Note in its own column. An em-dash keeps
// the table visually balanced when no rule fired (better than an empty
// cell, which some markdown renderers collapse).
func renderReason(e model.DiffEntry) string {
	if e.Note == "" {
		return "—"
	}
	return e.Note
}

func changeMarker(c model.ChangeType) string {
	switch c {
	case model.ChangeAdded:
		return "+"
	case model.ChangeRemoved:
		return "-"
	case model.ChangeModified:
		return "~"
	}
	return "?"
}

// renderApprovalsSnippet returns a fenced YAML block conforming to
// PRODUCT.md §8, pre-filled with one approval entry per added delta at
// severity >= SnippetThreshold. Returns "" if no such deltas exist.
//
// Reviewer + reason fields are placeholders the reviewer fills in;
// reviewed_at is the current wall clock (UTC, second precision) so a
// paste into the repo is a complete, valid record.
func renderApprovalsSnippet(d model.Diff) string {
	threshold := severityRank(SnippetThreshold)
	var blocking []model.DiffEntry
	for _, e := range d.Entries {
		if e.Change != model.ChangeAdded {
			continue
		}
		if severityRank(e.Severity) >= threshold {
			blocking = append(blocking, e)
		}
	}
	if len(blocking) == 0 {
		return ""
	}
	stamp := nowFunc().UTC().Truncate(time.Second).Format(time.RFC3339)
	var b strings.Builder
	fmt.Fprint(&b, "\n**To approve, append to `.skil-lock-approvals.yaml`:**\n\n")
	fmt.Fprint(&b, "```yaml\n")
	fmt.Fprint(&b, "schema_version: \"0.1\"\n")
	fmt.Fprint(&b, "approvals:\n")
	for _, e := range blocking {
		fmt.Fprintf(&b, "  - skill: %s\n", yamlString(e.Skill))
		fmt.Fprint(&b, "    delta:\n")
		fmt.Fprintf(&b, "      %s: %s\n", DeltaKey(e.Capability, e.Change), yamlString(e.Value))
		fmt.Fprint(&b, "    reviewer: \"you@example.com\"\n")
		fmt.Fprintf(&b, "    reviewed_at: %s\n", yamlString(stamp))
		fmt.Fprint(&b, "    reason: \"<why this delta is acceptable>\"\n")
	}
	fmt.Fprint(&b, "```\n")
	return b.String()
}

// DeltaKey turns ("shell_commands", added) into "added_shell_command",
// matching PRODUCT.md §8's example schema. v0.1 capability keys are all
// regular plurals so a single trailing-`s` strip is enough.
//
// Exported because internal/approvals reads .skil-lock-approvals.yaml
// and must match incoming delta keys against entries in a model.Diff;
// the renderer and the consumer have to agree on the key shape, and
// keeping the function in one place is how we keep them in sync.
func DeltaKey(capability string, change model.ChangeType) string {
	verb := "added"
	switch change {
	case model.ChangeRemoved:
		verb = "removed"
	case model.ChangeModified:
		verb = "modified"
	}
	return verb + "_" + strings.TrimSuffix(capability, "s")
}

// severityRank mirrors policy.severityRank so the two stay in lock-step.
// We duplicate rather than import to avoid a circular dep — diff is
// upstream of policy.
func severityRank(s model.Severity) int {
	switch s {
	case model.SeverityHigh:
		return 4
	case model.SeverityMedium:
		return 3
	case model.SeverityLow:
		return 2
	case model.SeverityInfo:
		return 1
	}
	return 0
}

// yamlString double-quotes a value and escapes the two characters that
// would break a double-quoted YAML scalar. Diff values are URLs / paths /
// command names; conservative quoting keeps the snippet parseable even
// when the value contains spaces, colons, or globs.
func yamlString(s string) string {
	escaped := strings.ReplaceAll(s, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)
	return `"` + escaped + `"`
}
