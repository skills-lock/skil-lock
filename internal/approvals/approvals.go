// Package approvals parses .skil-lock-approvals.yaml — the override file
// a reviewer commits to mark specific capability deltas as approved —
// and applies it to a model.Diff before the policy layer sees it.
//
// The file format is the inverse of the snippet that internal/diff
// renders into PR comments: the renderer writes a copy-paste block
// pre-filled with skill / delta-key / value, and this package parses
// the same shape back in. A reviewer's path of least resistance is to
// paste the snippet, fill `reviewer` + `reason`, push — and have CI
// re-run green. The paste-to-approve loop is the wedge versus
// exit-code-only scanners.
//
// Filter runs before policy.Apply, so approved deltas are invisible to
// the policy layer (and to the rendered PR comment). Expired approvals
// do *not* drop their delta — the delta resurfaces in the diff with a
// note that the previously-recorded approval has gone stale, so a
// years-old override cannot quietly pin sensitive behavior.
package approvals

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/skills-lock/skil-lock/internal/diff"
	"github.com/skills-lock/skil-lock/internal/model"
)

// FileName is the canonical name of the override file at the repo root.
const FileName = ".skil-lock-approvals.yaml"

// SchemaVersionV01 is the only schema tag accepted by Load. Bumping
// this requires a migration plan; see SPEC.md for the schema definition.
const SchemaVersionV01 = "0.1"

var (
	// ErrMissingApprovals is returned when the override file does not
	// exist. Callers decide whether absence is fatal; `skil-lock ci`
	// treats it as "no overrides yet" and proceeds with the full diff.
	ErrMissingApprovals = errors.New("approvals file not found")

	// ErrUnsupportedSchema is returned when schema_version is missing
	// or not equal to SchemaVersionV01.
	ErrUnsupportedSchema = errors.New("unsupported approvals schema_version")

	// ErrInvalidApproval is returned when an approval entry fails
	// validation (missing required field, multi-key delta, etc.).
	ErrInvalidApproval = errors.New("invalid approval entry")
)

// Approval is one row in the override file. Matches the shape produced
// by internal/diff's copy-paste snippet.
//
// PR, when non-zero, scopes the approval to that pull request: it only
// matches when `skil-lock ci` runs with the same PR number. This closes
// the approval-replay gap — a delta that is approved, reverted, and
// later reintroduced in a different PR re-blocks instead of silently
// riding the stale approval. PR == 0 is a standing approval that
// matches by value alone, as before.
type Approval struct {
	Skill      string            `yaml:"skill"`
	Delta      map[string]string `yaml:"delta"`
	Reviewer   string            `yaml:"reviewer"`
	ReviewedAt time.Time         `yaml:"reviewed_at"`
	Reason     string            `yaml:"reason"`
	ExpiresAt  *time.Time        `yaml:"expires_at,omitempty"`
	PR         int               `yaml:"pr,omitempty"`
}

// file is the on-disk wrapper holding schema_version + the list.
// Hidden because callers should only see []Approval, not the envelope.
type file struct {
	SchemaVersion string     `yaml:"schema_version"`
	Approvals     []Approval `yaml:"approvals"`
}

// Load reads path and returns the parsed approvals.
//
// Errors:
//   - ErrMissingApprovals (wrapped) when path does not exist.
//   - ErrUnsupportedSchema (wrapped) when schema_version is missing
//     or not v0.1.
//   - ErrInvalidApproval (wrapped) when any individual approval fails
//     validation.
//   - A plain parse error on malformed YAML / unknown keys.
//
// An empty (but present) file is treated as zero approvals, not an
// error — useful for teams who want to commit the file as a placeholder
// before their first approval.
func Load(path string) ([]Approval, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%s: %w", path, ErrMissingApprovals)
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)

	var f file
	if err := dec.Decode(&f); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, nil
		}
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	if f.SchemaVersion != SchemaVersionV01 {
		return nil, fmt.Errorf("%s: %w: got %q, want %q",
			path, ErrUnsupportedSchema, f.SchemaVersion, SchemaVersionV01)
	}

	for i, a := range f.Approvals {
		if err := validate(a); err != nil {
			return nil, fmt.Errorf("%s: approval %d: %w", path, i, err)
		}
	}
	return f.Approvals, nil
}

func validate(a Approval) error {
	if a.Skill == "" {
		return fmt.Errorf("%w: skill is required", ErrInvalidApproval)
	}
	if a.Reviewer == "" {
		return fmt.Errorf("%w: reviewer is required (skill=%s)",
			ErrInvalidApproval, a.Skill)
	}
	if a.Reason == "" {
		return fmt.Errorf("%w: reason is required (skill=%s)",
			ErrInvalidApproval, a.Skill)
	}
	switch len(a.Delta) {
	case 0:
		return fmt.Errorf("%w: delta must have exactly one entry (skill=%s)",
			ErrInvalidApproval, a.Skill)
	case 1:
		// happy path — matches the snippet shape
	default:
		return fmt.Errorf("%w: delta must have exactly one entry, got %d (skill=%s)",
			ErrInvalidApproval, len(a.Delta), a.Skill)
	}
	if a.PR < 0 {
		return fmt.Errorf("%w: pr must be a positive pull-request number (skill=%s)",
			ErrInvalidApproval, a.Skill)
	}
	return nil
}

// Filter removes approved deltas from d.Entries.
//
// Matching rule, per entry: an approval matches when approval.Skill ==
// entry.Skill AND approval.Delta[diff.DeltaKey(entry.Capability,
// entry.Change)] == entry.Value (exact string compare — globs are out
// of scope for v0.1; the snippet writes exact values and reviewers
// paste them back unchanged) AND, when the approval carries a non-zero
// PR, currentPR equals it. currentPR == 0 means "no PR context" (a
// local run); PR-scoped approvals never match there — the scope is the
// whole point, and a local re-block simply mirrors what CI on another
// PR would say.
//
// Returns:
//   - filtered: a new Diff with the same BaselinePath/CurrentPath and
//     only the entries that were NOT dropped by a non-expired approval.
//     Entries that match an *expired* approval are kept, with an
//     "approval expired YYYY-MM-DD" note appended so reviewers see
//     why a previously-approved delta resurfaced. Entries whose only
//     value-match is scoped to a different PR are kept with an
//     "approval scoped to PR #N" note for the same reason.
//   - applied: the subset of `as` that matched at least one entry and
//     was not expired. Useful for stderr telemetry in cmd/skil-lock ci.
//   - expired: the subset of `as` whose ExpiresAt is set and before
//     `now`. Reported so a reviewer can decide whether to renew or
//     remove.
//
// Approvals that match no entries (reviewer drift) are not surfaced
// in this return — that's a Phase 4 lint candidate, not a v0.1 wedge
// feature.
func Filter(d model.Diff, as []Approval, now time.Time, currentPR int) (filtered model.Diff, applied []Approval, expired []Approval) {
	filtered = model.Diff{
		BaselinePath: d.BaselinePath,
		CurrentPath:  d.CurrentPath,
	}

	// Pre-compute expiry per approval so we don't time.Before in the inner loop.
	isExpired := make([]bool, len(as))
	for i, a := range as {
		if a.ExpiresAt != nil && a.ExpiresAt.Before(now) {
			isExpired[i] = true
			expired = append(expired, a)
		}
	}

	appliedSet := make(map[int]struct{}, len(as))

	for _, e := range d.Entries {
		key := diff.DeltaKey(e.Capability, e.Change)
		matchIdx, matchExpired, otherPR := findMatch(e, key, as, isExpired, currentPR)

		if matchIdx < 0 {
			if otherPR != 0 {
				// A value-match exists but it is scoped to a different PR.
				// Surface why the delta re-blocked so the reviewer doesn't
				// chase a phantom: the old approval served its PR and is
				// deliberately not transferable.
				note := fmt.Sprintf("approval scoped to PR #%d", otherPR)
				if e.Note == "" {
					e.Note = note
				} else {
					e.Note = e.Note + "; " + note
				}
			}
			filtered.Entries = append(filtered.Entries, e)
			continue
		}

		if matchExpired {
			// Keep the entry but annotate. Severity is left alone — the
			// policy layer (which runs next in cmd/skil-lock ci) will lift
			// it again, and the reviewer sees both the original reason and
			// the expiry.
			a := as[matchIdx]
			expiryNote := fmt.Sprintf("approval expired %s", a.ExpiresAt.UTC().Format("2006-01-02"))
			if e.Note == "" {
				e.Note = expiryNote
			} else {
				e.Note = e.Note + "; " + expiryNote
			}
			filtered.Entries = append(filtered.Entries, e)
			continue
		}

		// Live approval — drop the entry. Record the approval as applied
		// at most once even if it matches multiple rows.
		if _, seen := appliedSet[matchIdx]; !seen {
			appliedSet[matchIdx] = struct{}{}
			applied = append(applied, as[matchIdx])
		}
	}
	return filtered, applied, expired
}

// findMatch looks for the first approval whose (skill, delta key,
// delta value) tuple matches e and whose PR scope (if any) covers
// currentPR. Returns the index into `as`, whether that approval is
// expired, and — when no in-scope match exists — the PR number of a
// value-match scoped to a different PR (0 when none), so the caller
// can annotate the resurfaced delta. Non-expired approvals win over
// expired ones when both match — a renewed approval supersedes the
// stale one.
func findMatch(e model.DiffEntry, key string, as []Approval, isExpired []bool, currentPR int) (idx int, expired bool, otherPR int) {
	bestExpired := -1
	for i, a := range as {
		if a.Skill != e.Skill {
			continue
		}
		v, ok := a.Delta[key]
		if !ok || v != e.Value {
			continue
		}
		if a.PR != 0 && a.PR != currentPR {
			if otherPR == 0 {
				otherPR = a.PR
			}
			continue
		}
		if !isExpired[i] {
			return i, false, 0
		}
		if bestExpired < 0 {
			bestExpired = i
		}
	}
	if bestExpired >= 0 {
		return bestExpired, true, 0
	}
	return -1, false, otherPR
}
