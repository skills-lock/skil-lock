// Package lockfile handles the on-disk skills.lock document: marshaling
// a model.Lockfile to canonical YAML and parsing one back. Determinism is
// load-bearing — the file is committed to git and diffed in pull
// requests; spurious byte-level churn would defeat the review workflow.
//
// Determinism guarantees:
//   - yaml.v3 sorts string-map keys alphabetically, so the order of skills
//     under `skills:` is stable across runs.
//   - Field order within each struct is the YAML tag declaration order in
//     model.LockEntry / model.Behavior — chosen to match MOCKUPS.md §1.
//   - Empty Behavior slices are emitted as `[]` (no `omitempty`) so all six
//     capability categories are always visible to a human reviewer.
package lockfile

import (
	"bytes"
	"errors"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"

	"github.com/skills-lock/skil-lock/internal/model"
)

// IndentSpaces is the YAML indentation used in skills.lock. Two spaces is
// the community convention and keeps PR diffs narrower than yaml.v3's
// 4-space default.
const IndentSpaces = 2

// Header is prepended to every saved lockfile. It is plain YAML comment
// text (not part of the parsed document) and is here so reviewers who
// open the file in their editor see what it is before they read the data.
const Header = `# skills.lock — generated and maintained by skil-lock. Do not hand-edit.
# Commit this file. PR reviewers will see capability deltas inline.
`

// ErrUnsupportedSchema is returned by Load when the file's
// schema_version does not match the version this build understands, and
// by Save when the caller hands in a Lockfile with a non-current
// schema_version. Use errors.Is to check.
var ErrUnsupportedSchema = errors.New("unsupported lockfile schema version")

// Save writes lf to path as canonical YAML, with the SkilLock comment
// header prepended. File is written 0644 — skills.lock is meant to be
// committed, not secret.
func Save(lf model.Lockfile, path string) error {
	if lf.SchemaVersion != model.SchemaVersionV01 {
		return fmt.Errorf("%w: got %q, want %q",
			ErrUnsupportedSchema, lf.SchemaVersion, model.SchemaVersionV01)
	}
	if lf.Skills == nil {
		lf.Skills = map[string]model.LockEntry{}
	}

	var body bytes.Buffer
	enc := yaml.NewEncoder(&body)
	enc.SetIndent(IndentSpaces)
	if err := enc.Encode(lf); err != nil {
		_ = enc.Close()
		return fmt.Errorf("marshal lockfile: %w", err)
	}
	if err := enc.Close(); err != nil {
		return fmt.Errorf("flush lockfile: %w", err)
	}

	out := make([]byte, 0, len(Header)+1+body.Len())
	out = append(out, Header...)
	out = append(out, '\n')
	out = append(out, body.Bytes()...)

	if err := os.WriteFile(path, out, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// Load reads path and returns the parsed Lockfile. It returns
// ErrUnsupportedSchema (wrapped, with the observed and expected versions)
// if schema_version is anything but the current value. Comments in the
// file are discarded by the parser, including the SkilLock header.
func Load(path string) (model.Lockfile, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return model.Lockfile{}, fmt.Errorf("read %s: %w", path, err)
	}

	var lf model.Lockfile
	if err := yaml.Unmarshal(raw, &lf); err != nil {
		return model.Lockfile{}, fmt.Errorf("parse %s: %w", path, err)
	}

	if lf.SchemaVersion != model.SchemaVersionV01 {
		return model.Lockfile{}, fmt.Errorf("%s: %w: got %q, want %q",
			path, ErrUnsupportedSchema, lf.SchemaVersion, model.SchemaVersionV01)
	}
	if lf.Skills == nil {
		lf.Skills = map[string]model.LockEntry{}
	}
	return lf, nil
}
