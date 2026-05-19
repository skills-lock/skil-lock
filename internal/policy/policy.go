// Package policy parses .skil-lock.yaml — the user-authored policy file
// that drives the skil-lock ci gate. Distinct from internal/lockfile,
// which round-trips a tool-generated artifact: this file is hand-edited,
// so the loader applies defaults, validates enum fields, and rejects
// unknown keys with a useful error message rather than silently dropping
// typos.
package policy

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"

	"gopkg.in/yaml.v3"

	"github.com/skills-lock/skil-lock/internal/model"
)

// FileName is the canonical name of the policy file at the repo root.
const FileName = ".skil-lock.yaml"

var (
	// ErrMissingPolicy is returned when the policy file does not exist.
	// Callers decide whether to treat this as fatal or fall back to
	// Default() — `skil-lock ci` falls back to warn-mode defaults so
	// first-run adoption is not blocked by a missing config file.
	ErrMissingPolicy = errors.New("policy file not found")

	// ErrInvalidMode is returned when `mode` is set to a value other
	// than "warn" or "block".
	ErrInvalidMode = errors.New("invalid policy mode")

	// ErrInvalidRequireApproval is returned when a require_approval
	// entry is not a recognised capability category.
	ErrInvalidRequireApproval = errors.New("invalid require_approval category")
)

// validRequireApproval is the set of capability categories that can
// appear in `require_approval`. Limited to categories with reviewable
// security meaning; allowed_tools and bundled_scripts are metadata, not
// gateable behavior in their own right.
var validRequireApproval = map[string]struct{}{
	"shell_commands": {},
	"network_urls":   {},
	"file_reads":     {},
	"file_writes":    {},
}

// Default returns a zero-cost-of-adoption policy: warn mode, no
// protected paths, nothing required, no allow-list. Used as the fallback
// when .skil-lock.yaml is absent so the tool installs without a config
// step.
func Default() model.Policy {
	return model.Policy{Mode: model.PolicyModeWarn}
}

// Load reads path and returns the parsed policy with defaults filled
// in. Returns ErrMissingPolicy (wrapped) when path does not exist;
// ErrInvalidMode / ErrInvalidRequireApproval (wrapped) on validation
// failures; a plain parse error when YAML is malformed or contains
// unknown keys.
func Load(path string) (model.Policy, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return model.Policy{}, fmt.Errorf("%s: %w", path, ErrMissingPolicy)
		}
		return model.Policy{}, fmt.Errorf("read %s: %w", path, err)
	}

	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)

	var pol model.Policy
	if err := dec.Decode(&pol); err != nil {
		if errors.Is(err, io.EOF) {
			return Default(), nil
		}
		return model.Policy{}, fmt.Errorf("parse %s: %w", path, err)
	}

	if err := validate(&pol); err != nil {
		return model.Policy{}, fmt.Errorf("%s: %w", path, err)
	}
	return pol, nil
}

func validate(pol *model.Policy) error {
	switch pol.Mode {
	case "":
		pol.Mode = model.PolicyModeWarn
	case model.PolicyModeWarn, model.PolicyModeBlock:
	default:
		return fmt.Errorf("%w: %q (want %q or %q)",
			ErrInvalidMode, pol.Mode, model.PolicyModeWarn, model.PolicyModeBlock)
	}

	for _, cat := range pol.RequireApproval {
		if _, ok := validRequireApproval[cat]; !ok {
			return fmt.Errorf("%w: %q", ErrInvalidRequireApproval, cat)
		}
	}
	return nil
}
