// Package claude parses Claude Code SKILL.md sources into a raw textual
// surface that detectors mine. It is runtime-aware (sets
// model.Runtime = "claude") but does not classify behavior — that is
// the detectors' job.
//
// The Codex SKILL.md format is identical; the codex package is a thin
// adapter that calls Parse and rewrites the runtime tag.
package claude

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
	"gopkg.in/yaml.v3"

	"github.com/skills-lock/skil-lock/internal/model"
)

// SkillFilename is the on-disk name a SKILL.md folder uses. Both Claude
// Code and Codex use the same filename — that is the whole reason the
// codex adapter is one file.
const SkillFilename = "SKILL.md"

// MaxScriptBytes caps the size of a single bundled script we read into
// memory. Larger files are still recorded by relative path but their
// content is replaced with an empty string so detectors don't OOM on a
// rogue binary blob committed by mistake.
const MaxScriptBytes = 1 << 20 // 1 MiB

// Errors surface as sentinel values so the CLI layer can distinguish a
// missing SKILL.md from a malformed one for exit-code purposes.
var (
	ErrMissingSkillFile     = errors.New("SKILL.md not found")
	ErrMissingFrontmatter   = errors.New("SKILL.md has no YAML frontmatter")
	ErrMalformedFrontmatter = errors.New("SKILL.md frontmatter is malformed")
	ErrMissingRequiredField = errors.New("SKILL.md frontmatter is missing a required field")
)

// CodeBlock is one literal code-fence extracted from the SKILL.md body.
// Plain prose is dropped — detectors only care about executable hints.
type CodeBlock struct {
	Language string // info string lowercased; "" for indented blocks
	Content  string // literal bytes between fences (no leading "```...")
}

// Script is one bundled file under <skill-dir>/scripts/ or
// <skill-dir>/resources/. RelPath is relative to the skill directory.
type Script struct {
	RelPath string
	Content string
}

// ParsedSkill is the parser-local raw textual surface. Detectors mine
// these fields. It is intentionally NOT model.Behavior — the detectors
// are responsible for that mapping (T1.6–T1.8).
type ParsedSkill struct {
	Frontmatter  map[string]any
	Body         string
	AllowedTools []string
	CodeBlocks   []CodeBlock
	Scripts      []Script
}

// Parsed is the full return shape: stable identity, raw textual surface,
// and the SHA-256 of the SKILL.md bytes (for tamper detection in the
// lockfile; not part of ParsedSkill because no detector reads it).
type Parsed struct {
	Identity    model.Identity
	Skill       ParsedSkill
	ContentHash string
}

// Parse reads <dir>/SKILL.md plus any bundled scripts/ and resources/
// files and returns the parsed surface. dir must be the skill's own
// directory (e.g. `.claude/skills/pdf-extractor`), not the containing
// `.claude/skills` root — that's the CLI layer's job to walk.
//
// SourcePath in the returned Identity is dir + "/SKILL.md" as given;
// callers that want a repo-relative path should pass dir in repo-relative
// form.
func Parse(dir string) (Parsed, error) {
	return parseWithRuntime(dir, model.RuntimeClaude)
}

// ParseAs is the runtime-parameterised entry point used by the codex
// adapter. Identical to Parse except the Runtime tag.
func ParseAs(dir string, rt model.Runtime) (Parsed, error) {
	return parseWithRuntime(dir, rt)
}

func parseWithRuntime(dir string, rt model.Runtime) (Parsed, error) {
	skillPath := filepath.Join(dir, SkillFilename)
	raw, err := os.ReadFile(skillPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Parsed{}, fmt.Errorf("%w: %s", ErrMissingSkillFile, skillPath)
		}
		return Parsed{}, fmt.Errorf("read %s: %w", skillPath, err)
	}

	fmMap, body, err := splitFrontmatter(raw)
	if err != nil {
		return Parsed{}, fmt.Errorf("%s: %w", skillPath, err)
	}

	name, err := requiredString(fmMap, "name")
	if err != nil {
		return Parsed{}, fmt.Errorf("%s: %w", skillPath, err)
	}
	version, err := requiredString(fmMap, "version")
	if err != nil {
		return Parsed{}, fmt.Errorf("%s: %w", skillPath, err)
	}

	id := model.Identity{
		Name:       name,
		Version:    version,
		SourcePath: filepath.ToSlash(skillPath),
		Runtime:    rt,
	}

	allowed := stringList(fmMap["allowed-tools"])

	codeBlocks := extractCodeBlocks(body)

	scripts, err := walkBundles(dir)
	if err != nil {
		return Parsed{}, fmt.Errorf("%s: %w", dir, err)
	}

	sum := sha256.Sum256(raw)

	return Parsed{
		Identity: id,
		Skill: ParsedSkill{
			Frontmatter:  fmMap,
			Body:         string(body),
			AllowedTools: allowed,
			CodeBlocks:   codeBlocks,
			Scripts:      scripts,
		},
		ContentHash: "sha256:" + hex.EncodeToString(sum[:]),
	}, nil
}

// splitFrontmatter splits a SKILL.md into its YAML frontmatter map and
// the markdown body. The frontmatter must start at byte 0 with a `---`
// line; CRLF and LF line endings are both accepted.
func splitFrontmatter(raw []byte) (map[string]any, []byte, error) {
	normalised := bytes.ReplaceAll(raw, []byte("\r\n"), []byte("\n"))
	if !bytes.HasPrefix(normalised, []byte("---\n")) && !bytes.Equal(normalised, []byte("---")) {
		return nil, nil, ErrMissingFrontmatter
	}
	rest := normalised[len("---\n"):]
	end := bytes.Index(rest, []byte("\n---\n"))
	if end < 0 {
		// Allow a trailing closing fence with no body after it.
		if bytes.HasSuffix(rest, []byte("\n---")) {
			end = len(rest) - len("\n---")
		} else {
			return nil, nil, ErrMissingFrontmatter
		}
	}
	yamlBytes := rest[:end]
	body := []byte{}
	if end+len("\n---\n") <= len(rest) {
		body = rest[end+len("\n---\n"):]
	}

	var fm map[string]any
	if err := yaml.Unmarshal(yamlBytes, &fm); err != nil {
		return nil, nil, fmt.Errorf("%w: %v", ErrMalformedFrontmatter, err)
	}
	if fm == nil {
		fm = map[string]any{}
	}
	return fm, body, nil
}

func requiredString(fm map[string]any, key string) (string, error) {
	v, ok := fm[key]
	if !ok {
		return "", fmt.Errorf("%w: %q", ErrMissingRequiredField, key)
	}
	// Unquoted YAML scalars that happen to look numeric (e.g. version: 1.0)
	// come back as int/float64. Coerce them so authors don't need to quote.
	var s string
	switch t := v.(type) {
	case string:
		s = t
	case int:
		s = fmt.Sprintf("%d", t)
	case int64:
		s = fmt.Sprintf("%d", t)
	case float64:
		s = fmt.Sprintf("%g", t)
	case bool:
		s = fmt.Sprintf("%v", t)
	default:
		return "", fmt.Errorf("%w: %q must be a scalar", ErrMissingRequiredField, key)
	}
	if strings.TrimSpace(s) == "" {
		return "", fmt.Errorf("%w: %q must be non-empty", ErrMissingRequiredField, key)
	}
	return s, nil
}

// stringList coerces a YAML scalar / sequence value into a sorted,
// deduplicated []string. Non-string elements are skipped.
func stringList(v any) []string {
	if v == nil {
		return nil
	}
	seen := map[string]struct{}{}
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		seen[s] = struct{}{}
	}
	switch t := v.(type) {
	case string:
		add(t)
	case []any:
		for _, e := range t {
			if s, ok := e.(string); ok {
				add(s)
			}
		}
	}
	if len(seen) == 0 {
		return nil
	}
	out := make([]string, 0, len(seen))
	for s := range seen {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// extractCodeBlocks walks the markdown AST and returns every fenced or
// indented code block in document order. Inline code spans are not
// included — detectors should reason about commands and URLs at
// statement granularity, not phrase granularity.
func extractCodeBlocks(body []byte) []CodeBlock {
	if len(body) == 0 {
		return nil
	}
	md := goldmark.New()
	reader := text.NewReader(body)
	doc := md.Parser().Parse(reader)

	var blocks []CodeBlock
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch node := n.(type) {
		case *ast.FencedCodeBlock:
			blocks = append(blocks, CodeBlock{
				Language: strings.ToLower(string(node.Language(body))),
				Content:  joinLines(node.Lines(), body),
			})
			return ast.WalkSkipChildren, nil
		case *ast.CodeBlock:
			blocks = append(blocks, CodeBlock{
				Language: "",
				Content:  joinLines(node.Lines(), body),
			})
			return ast.WalkSkipChildren, nil
		}
		return ast.WalkContinue, nil
	})
	return blocks
}

// joinLines concatenates the line segments of a block node into a
// single string. goldmark does not expose a stable Text() method on
// code blocks — the canonical way to read fence contents is to walk
// Lines() and slice the source.
func joinLines(segs *text.Segments, source []byte) string {
	if segs == nil {
		return ""
	}
	var b strings.Builder
	for i := 0; i < segs.Len(); i++ {
		seg := segs.At(i)
		b.Write(seg.Value(source))
	}
	return b.String()
}

// walkBundles reads scripts/ and resources/ under the skill directory.
// Missing directories are not an error — most skills don't ship bundled
// files. Returned entries are sorted by RelPath for deterministic
// downstream hashing and diffs.
func walkBundles(dir string) ([]Script, error) {
	var scripts []Script
	for _, sub := range []string{"scripts", "resources"} {
		root := filepath.Join(dir, sub)
		info, err := os.Stat(root)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("stat %s: %w", root, err)
		}
		if !info.IsDir() {
			continue
		}
		err = filepath.WalkDir(root, func(path string, d fs.DirEntry, werr error) error {
			if werr != nil {
				return werr
			}
			if d.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(dir, path)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			content, err := readCapped(path)
			if err != nil {
				return err
			}
			scripts = append(scripts, Script{RelPath: rel, Content: content})
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Slice(scripts, func(i, j int) bool { return scripts[i].RelPath < scripts[j].RelPath })
	return scripts, nil
}

// readCapped reads up to MaxScriptBytes from path; if the file is
// larger it returns the empty string. Detectors then see the path but
// no content — preferable to refusing the whole parse over one large
// asset.
func readCapped(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("stat %s: %w", path, err)
	}
	if info.Size() > MaxScriptBytes {
		return "", nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	return string(b), nil
}
