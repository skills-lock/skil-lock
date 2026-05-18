// Package diff compares two lockfiles and renders the capability
// delta as a model.Diff. Rendering to PR-comment markdown lives in the
// renderer subpackage (or for v1 simplicity, here next to Compare).
package diff

import (
	"fmt"
	"sort"
	"strings"

	"github.com/skills-lock/skil-lock/internal/model"
)

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
// MOCKUPS.md / PRODUCT.md §13: short header, capability table grouped
// by change type, then a verdict line.
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
	fmt.Fprintf(&b, "| Skill | Capability | Change | Detail |\n")
	fmt.Fprintf(&b, "|---|---|---|---|\n")
	for _, e := range d.Entries {
		fmt.Fprintf(&b, "| %s | %s | %s | %s |\n",
			e.Skill, e.Capability, changeMarker(e.Change), renderDetail(e))
	}
	if verdict != "" {
		fmt.Fprintf(&b, "\n**Verdict:** %s\n", verdict)
	}
	return b.String()
}

func renderDetail(e model.DiffEntry) string {
	switch e.Change {
	case model.ChangeAdded:
		if e.Note != "" {
			return fmt.Sprintf("`%s` (%s)", e.Value, e.Note)
		}
		return fmt.Sprintf("`%s`", e.Value)
	case model.ChangeRemoved:
		if e.Note != "" {
			return fmt.Sprintf("`%s` (%s)", e.Value, e.Note)
		}
		return fmt.Sprintf("`%s`", e.Value)
	case model.ChangeModified:
		if e.Note != "" {
			return fmt.Sprintf("`%s` → `%s` (%s)", e.OldValue, e.Value, e.Note)
		}
		return fmt.Sprintf("`%s` → `%s`", e.OldValue, e.Value)
	}
	return e.Value
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
