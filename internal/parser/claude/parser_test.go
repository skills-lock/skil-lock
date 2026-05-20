package claude

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/skills-lock/skil-lock/internal/model"
)

func TestParse_PdfExtractor_Happy(t *testing.T) {
	dir := filepath.Join("testdata", "pdf-extractor")
	p, err := Parse(dir)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if p.Identity.Name != "pdf-extractor" {
		t.Errorf("Name: want pdf-extractor, got %q", p.Identity.Name)
	}
	if p.Identity.Version != "1.2.0" {
		t.Errorf("Version: want 1.2.0, got %q", p.Identity.Version)
	}
	if p.Identity.Runtime != model.RuntimeClaude {
		t.Errorf("Runtime: want claude, got %q", p.Identity.Runtime)
	}
	wantSrc := "testdata/pdf-extractor/SKILL.md"
	if p.Identity.SourcePath != wantSrc {
		t.Errorf("SourcePath: want %q, got %q", wantSrc, p.Identity.SourcePath)
	}

	wantTools := []string{"Bash", "Read", "Write"}
	if !equalStringSlices(p.Skill.AllowedTools, wantTools) {
		t.Errorf("AllowedTools: want %v, got %v", wantTools, p.Skill.AllowedTools)
	}

	if len(p.Skill.CodeBlocks) != 1 {
		t.Fatalf("CodeBlocks: want 1 fenced block, got %d", len(p.Skill.CodeBlocks))
	}
	cb := p.Skill.CodeBlocks[0]
	if cb.Language != "bash" {
		t.Errorf("CodeBlock language: want bash, got %q", cb.Language)
	}
	if !strings.Contains(cb.Content, "pdftotext ./input/sample.pdf") {
		t.Errorf("CodeBlock content missing pdftotext command: %q", cb.Content)
	}
	if !strings.Contains(cb.Content, "curl -sSf https://example.com/sample.pdf") {
		t.Errorf("CodeBlock content missing curl command: %q", cb.Content)
	}

	wantScripts := map[string]bool{
		"resources/README.txt": false,
		"scripts/extract.sh":   false,
	}
	for _, s := range p.Skill.Scripts {
		if _, ok := wantScripts[s.RelPath]; !ok {
			t.Errorf("unexpected script RelPath: %q", s.RelPath)
			continue
		}
		wantScripts[s.RelPath] = true
	}
	for path, found := range wantScripts {
		if !found {
			t.Errorf("missing bundled file: %q", path)
		}
	}

	for _, s := range p.Skill.Scripts {
		if s.RelPath == "scripts/extract.sh" && !strings.Contains(s.Content, "pdftotext") {
			t.Errorf("extract.sh content missing pdftotext invocation: %q", s.Content)
		}
	}

	raw, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if err != nil {
		t.Fatalf("read raw SKILL.md: %v", err)
	}
	sum := sha256.Sum256(raw)
	wantHash := "sha256:" + hex.EncodeToString(sum[:])
	if p.ContentHash != wantHash {
		t.Errorf("ContentHash: want %q, got %q", wantHash, p.ContentHash)
	}
}

func TestParse_ScriptsSortedDeterministically(t *testing.T) {
	p, err := Parse(filepath.Join("testdata", "pdf-extractor"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	for i := 1; i < len(p.Skill.Scripts); i++ {
		if p.Skill.Scripts[i-1].RelPath >= p.Skill.Scripts[i].RelPath {
			t.Errorf("Scripts not sorted: %q >= %q at index %d",
				p.Skill.Scripts[i-1].RelPath, p.Skill.Scripts[i].RelPath, i)
		}
	}
}

func TestParse_Minimal_NoBundlesOrCode(t *testing.T) {
	p, err := Parse(filepath.Join("testdata", "minimal"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if p.Identity.Name != "minimal" || p.Identity.Version != "0.1.0" {
		t.Errorf("Identity: %+v", p.Identity)
	}
	if len(p.Skill.AllowedTools) != 0 {
		t.Errorf("AllowedTools should be empty; got %v", p.Skill.AllowedTools)
	}
	if len(p.Skill.CodeBlocks) != 0 {
		t.Errorf("CodeBlocks should be empty; got %d", len(p.Skill.CodeBlocks))
	}
	if len(p.Skill.Scripts) != 0 {
		t.Errorf("Scripts should be empty; got %d", len(p.Skill.Scripts))
	}
}

func TestParse_MissingFile(t *testing.T) {
	_, err := Parse(filepath.Join("testdata", "does-not-exist"))
	if !errors.Is(err, ErrMissingSkillFile) {
		t.Errorf("want ErrMissingSkillFile, got %v", err)
	}
}

func TestParse_NoFrontmatter(t *testing.T) {
	_, err := Parse(filepath.Join("testdata", "no-frontmatter"))
	if !errors.Is(err, ErrMissingFrontmatter) {
		t.Errorf("want ErrMissingFrontmatter, got %v", err)
	}
}

func TestParse_VersionOptional(t *testing.T) {
	// Real-world skills (openai/skills, trailofbits/skills) routinely omit
	// the version field; parser must accept that and emit an empty version.
	p, err := Parse(filepath.Join("testdata", "missing-version"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if p.Identity.Name != "missing-version" {
		t.Errorf("Name: want %q, got %q", "missing-version", p.Identity.Name)
	}
	if p.Identity.Version != "" {
		t.Errorf("Version: want empty, got %q", p.Identity.Version)
	}
}

func TestParse_MissingName(t *testing.T) {
	_, err := Parse(filepath.Join("testdata", "missing-name"))
	if !errors.Is(err, ErrMissingRequiredField) {
		t.Errorf("want ErrMissingRequiredField, got %v", err)
	}
	if !strings.Contains(err.Error(), "name") {
		t.Errorf("error should mention the missing field; got %v", err)
	}
}

func TestParse_MalformedFrontmatter(t *testing.T) {
	_, err := Parse(filepath.Join("testdata", "malformed-frontmatter"))
	if !errors.Is(err, ErrMalformedFrontmatter) {
		t.Errorf("want ErrMalformedFrontmatter, got %v", err)
	}
}

func TestParseAs_OverridesRuntime(t *testing.T) {
	p, err := ParseAs(filepath.Join("testdata", "minimal"), model.RuntimeCodex)
	if err != nil {
		t.Fatalf("ParseAs: %v", err)
	}
	if p.Identity.Runtime != model.RuntimeCodex {
		t.Errorf("Runtime: want %q, got %q", model.RuntimeCodex, p.Identity.Runtime)
	}
}

func TestSplitFrontmatter_AcceptsCRLF(t *testing.T) {
	raw := []byte("---\r\nname: x\r\nversion: \"1.0\"\r\n---\r\nbody\r\n")
	fm, body, err := splitFrontmatter(raw)
	if err != nil {
		t.Fatalf("splitFrontmatter: %v", err)
	}
	if fm["name"] != "x" || fm["version"] != "1.0" {
		t.Errorf("frontmatter wrong: %+v", fm)
	}
	if !strings.Contains(string(body), "body") {
		t.Errorf("body lost: %q", body)
	}
}

func TestStringList_DedupesAndSorts(t *testing.T) {
	got := stringList([]any{"Read", "Bash", "Read", "", "Write"})
	want := []string{"Bash", "Read", "Write"}
	if !equalStringSlices(got, want) {
		t.Errorf("want %v, got %v", want, got)
	}
}

func TestStringList_AcceptsScalar(t *testing.T) {
	got := stringList("Bash")
	if !equalStringSlices(got, []string{"Bash"}) {
		t.Errorf("scalar coercion failed: %v", got)
	}
}

func TestStringList_NilInput(t *testing.T) {
	if got := stringList(nil); got != nil {
		t.Errorf("nil input should yield nil; got %v", got)
	}
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
