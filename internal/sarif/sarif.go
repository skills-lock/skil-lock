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
	"regexp"
	"strings"

	"github.com/skills-lock/skil-lock/internal/model"
)

// InformationURI is the canonical project URL emitted in the SARIF
// driver block; reviewers click it to learn what SkilLock is.
const InformationURI = "https://github.com/skills-lock/skil-lock"

// specFragment values are the SPEC.md anchors SkilLock emits into the
// document. They are read off the rendered headings rather than derived
// from the heading text, because GitHub's slug algorithm drops the
// section numbers' periods ("### 14.5 How to read a clean run" becomes
// "#145-how-to-read-a-clean-run"). A pointer that lands on the right
// document with the wrong fragment is barely better than no pointer, so
// TestSpecAnchorsResolve re-derives every anchor from SPEC.md itself and
// fails the build if one stops resolving.
const (
	// fragmentBehaviorFields defines the capability fields each rule
	// reports on.
	fragmentBehaviorFields = "#61-behavior-fields"
	// fragmentCleanRun is the interpretationUri target: what a run with
	// no findings does and does not claim.
	fragmentCleanRun = "#145-how-to-read-a-clean-run"
)

// releaseVersion matches the version strings that correspond to a real
// git tag in this repo (the tag is the version with a "v" prefix).
// Development builds carry "dev" or a `git describe` string like
// "0.2.4-9-gc0ebb91", neither of which is a tag, so they fall back to
// main rather than emitting a URL that 404s.
var releaseVersion = regexp.MustCompile(`^\d+\.\d+\.\d+(-rc\d+)?$`)

// specURI builds a link into this repo's SPEC.md pinned to the ref the
// running binary was built from.
//
// Pinning is the point. A consumer reading a two-year-old report needs
// the prose as it stood for the version that emitted it; a link to main
// hands them today's text, which may describe bounds the emitting
// version did not have (or omit ones it did). The same argument applies
// to every document link in the report, so rule helpUris are pinned too,
// not just the interpretation pointer.
func specURI(version, fragment string) string {
	ref := "main"
	if releaseVersion.MatchString(version) {
		ref = "v" + version
	}
	return InformationURI + "/blob/" + ref + "/SPEC.md" + fragment
}

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

// Unanalysed records one skill that was discovered but never analysed —
// its SKILL.md failed to parse. Path is the repo-relative SKILL.md path;
// Reason is the parser's message. These are the inputs the run was
// handed and could not turn into findings.
type Unanalysed struct {
	Path   string
	Reason string
}

// Completeness is a run's statement about what it actually analysed,
// and it is a required argument to Render on purpose.
//
// The failure this exists to prevent: a skill whose SKILL.md fails to
// parse drops out of the scan, and a report that says nothing about it
// is byte-identical to a report from a run where every skill parsed
// cleanly. The tool knew, and the knowledge never reached the artifact.
// A consumer merging this report with others (see SPEC §14.3) then
// reads "no drift findings" as "capability surface unchanged", which is
// a claim this run is not entitled to make about a skill it never read.
//
// Making it a parameter rather than an option means a caller cannot
// render a document that implies completeness by omission: to emit a
// report at all you must state what you analysed, even when the answer
// is "everything". That is the run-level completeness declaration
// SkilLock argued for in the multi-scanner envelope RFC — silence about
// bounds becomes a claim rather than an absence.
type Completeness struct {
	// Discovered is the number of skill directories the scan walked.
	Discovered int
	// Analysed is the number that parsed and contributed behavior.
	Analysed int
	// Unanalysed lists the skills that failed to parse, in scan order.
	Unanalysed []Unanalysed
}

// Complete returns a Completeness for a run that analysed every skill it
// discovered. Use it only when that is actually true.
func Complete(analysed int) Completeness {
	return Completeness{Discovered: analysed, Analysed: analysed}
}

// Render returns the SARIF v2.1.0 JSON document for diff d. The
// current lockfile is used to resolve each skill's SKILL.md path; an
// entry referencing a skill missing from current (a removed skill) is
// reported without a physicalLocation. version is the running CLI
// version string ("0.1.0", "dev", etc.) emitted in driver.version.
// comp states what the run analysed; see Completeness.
func Render(d model.Diff, current model.Lockfile, version string, comp Completeness) ([]byte, error) {
	arts, idx := buildArtifacts(d, current)
	doc := document{
		Schema:  "https://raw.githubusercontent.com/oasis-tcs/sarif-spec/main/Documents/CommitteeSpecifications/2.1.0/sarif-schema-2.1.0.json",
		Version: "2.1.0",
		Runs: []run{{
			Tool: tool{
				Driver: driver{
					Name:           "skil-lock",
					Version:        version,
					InformationURI: InformationURI,
					Rules:          allRules(version),
				},
			},
			Invocations: []invocation{completedInvocation(comp)},
			Artifacts:   arts,
			Results:     buildResults(d, current, idx),
			Taxonomies:  []taxonomy{astTaxonomy()},
			Properties: &runProperties{
				Completeness:      completenessProperties(comp),
				InterpretationURI: specURI(version, fragmentCleanRun),
			},
		}},
	}
	return json.MarshalIndent(doc, "", "  ")
}

// RenderFailure returns a SARIF document for a run that could not
// analyse anything — the scan itself failed (unreadable skill root, I/O
// error) rather than one skill failing to parse.
//
// This is the failure channel required by §4 of the multi-scanner
// envelope profile: a run that did not complete MUST say so in a fixed
// place, at level "error", rather than exiting non-zero with no
// artifact. Without it a failed run and a clean run are distinguishable
// only by exit code, which is lost the moment the report is uploaded
// and merged. executionSuccessful is false here and only here: a skill
// that failed to parse leaves executionSuccessful true, because the
// analysis did complete — it just covered less than it was handed.
func RenderFailure(version, reason string) ([]byte, error) {
	doc := document{
		Schema:  "https://raw.githubusercontent.com/oasis-tcs/sarif-spec/main/Documents/CommitteeSpecifications/2.1.0/sarif-schema-2.1.0.json",
		Version: "2.1.0",
		Runs: []run{{
			Tool: tool{
				Driver: driver{
					Name:           "skil-lock",
					Version:        version,
					InformationURI: InformationURI,
					Rules:          allRules(version),
				},
			},
			Invocations: []invocation{{
				ExecutionSuccessful: false,
				ToolExecutionNotifications: []notification{{
					Level:   levelError,
					Message: msg{Text: "skil-lock could not complete the scan: " + reason},
					Properties: &notificationProperties{
						SkilLockKind: kindScanFailed,
					},
				}},
			}},
			Results:    []result{},
			Taxonomies: []taxonomy{astTaxonomy()},
			Properties: &runProperties{
				Completeness: &completenessProps{
					ResultsBounded: false,
					Basis:          basisNotAnalysed,
					Discovered:     0,
					Analysed:       0,
					Unanalysed:     0,
				},
				InterpretationURI: specURI(version, fragmentCleanRun),
			},
		}},
	}
	return json.MarshalIndent(doc, "", "  ")
}

// completedInvocation builds the invocations[0] entry for a run that
// finished. executionSuccessful stays true even when skills failed to
// parse: the analysis ran to completion over what it could read, and
// the results it produced are valid. Each unanalysed skill gets its own
// warning-level notification so a consumer can name the file, not just
// count it — SARIF §3.58.6 defines warning as "the analysis might be
// incomplete but the results that were generated are probably valid",
// which is exactly this state.
func completedInvocation(comp Completeness) invocation {
	inv := invocation{ExecutionSuccessful: true}
	for _, u := range comp.Unanalysed {
		// Parser errors are prefixed with the file they concern, which
		// path already carries; drop the duplicate so the message reads
		// as one sentence rather than naming the file twice.
		reason := strings.TrimPrefix(u.Reason, u.Path+": ")
		inv.ToolExecutionNotifications = append(inv.ToolExecutionNotifications, notification{
			Level: levelWarning,
			Message: msg{Text: fmt.Sprintf(
				"%s was not analysed (%s); its capability surface is unknown and absent from this report",
				u.Path, reason)},
			Properties: &notificationProperties{
				SkilLockKind: kindSkillNotAnalysed,
				Path:         u.Path,
				Reason:       reason,
			},
		})
	}
	return inv
}

// completenessProperties renders the run-level declaration. It is
// emitted on every run, including fully complete ones, which is the
// whole point: a consumer that sees no declaration cannot tell "nothing
// was bounded" from "something was and the tool didn't say".
//
// Deliberately absent: appliedCap. In the envelope RFC that key
// declares a cap on the results array, and SkilLock has none — one
// result per diff entry, no bound. Declaring appliedCap here would tell
// a consumer the result set may be truncated when it is whole.
//
// Also deliberately absent: droppedCount. An unparseable skill is not a
// finding held back, it is an input never read, and reporting it under
// a key that means "findings withheld" would publish a number that does
// not mean what the key says. The membership counts below carry it
// honestly instead.
func completenessProperties(comp Completeness) *completenessProps {
	basis := basisComplete
	if len(comp.Unanalysed) > 0 {
		basis = basisPartial
	}
	return &completenessProps{
		ResultsBounded: false,
		Basis:          basis,
		Discovered:     comp.Discovered,
		Analysed:       comp.Analysed,
		Unanalysed:     len(comp.Unanalysed),
	}
}

// buildArtifacts returns the run.artifacts[] array — one entry per distinct
// SKILL.md referenced by a result whose skill carries a content hash in the
// current lockfile — plus a sourcePath→index map so result locations can point
// at each artifact. The sha-256 digest binds every finding to the exact skill
// content it was raised against (SARIF's standard artifactLocation + hashes
// pattern). This is what makes a drift finding self-invalidating: once a
// finding is pinned to the content hash, it no longer applies the moment the
// SKILL.md changes. A skill with no resolvable content hash (e.g. a removed
// skill, absent from current) contributes no artifact.
func buildArtifacts(d model.Diff, current model.Lockfile) ([]artifact, map[string]int) {
	idx := map[string]int{}
	var arts []artifact
	for _, e := range d.Entries {
		entry, ok := current.Skills[e.Skill]
		if !ok || entry.SourcePath == "" || entry.ContentHash == "" {
			continue
		}
		if _, seen := idx[entry.SourcePath]; seen {
			continue
		}
		idx[entry.SourcePath] = len(arts)
		arts = append(arts, artifact{
			Location: artifactLocation{URI: entry.SourcePath},
			Hashes:   &hashes{SHA256: normalizeSHA256(entry.ContentHash)},
		})
	}
	return arts, idx
}

// normalizeSHA256 renders a lockfile content hash ("sha256:" + 64 hex) as the
// bare lowercase 64-char hex SARIF expects under hashes["sha-256"].
func normalizeSHA256(h string) string {
	return strings.TrimPrefix(strings.ToLower(h), "sha256:")
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
func allRules(version string) []rule {
	defs := ruleDefs()
	help := specURI(version, fragmentBehaviorFields)
	out := make([]rule, 0, len(defs))
	for _, d := range defs {
		tags := append(append([]string{}, d.tags...), astTags(d.capability)...)
		out = append(out, rule{
			ID:               ruleIDFor(d.capability),
			Name:             d.name,
			ShortDescription: msg{Text: d.short},
			FullDescription:  msg{Text: d.full},
			HelpURI:          help,
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
func buildResults(d model.Diff, current model.Lockfile, idx map[string]int) []result {
	out := make([]result, 0, len(d.Entries))
	for _, e := range d.Entries {
		r := result{
			RuleID:  ruleIDFor(e.Capability),
			Level:   levelFor(e.Severity),
			Message: msg{Text: messageFor(e)},
		}
		if loc := locationFor(e, current, idx); loc != nil {
			r.Locations = []location{*loc}
		}
		r.Properties = resultProperties{
			Layer:      layerDrift,
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
func locationFor(e model.DiffEntry, current model.Lockfile, idx map[string]int) *location {
	entry, ok := current.Skills[e.Skill]
	if !ok || entry.SourcePath == "" {
		return nil
	}
	al := artifactLocation{URI: entry.SourcePath}
	if i, ok := idx[entry.SourcePath]; ok {
		al.Index = &i
	}
	return &location{
		PhysicalLocation: physicalLocation{ArtifactLocation: al},
	}
}

// --- SARIF v2.1.0 types (minimal subset SkilLock emits) ---

type document struct {
	Schema  string `json:"$schema"`
	Version string `json:"version"`
	Runs    []run  `json:"runs"`
}

type run struct {
	Tool        tool           `json:"tool"`
	Invocations []invocation   `json:"invocations,omitempty"`
	Artifacts   []artifact     `json:"artifacts,omitempty"`
	Results     []result       `json:"results"`
	Taxonomies  []taxonomy     `json:"taxonomies,omitempty"`
	Properties  *runProperties `json:"properties,omitempty"`
}

// invocation is the SARIF run.invocations[] entry. SkilLock emits
// exactly one: a scan is a single invocation of the tool.
type invocation struct {
	ExecutionSuccessful        bool           `json:"executionSuccessful"`
	ToolExecutionNotifications []notification `json:"toolExecutionNotifications,omitempty"`
}

// notification is a toolExecutionNotifications entry — a statement
// about the run itself rather than about the code being analysed.
type notification struct {
	Level      string                  `json:"level"`
	Message    msg                     `json:"message"`
	Properties *notificationProperties `json:"properties,omitempty"`
}

// SARIF notification levels SkilLock emits. error means the run did not
// complete (§3.20.21); warning means the analysis may be incomplete but
// the results produced are probably valid (§3.58.6).
const (
	levelError   = "error"
	levelWarning = "warning"
)

// notification kinds, namespaced so a merged multi-scanner report can
// tell SkilLock's notifications apart from a sibling tool's.
const (
	kindSkillNotAnalysed = "skill-not-analysed"
	kindScanFailed       = "scan-failed"
)

type notificationProperties struct {
	// SkilLockKind names what the notification is about, so a consumer
	// can branch without parsing the message text.
	SkilLockKind string `json:"skilLockKind"`
	Path         string `json:"path,omitempty"`
	Reason       string `json:"reason,omitempty"`
}

// runProperties carries the run-level declarations: what this run
// covered, and where to read what a clean result from it means.
type runProperties struct {
	Completeness *completenessProps `json:"completeness,omitempty"`
	// InterpretationURI points at the prose stating how to read a run
	// with no findings. It is a pointer, not a declaration: it quantifies
	// nothing about coverage, so unlike a numeric field it cannot lie
	// about coverage — which is what makes it usable for bounds that live
	// inside the detection logic and cannot be honestly measured.
	//
	// Run level, not rule level, and that placement is forced. Only rules
	// that fired appear in tool.driver.rules, so a clean run carries zero
	// rule objects: a caveat hung off reportingDescriptor.helpUri is
	// absent from exactly the report most likely to be read as an
	// all-clear. Nor informationUri, which SARIF §3.19.3 defines as
	// information about the tool and consumers render as a homepage.
	//
	// Emitted unconditionally, including on failure runs, for the same
	// reason the completeness block is: a pointer that appears only when
	// something is wrong teaches consumers to read its absence as an
	// assurance.
	//
	// The key name is ppcvote's spelling from the envelope RFC thread,
	// adopted verbatim rather than re-coined. Convergence on one name
	// across independent emitters is worth more here than a marginally
	// better one, and the name was offered explicitly as not-a-proposal.
	InterpretationURI string `json:"interpretationUri,omitempty"`
}

// completeness basis values: what kind of statement this run is making
// about its own coverage.
const (
	// basisComplete: every discovered skill was analysed.
	basisComplete = "complete"
	// basisPartial: at least one discovered skill was not analysed;
	// see the warning notifications for which and why.
	basisPartial = "partial"
	// basisNotAnalysed: the scan itself failed; nothing was analysed.
	basisNotAnalysed = "not-analysed"
)

// completenessProps is SkilLock's run-level completeness declaration.
//
// SkilLock is a set-valued emitter: one run covers every skill in a
// repo, and its results do not enumerate their own inputs. A
// per-artifact digest cannot recover membership — a skill that failed
// to parse has no digest, no result, and no absence anywhere in the
// report — so the count of what was discovered versus analysed is the
// only thing that makes the gap visible.
type completenessProps struct {
	// ResultsBounded reports whether the results array was capped.
	// SkilLock never caps it (one result per diff entry), and says so
	// explicitly rather than by omission.
	ResultsBounded bool   `json:"resultsBounded"`
	Basis          string `json:"basis"`
	// Discovered, Analysed and Unanalysed are skill counts, not finding
	// counts: Discovered = Analysed + Unanalysed.
	Discovered int `json:"skillsDiscovered"`
	Analysed   int `json:"skillsAnalysed"`
	Unanalysed int `json:"skillsUnanalysed"`
}

// artifact is a SARIF run.artifacts[] entry: the scanned SKILL.md identified by
// path and SHA-256 content digest. results reference it by index.
type artifact struct {
	Location artifactLocation `json:"location"`
	Hashes   *hashes          `json:"hashes,omitempty"`
}

// hashes carries the SARIF-standard content digest. SkilLock emits sha-256
// (the lockfile content hash) so findings dedupe and bind to exact content.
type hashes struct {
	SHA256 string `json:"sha-256"`
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
	// Layer is the scan-report-envelope discriminator (SPEC §14.3): it
	// tells a multi-scanner consumer which class of tool produced the
	// finding so reports merged on one content digest stay attributable.
	// Every SkilLock finding is a baseline-to-current capability delta, so
	// the layer is always "drift".
	Layer      string `json:"layer"`
	Skill      string `json:"skill"`
	Capability string `json:"capability"`
	Change     string `json:"change"`
	Severity   string `json:"severity"`
	Note       string `json:"note,omitempty"`
}

// layerDrift is the envelope layer SkilLock emits — it is a drift scanner:
// every result is a delta from an approved baseline. Other layers a
// consumer may see from sibling tools are "content" (static content scan)
// and "atr" (agentic-threat rules); see SPEC §14.3.
const layerDrift = "drift"

type location struct {
	PhysicalLocation physicalLocation `json:"physicalLocation"`
}

type physicalLocation struct {
	ArtifactLocation artifactLocation `json:"artifactLocation"`
}

type artifactLocation struct {
	URI string `json:"uri"`
	// Index points into run.artifacts[] so a result's location resolves to the
	// artifact carrying its sha-256 digest. Pointer so index 0 is emitted
	// (a plain int with omitempty would drop the valid zero index).
	Index *int `json:"index,omitempty"`
}

type msg struct {
	Text string `json:"text"`
}
