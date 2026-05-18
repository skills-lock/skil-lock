// Package model holds the runtime-agnostic domain types for SkilLock.
//
// Parsers populate Skill values; detectors read Behavior; the diff engine
// compares two Lockfile values; the CI gate evaluates a Policy. Nothing
// in this package does I/O.
package model

import "time"

// Runtime identifies the agent runtime that owns a skill's source format.
type Runtime string

const (
	RuntimeClaude Runtime = "claude"
	RuntimeCodex  Runtime = "codex"
)

// Identity is the stable identity of a skill discovered in a repo.
type Identity struct {
	Name       string  `yaml:"name,omitempty"`
	Version    string  `yaml:"version,omitempty"`
	SourcePath string  `yaml:"source_path"`
	Runtime    Runtime `yaml:"runtime"`
}

// Behavior is the approved capability surface of a skill. Each slice is a
// sorted set of distinct values; detectors are responsible for normalisation
// before populating these fields. All six categories are always emitted in
// skills.lock (empty as `[]`) so reviewers see the full surface explicitly.
type Behavior struct {
	ShellCommands  []string `yaml:"shell_commands"`
	NetworkURLs    []string `yaml:"network_urls"`
	FileReads      []string `yaml:"file_reads"`
	FileWrites     []string `yaml:"file_writes"`
	AllowedTools   []string `yaml:"allowed_tools"`
	BundledScripts []string `yaml:"bundled_scripts"`
}

// LockEntry is one skill's row in skills.lock. Fields are declared in the
// canonical on-disk order (runtime → source_path → version → content_hash →
// behavior); Name is not a field because the surrounding map key carries it.
type LockEntry struct {
	Runtime     Runtime  `yaml:"runtime"`
	SourcePath  string   `yaml:"source_path"`
	Version     string   `yaml:"version"`
	ContentHash string   `yaml:"content_hash"`
	Behavior    Behavior `yaml:"behavior"`
}

// NewLockEntry builds an entry from an Identity (parser output) + the
// content hash + behavior. Drops Identity.Name — it becomes the map key.
func NewLockEntry(id Identity, contentHash string, b Behavior) LockEntry {
	return LockEntry{
		Runtime:     id.Runtime,
		SourcePath:  id.SourcePath,
		Version:     id.Version,
		ContentHash: contentHash,
		Behavior:    b,
	}
}

// Lockfile is the on-disk skills.lock document.
type Lockfile struct {
	SchemaVersion string               `yaml:"schema_version"`
	GeneratedAt   time.Time            `yaml:"generated_at"`
	GeneratedBy   string               `yaml:"generated_by"`
	Skills        map[string]LockEntry `yaml:"skills"`
}

// SchemaVersionV01 is the current lockfile schema tag.
const SchemaVersionV01 = "0.1"

// NewLockfile returns an empty lockfile with metadata initialised.
// The timestamp is normalised to UTC and truncated to the second so
// re-runs on the same wall clock produce the same file — sub-second
// noise in skills.lock would defeat the PR-diff review workflow.
func NewLockfile(generatedBy string, generatedAt time.Time) Lockfile {
	return Lockfile{
		SchemaVersion: SchemaVersionV01,
		GeneratedAt:   generatedAt.UTC().Truncate(time.Second),
		GeneratedBy:   generatedBy,
		Skills:        map[string]LockEntry{},
	}
}

// PolicyMode controls whether capability deltas fail the build.
type PolicyMode string

const (
	PolicyModeWarn  PolicyMode = "warn"
	PolicyModeBlock PolicyMode = "block"
)

// Policy is the contents of .skil-lock.yaml. Defaults are filled in by the
// loader, not this package.
type Policy struct {
	Mode            PolicyMode `yaml:"mode"`
	ProtectedPaths  []string   `yaml:"protected_paths,omitempty"`
	RequireApproval []string   `yaml:"require_approval,omitempty"`
	AllowedDomains  []string   `yaml:"allowed_domains,omitempty"`
}

// Severity is the reviewer-facing classification of a delta.
type Severity string

const (
	SeverityInfo   Severity = "info"
	SeverityLow    Severity = "low"
	SeverityMedium Severity = "medium"
	SeverityHigh   Severity = "high"
)

// ChangeType is the kind of delta a DiffEntry represents.
type ChangeType string

const (
	ChangeAdded    ChangeType = "added"
	ChangeRemoved  ChangeType = "removed"
	ChangeModified ChangeType = "modified"
)

// DiffEntry is one row of a SecurityDiff: a single capability change on a
// single skill.
type DiffEntry struct {
	Skill      string     // skill name as keyed in Lockfile.Skills
	Capability string     // "shell_commands", "network_urls", etc.
	Change     ChangeType // added / removed / modified
	Value      string     // the value that was added/removed; for modified, the new value
	OldValue   string     // for modified only: the previous value
	Severity   Severity
	Note       string // free-text annotation (e.g. "matches protected_paths")
}

// Diff is the result of comparing two lockfiles. Entries are sorted by
// (Skill, Capability, Value) before rendering.
type Diff struct {
	BaselinePath string
	CurrentPath  string
	Entries      []DiffEntry
}

// HasBlocking returns true if any entry's severity is at or above the given
// threshold. Used by `skil-lock ci` to decide exit status.
func (d Diff) HasBlocking(threshold Severity) bool {
	rank := func(s Severity) int {
		switch s {
		case SeverityHigh:
			return 4
		case SeverityMedium:
			return 3
		case SeverityLow:
			return 2
		case SeverityInfo:
			return 1
		default:
			return 0
		}
	}
	min := rank(threshold)
	for _, e := range d.Entries {
		if rank(e.Severity) >= min {
			return true
		}
	}
	return false
}
