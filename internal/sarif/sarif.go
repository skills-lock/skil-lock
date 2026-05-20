// Package sarif renders a model.Diff as a SARIF v2.1.0 document so
// GitHub's code-scanning UI can surface SkilLock findings inline in
// pull requests and in the repo's Security tab.
//
// The output is the GitHub-flavored subset of SARIF: a single run with
// driver metadata, the six capability rules, and one result per diff
// entry. Locations point at each skill's SKILL.md (resolved via the
// current lockfile passed to Render).
package sarif

import (
	"encoding/json"
	"fmt"

	"github.com/skills-lock/skil-lock/internal/model"
)

// InformationURI is the canonical project URL emitted in the SARIF
// driver block; reviewers click it to learn what SkilLock is.
const InformationURI = "https://github.com/skills-lock/skil-lock"

// HelpURIBase is the prefix appended with each rule ID for the
// per-rule helpUri. Anchors land readers on the spec section that
// defines the capability and severity rules.
const HelpURIBase = "https://github.com/skills-lock/skil-lock/blob/main/SPEC.md"

// Render returns the SARIF v2.1.0 JSON document for diff d. The
// current lockfile is used to resolve each skill's SKILL.md path; an
// entry referencing a skill missing from current (a removed skill) is
// reported without a physicalLocation. version is the running CLI
// version string ("0.1.0", "dev", etc.) emitted in driver.version.
func Render(d model.Diff, current model.Lockfile, version string) ([]byte, error) {
	doc := document{
		Schema:  "https://raw.githubusercontent.com/oasis-tcs/sarif-spec/main/Documents/CommitteeSpecifications/2.1.0/sarif-schema-2.1.0.json",
		Version: "2.1.0",
		Runs: []run{{
			Tool: tool{
				Driver: driver{
					Name:           "skil-lock",
					Version:        version,
					InformationURI: InformationURI,
					Rules:          allRules(),
				},
			},
			Results: buildResults(d, current),
		}},
	}
	return json.MarshalIndent(doc, "", "  ")
}

// allRules returns the static rule set. SkilLock's deterministic
// detectors emit at most six kinds of capability deltas; one rule per
// capability keeps the GitHub Security tab grouping intuitive.
func allRules() []rule {
	return []rule{
		{
			ID:               "SKL-SHELL",
			Name:             "ShellCommandDelta",
			ShortDescription: msg{Text: "A skill's shell command surface changed."},
			FullDescription:  msg{Text: "skil-lock detected an added, removed, or modified shell command in a SKILL.md fenced bash block. New shell commands are medium severity by default; protected_paths and require_approval can lift this to high."},
			HelpURI:          HelpURIBase + "#5-detectors",
			Properties:       ruleProperties{Tags: []string{"security", "skill-behavior", "shell"}},
		},
		{
			ID:               "SKL-NETWORK",
			Name:             "NetworkURLDelta",
			ShortDescription: msg{Text: "A skill's outbound network surface changed."},
			FullDescription:  msg{Text: "skil-lock detected an added, removed, or modified URL referenced by a SKILL.md. New URLs whose host is not in allowed_domains are lifted to high severity."},
			HelpURI:          HelpURIBase + "#5-detectors",
			Properties:       ruleProperties{Tags: []string{"security", "skill-behavior", "network"}},
		},
		{
			ID:               "SKL-FILE-READ",
			Name:             "FileReadDelta",
			ShortDescription: msg{Text: "A skill's file-read surface changed."},
			FullDescription:  msg{Text: "skil-lock detected an added, removed, or modified file path read by a SKILL.md. Paths matching protected_paths globs are lifted to high severity."},
			HelpURI:          HelpURIBase + "#5-detectors",
			Properties:       ruleProperties{Tags: []string{"security", "skill-behavior", "file"}},
		},
		{
			ID:               "SKL-FILE-WRITE",
			Name:             "FileWriteDelta",
			ShortDescription: msg{Text: "A skill's file-write surface changed."},
			FullDescription:  msg{Text: "skil-lock detected an added, removed, or modified file path written by a SKILL.md. Paths matching protected_paths globs are lifted to high severity."},
			HelpURI:          HelpURIBase + "#5-detectors",
			Properties:       ruleProperties{Tags: []string{"security", "skill-behavior", "file"}},
		},
		{
			ID:               "SKL-TOOLS",
			Name:             "AllowedToolDelta",
			ShortDescription: msg{Text: "A skill's declared allowed_tools surface changed."},
			FullDescription:  msg{Text: "skil-lock detected a change to a SKILL.md frontmatter allowed_tools list. Low severity by default; metadata, not capability."},
			HelpURI:          HelpURIBase + "#5-detectors",
			Properties:       ruleProperties{Tags: []string{"skill-behavior", "metadata"}},
		},
		{
			ID:               "SKL-SCRIPTS",
			Name:             "BundledScriptDelta",
			ShortDescription: msg{Text: "A skill's bundled scripts surface changed."},
			FullDescription:  msg{Text: "skil-lock detected an added, removed, or modified bundled script referenced by a SKILL.md. Low severity by default; review the script content separately."},
			HelpURI:          HelpURIBase + "#5-detectors",
			Properties:       ruleProperties{Tags: []string{"skill-behavior", "scripts"}},
		},
	}
}

// ruleIDFor maps a capability key to its SARIF rule ID. Unknown
// capabilities fall through to a synthetic ID so emitting a result
// never panics; SkilLock's six capabilities are the closed set in
// practice.
func ruleIDFor(capability string) string {
	switch capability {
	case "shell_commands":
		return "SKL-SHELL"
	case "network_urls":
		return "SKL-NETWORK"
	case "file_reads":
		return "SKL-FILE-READ"
	case "file_writes":
		return "SKL-FILE-WRITE"
	case "allowed_tools":
		return "SKL-TOOLS"
	case "bundled_scripts":
		return "SKL-SCRIPTS"
	}
	return "SKL-OTHER"
}

// buildResults converts each DiffEntry into a SARIF result. Skills are
// resolved against the current lockfile to attach a physicalLocation;
// removed-skill entries (no source path) report at the lockfile root.
func buildResults(d model.Diff, current model.Lockfile) []result {
	out := make([]result, 0, len(d.Entries))
	for _, e := range d.Entries {
		r := result{
			RuleID:  ruleIDFor(e.Capability),
			Level:   levelFor(e.Severity),
			Message: msg{Text: messageFor(e)},
		}
		if loc := locationFor(e, current); loc != nil {
			r.Locations = []location{*loc}
		}
		r.Properties = resultProperties{
			Skill:      e.Skill,
			Capability: e.Capability,
			Change:     string(e.Change),
			Severity:   string(e.Severity),
			Note:       e.Note,
		}
		out = append(out, r)
	}
	return out
}

// levelFor maps SkilLock severities onto SARIF levels. high → error
// puts the finding on the PR review surface; medium → warning shows it
// inline but doesn't fail the SARIF gate; low/info → note keeps it out
// of the way unless someone opens the file. SARIF blocking is set by
// GitHub from level=error; SkilLock's own block decision is independent
// (mode=block + severity>=medium fails the build).
func levelFor(s model.Severity) string {
	switch s {
	case model.SeverityHigh:
		return "error"
	case model.SeverityMedium:
		return "warning"
	case model.SeverityLow, model.SeverityInfo:
		return "note"
	}
	return "note"
}

// messageFor formats the inline annotation text. Mirrors the markdown
// renderer's row shape so reviewers see the same wording in both
// surfaces; the Note column rides at the end when present.
func messageFor(e model.DiffEntry) string {
	verb := string(e.Change)
	cap := e.Capability
	val := e.Value
	if e.Change == model.ChangeModified && e.OldValue != "" {
		val = fmt.Sprintf("%s → %s", e.OldValue, e.Value)
	}
	base := fmt.Sprintf("Skill %q %s %s: %s", e.Skill, verb, cap, val)
	if e.Note != "" {
		return base + " — " + e.Note
	}
	return base
}

// locationFor resolves an entry's physicalLocation by looking up the
// skill in the current lockfile. SARIF paths are repo-relative and
// forward-slash normalized; SkilLock already uses forward slashes
// internally so no replacement is needed.
func locationFor(e model.DiffEntry, current model.Lockfile) *location {
	entry, ok := current.Skills[e.Skill]
	if !ok || entry.SourcePath == "" {
		return nil
	}
	return &location{
		PhysicalLocation: physicalLocation{
			ArtifactLocation: artifactLocation{URI: entry.SourcePath},
		},
	}
}

// --- SARIF v2.1.0 types (minimal subset SkilLock emits) ---

type document struct {
	Schema  string `json:"$schema"`
	Version string `json:"version"`
	Runs    []run  `json:"runs"`
}

type run struct {
	Tool    tool     `json:"tool"`
	Results []result `json:"results"`
}

type tool struct {
	Driver driver `json:"driver"`
}

type driver struct {
	Name           string `json:"name"`
	Version        string `json:"version"`
	InformationURI string `json:"informationUri"`
	Rules          []rule `json:"rules"`
}

type rule struct {
	ID               string         `json:"id"`
	Name             string         `json:"name"`
	ShortDescription msg            `json:"shortDescription"`
	FullDescription  msg            `json:"fullDescription"`
	HelpURI          string         `json:"helpUri"`
	Properties       ruleProperties `json:"properties"`
}

type ruleProperties struct {
	Tags []string `json:"tags"`
}

type result struct {
	RuleID     string           `json:"ruleId"`
	Level      string           `json:"level"`
	Message    msg              `json:"message"`
	Locations  []location       `json:"locations,omitempty"`
	Properties resultProperties `json:"properties"`
}

type resultProperties struct {
	Skill      string `json:"skill"`
	Capability string `json:"capability"`
	Change     string `json:"change"`
	Severity   string `json:"severity"`
	Note       string `json:"note,omitempty"`
}

type location struct {
	PhysicalLocation physicalLocation `json:"physicalLocation"`
}

type physicalLocation struct {
	ArtifactLocation artifactLocation `json:"artifactLocation"`
}

type artifactLocation struct {
	URI string `json:"uri"`
}

type msg struct {
	Text string `json:"text"`
}
