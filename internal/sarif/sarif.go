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
	"strings"

	"github.com/skills-lock/skil-lock/internal/model"
)

// InformationURI is the canonical project URL emitted in the SARIF
// driver block; reviewers click it to learn what SkilLock is.
const InformationURI = "https://github.com/skills-lock/skil-lock"

// HelpURIBase is the prefix appended with each rule ID for the
// per-rule helpUri. Anchors land readers on the spec section that
// defines the capability and severity rules.
const HelpURIBase = "https://github.com/skills-lock/skil-lock/blob/main/SPEC.md"

// astTaxonomyName is the SARIF toolComponent name for the OWASP Agentic
// Skills Top 10 (AST10) taxonomy SkilLock attaches to its findings.
// Rule relationships and per-result taxa reference it by this name so
// GitHub Code Scanning (and any SARIF consumer) can surface the AST risk
// ID alongside each capability delta. This is the alignment convention
// the AST10 maintainers asked for — the project publishes no separate
// SARIF category scheme, so the AST IDs themselves are the categories.
const astTaxonomyName = "OWASP-AST10"

// astInformationURI is the canonical AST10 project page; astHelpURIBase
// is the prefix for each per-risk page (ast01.md … ast10.md).
const (
	astInformationURI = "https://github.com/OWASP/www-project-agentic-skills-top-10"
	astHelpURIBase    = "https://github.com/OWASP/www-project-agentic-skills-top-10/blob/main/"
)

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
			Results:    buildResults(d, current),
			Taxonomies: []taxonomy{astTaxonomy()},
		}},
	}
	return json.MarshalIndent(doc, "", "  ")
}

// ruleDef is the static definition of one capability rule. It is keyed
// by the model capability so the SARIF rule ID, the OWASP AST10
// relationships, and the AST tags are all derived from one place.
type ruleDef struct {
	capability string
	name       string
	short      string
	full       string
	tags       []string
}

// ruleDefs is the closed set of six capability rules SkilLock's
// deterministic detectors emit; one rule per capability keeps the GitHub
// Security tab grouping intuitive.
func ruleDefs() []ruleDef {
	return []ruleDef{
		{
			capability: "shell_commands",
			name:       "ShellCommandDelta",
			short:      "A skill's shell command surface changed.",
			full:       "skil-lock detected an added, removed, or modified shell command in a SKILL.md fenced bash block. New shell commands are medium severity by default; protected_paths and require_approval can lift this to high.",
			tags:       []string{"security", "skill-behavior", "shell"},
		},
		{
			capability: "network_urls",
			name:       "NetworkURLDelta",
			short:      "A skill's outbound network surface changed.",
			full:       "skil-lock detected an added, removed, or modified URL referenced by a SKILL.md. New URLs whose host is not in allowed_domains are lifted to high severity.",
			tags:       []string{"security", "skill-behavior", "network"},
		},
		{
			capability: "file_reads",
			name:       "FileReadDelta",
			short:      "A skill's file-read surface changed.",
			full:       "skil-lock detected an added, removed, or modified file path read by a SKILL.md. Paths matching protected_paths globs are lifted to high severity.",
			tags:       []string{"security", "skill-behavior", "file"},
		},
		{
			capability: "file_writes",
			name:       "FileWriteDelta",
			short:      "A skill's file-write surface changed.",
			full:       "skil-lock detected an added, removed, or modified file path written by a SKILL.md. Paths matching protected_paths globs are lifted to high severity.",
			tags:       []string{"security", "skill-behavior", "file"},
		},
		{
			capability: "allowed_tools",
			name:       "AllowedToolDelta",
			short:      "A skill's declared allowed_tools surface changed.",
			full:       "skil-lock detected a change to a SKILL.md frontmatter allowed_tools list. Low severity by default; metadata, not capability.",
			tags:       []string{"skill-behavior", "metadata"},
		},
		{
			capability: "bundled_scripts",
			name:       "BundledScriptDelta",
			short:      "A skill's bundled scripts surface changed.",
			full:       "skil-lock detected an added, removed, or modified bundled script referenced by a SKILL.md. Low severity by default; review the script content separately.",
			tags:       []string{"skill-behavior", "scripts"},
		},
	}
}

// allRules returns the static rule set, each carrying its OWASP AST10
// relationships and ast tags so consumers can map a SkilLock rule to the
// AST risk(s) it represents without reading the spec.
func allRules() []rule {
	defs := ruleDefs()
	out := make([]rule, 0, len(defs))
	for _, d := range defs {
		tags := append(append([]string{}, d.tags...), astTags(d.capability)...)
		out = append(out, rule{
			ID:               ruleIDFor(d.capability),
			Name:             d.name,
			ShortDescription: msg{Text: d.short},
			FullDescription:  msg{Text: d.full},
			HelpURI:          HelpURIBase + "#5-detectors",
			Properties:       ruleProperties{Tags: tags},
			Relationships:    astRelationships(d.capability),
		})
	}
	return out
}

// astForCapability maps a SkilLock capability key to the OWASP AST10
// risk IDs it represents. Every finding is a delta from an approved
// baseline, so AST07 (Update Drift) applies to all; the first ID is the
// capability-specific risk. This mirrors SPEC §10 and the SkilLock entry
// in the OWASP solutions.md catalog.
func astForCapability(capability string) []string {
	switch capability {
	case "shell_commands", "network_urls", "file_reads", "file_writes":
		// Observed capabilities a skill exercises beyond what it declares.
		return []string{"AST03", "AST07"}
	case "allowed_tools":
		// The declared frontmatter field itself — metadata layer.
		return []string{"AST04", "AST07"}
	case "bundled_scripts":
		// Tampered or added shipped scripts — supply-chain layer.
		return []string{"AST02", "AST07"}
	}
	// Unknown capability: still a drift event.
	return []string{"AST07"}
}

// astTags renders the AST IDs for a capability as GitHub-recognized
// external taxonomy tags (e.g. "external/owasp-ast/ast03").
func astTags(capability string) []string {
	ids := astForCapability(capability)
	tags := make([]string, 0, len(ids))
	for _, id := range ids {
		tags = append(tags, "external/owasp-ast/"+strings.ToLower(id))
	}
	return tags
}

// astRelationships builds the SARIF rule.relationships entries pointing
// each rule at the AST taxa it is relevant to.
func astRelationships(capability string) []relationship {
	ids := astForCapability(capability)
	rels := make([]relationship, 0, len(ids))
	for _, id := range ids {
		rels = append(rels, relationship{
			Target: descriptorRef{ID: id, ToolComponent: toolComponentRef{Name: astTaxonomyName, Index: 0}},
			Kinds:  []string{"relevant"},
		})
	}
	return rels
}

// taxaRefs builds the per-result taxa references so each finding carries
// its AST risk ID(s) directly.
func taxaRefs(capability string) []descriptorRef {
	ids := astForCapability(capability)
	refs := make([]descriptorRef, 0, len(ids))
	for _, id := range ids {
		refs = append(refs, descriptorRef{ID: id, ToolComponent: toolComponentRef{Name: astTaxonomyName, Index: 0}})
	}
	return refs
}

// astTaxonomy returns the OWASP AST10 taxonomy toolComponent emitted
// under run.taxonomies[0]. It defines all ten AST risks so the document
// is self-describing; rules and results reference the subset SkilLock
// addresses.
func astTaxonomy() taxonomy {
	return taxonomy{
		Name:             astTaxonomyName,
		Organization:     "OWASP",
		ShortDescription: msg{Text: "OWASP Agentic Skills Top 10 (AST10) - the ten most critical security risks in agentic AI skills."},
		InformationURI:   astInformationURI,
		IsComprehensive:  true,
		Taxa:             astTaxa(),
	}
}

// astTaxa is the AST01-AST10 risk catalog (names per the AST10 project
// README); helpUri points at each risk's page.
func astTaxa() []taxon {
	defs := []struct{ id, name string }{
		{"AST01", "Malicious Skills"},
		{"AST02", "Supply Chain Compromise"},
		{"AST03", "Over-Privileged Skills"},
		{"AST04", "Insecure Metadata"},
		{"AST05", "Unsafe Deserialization"},
		{"AST06", "Weak Isolation"},
		{"AST07", "Update Drift"},
		{"AST08", "Poor Scanning"},
		{"AST09", "No Governance"},
		{"AST10", "Cross-Platform Reuse"},
	}
	out := make([]taxon, 0, len(defs))
	for _, d := range defs {
		out = append(out, taxon{
			ID:               d.id,
			Name:             d.name,
			ShortDescription: msg{Text: d.name},
			HelpURI:          astHelpURIBase + strings.ToLower(d.id) + ".md",
		})
	}
	return out
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
		r.Taxa = taxaRefs(e.Capability)
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
		return base + " - " + e.Note
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
	Tool       tool       `json:"tool"`
	Results    []result   `json:"results"`
	Taxonomies []taxonomy `json:"taxonomies,omitempty"`
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
	Relationships    []relationship `json:"relationships,omitempty"`
}

type ruleProperties struct {
	Tags []string `json:"tags"`
}

// relationship links a SkilLock rule to a taxon (an OWASP AST10 risk).
type relationship struct {
	Target descriptorRef `json:"target"`
	Kinds  []string      `json:"kinds,omitempty"`
}

// descriptorRef references a reportingDescriptor (here, a taxon) in a
// named toolComponent (the AST10 taxonomy). Reused for result.taxa.
type descriptorRef struct {
	ID            string           `json:"id"`
	ToolComponent toolComponentRef `json:"toolComponent"`
}

type toolComponentRef struct {
	Name  string `json:"name"`
	Index int    `json:"index"`
}

// taxonomy is a SARIF toolComponent holding a set of taxa; SkilLock
// emits one for the OWASP AST10 risk catalog.
type taxonomy struct {
	Name             string  `json:"name"`
	Organization     string  `json:"organization,omitempty"`
	ShortDescription msg     `json:"shortDescription"`
	InformationURI   string  `json:"informationUri,omitempty"`
	IsComprehensive  bool    `json:"isComprehensive"`
	Taxa             []taxon `json:"taxa"`
}

type taxon struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	ShortDescription msg    `json:"shortDescription"`
	HelpURI          string `json:"helpUri,omitempty"`
}

type result struct {
	RuleID     string           `json:"ruleId"`
	Level      string           `json:"level"`
	Message    msg              `json:"message"`
	Locations  []location       `json:"locations,omitempty"`
	Properties resultProperties `json:"properties"`
	Taxa       []descriptorRef  `json:"taxa,omitempty"`
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
