// Package codex parses Codex SKILL.md sources. Codex uses the exact
// same SKILL.md format as Claude Code, so this is a one-line adapter
// over the claude package — only the runtime tag changes.
package codex

import (
	"github.com/skills-lock/skil-lock/internal/model"
	"github.com/skills-lock/skil-lock/internal/parser/claude"
)

// Parse reads <dir>/SKILL.md and returns the parsed surface with
// Runtime = "codex". See claude.Parse for the parsing semantics.
func Parse(dir string) (claude.Parsed, error) {
	return claude.ParseAs(dir, model.RuntimeCodex)
}
