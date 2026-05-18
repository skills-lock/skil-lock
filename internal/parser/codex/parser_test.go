package codex

import (
	"path/filepath"
	"testing"

	"github.com/skills-lock/skil-lock/internal/model"
)

func TestParse_StampsCodexRuntime(t *testing.T) {
	// Reuses the claude package's pdf-extractor fixture via a relative
	// testdata path. The point is the runtime tag, not file content.
	dir := filepath.Join("..", "claude", "testdata", "pdf-extractor")
	p, err := Parse(dir)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if p.Identity.Runtime != model.RuntimeCodex {
		t.Errorf("Runtime: want %q, got %q", model.RuntimeCodex, p.Identity.Runtime)
	}
	if p.Identity.Name != "pdf-extractor" || p.Identity.Version != "1.2.0" {
		t.Errorf("Identity not preserved: %+v", p.Identity)
	}
}
