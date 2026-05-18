// Package scan walks a repository, finds Claude Code and Codex skills,
// runs the three v1 detectors against each, and assembles model
// values that the CLI subcommands serialise. It is the orchestrator
// between the parsers (which produce raw textual surfaces) and the
// lockfile (which consumes resolved Behavior).
package scan

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/skills-lock/skil-lock/internal/detector/network"
	"github.com/skills-lock/skil-lock/internal/detector/paths"
	"github.com/skills-lock/skil-lock/internal/detector/shell"
	"github.com/skills-lock/skil-lock/internal/model"
	"github.com/skills-lock/skil-lock/internal/parser/claude"
	"github.com/skills-lock/skil-lock/internal/parser/codex"
)

// SkillRoots are the directories under a repo we walk for skills.
// Order matches the runtime priority (claude first), but the same
// skill directory name appearing in both is reported as two entries
// because they may legitimately differ — the lockfile keys disambiguate.
var SkillRoots = []struct {
	Path    string
	Runtime model.Runtime
}{
	{Path: ".claude/skills", Runtime: model.RuntimeClaude},
	{Path: ".codex/skills", Runtime: model.RuntimeCodex},
}

// Result is one skill's resolved behavior, ready to be turned into a
// LockEntry or serialised as JSON.
type Result struct {
	Identity    model.Identity `json:"identity"`
	Behavior    model.Behavior `json:"behavior"`
	ContentHash string         `json:"content_hash"`
	SourceDir   string         `json:"source_dir"` // skill's own directory (e.g. .claude/skills/x)
}

// Report is the full output of scanning a repo: every detected skill,
// plus any parse-level errors that did not abort the whole run.
type Report struct {
	Skills []Result      `json:"skills"`
	Errors []ScanError   `json:"errors,omitempty"`
}

// ScanError records one non-fatal failure: a single skill's SKILL.md
// could not be parsed. The CLI surfaces these to the reviewer but
// keeps going so one bad skill doesn't break the whole report.
type ScanError struct {
	Path  string `json:"path"`
	Error string `json:"error"`
}

// Repo walks repoRoot and returns the assembled report. repoRoot is
// usually `.`; SourcePath inside results is repo-relative.
func Repo(repoRoot string) (Report, error) {
	abs, err := filepath.Abs(repoRoot)
	if err != nil {
		return Report{}, fmt.Errorf("resolve %s: %w", repoRoot, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return Report{}, fmt.Errorf("stat %s: %w", abs, err)
	}
	if !info.IsDir() {
		return Report{}, fmt.Errorf("%s is not a directory", abs)
	}

	rep := Report{}
	for _, root := range SkillRoots {
		rootAbs := filepath.Join(abs, root.Path)
		if _, err := os.Stat(rootAbs); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return Report{}, fmt.Errorf("stat %s: %w", rootAbs, err)
		}
		entries, err := os.ReadDir(rootAbs)
		if err != nil {
			return Report{}, fmt.Errorf("read %s: %w", rootAbs, err)
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			skillDir := filepath.Join(rootAbs, e.Name())
			repoRel, _ := filepath.Rel(abs, skillDir)
			repoRel = filepath.ToSlash(repoRel)

			parsed, perr := parseFor(skillDir, root.Runtime)
			if perr != nil {
				rep.Errors = append(rep.Errors, ScanError{
					Path:  filepath.ToSlash(filepath.Join(repoRel, claude.SkillFilename)),
					Error: perr.Error(),
				})
				continue
			}
			rep.Skills = append(rep.Skills, build(parsed, repoRel))
		}
	}

	sort.Slice(rep.Skills, func(i, j int) bool {
		if rep.Skills[i].Identity.Name == rep.Skills[j].Identity.Name {
			return rep.Skills[i].Identity.Runtime < rep.Skills[j].Identity.Runtime
		}
		return rep.Skills[i].Identity.Name < rep.Skills[j].Identity.Name
	})
	sort.Slice(rep.Errors, func(i, j int) bool {
		return rep.Errors[i].Path < rep.Errors[j].Path
	})
	return rep, nil
}

// parseFor dispatches to the appropriate runtime parser. Both end up in
// the same Parsed shape; only the Runtime tag differs.
func parseFor(dir string, rt model.Runtime) (claude.Parsed, error) {
	switch rt {
	case model.RuntimeClaude:
		return claude.Parse(dir)
	case model.RuntimeCodex:
		return codex.Parse(dir)
	default:
		return claude.Parsed{}, fmt.Errorf("unknown runtime %q", rt)
	}
}

// build runs all three detectors against one parsed skill and folds
// the results into a model.Behavior. The behavior categories are kept
// in canonical (declaration) order in model.Behavior; we just populate
// them here.
func build(p claude.Parsed, repoRelDir string) Result {
	pathResult := paths.Detect(p.Skill)
	beh := model.Behavior{
		ShellCommands:  shell.Detect(p.Skill),
		NetworkURLs:    network.Detect(p.Skill),
		FileReads:      pathResult.Reads,
		FileWrites:     pathResult.Writes,
		AllowedTools:   p.Skill.AllowedTools,
		BundledScripts: bundledPaths(p.Skill),
	}
	beh = normaliseBehavior(beh)
	id := p.Identity
	id.SourcePath = filepath.ToSlash(filepath.Join(repoRelDir, claude.SkillFilename))
	return Result{
		Identity:    id,
		Behavior:    beh,
		ContentHash: p.ContentHash,
		SourceDir:   repoRelDir,
	}
}

// bundledPaths returns the script paths only — content is dropped
// because the lockfile records identity, not bytes.
func bundledPaths(p claude.ParsedSkill) []string {
	if len(p.Scripts) == 0 {
		return []string{}
	}
	out := make([]string, 0, len(p.Scripts))
	for _, s := range p.Scripts {
		out = append(out, s.RelPath)
	}
	sort.Strings(out)
	return out
}

// normaliseBehavior guarantees every slice is non-nil so the lockfile
// emits `[]` rather than `null` for empty categories. yaml.v3 prints
// nil slices as `null`; reviewers want an empty list shown explicitly.
func normaliseBehavior(b model.Behavior) model.Behavior {
	if b.ShellCommands == nil {
		b.ShellCommands = []string{}
	}
	if b.NetworkURLs == nil {
		b.NetworkURLs = []string{}
	}
	if b.FileReads == nil {
		b.FileReads = []string{}
	}
	if b.FileWrites == nil {
		b.FileWrites = []string{}
	}
	if b.AllowedTools == nil {
		b.AllowedTools = []string{}
	}
	if b.BundledScripts == nil {
		b.BundledScripts = []string{}
	}
	return b
}

// Inventory is the table-style row emitted by `skil-lock list`.
type Inventory struct {
	Name       string
	Runtime    model.Runtime
	Version    string
	SourcePath string
	NumShell   int
	NumURLs    int
	NumPaths   int
}

// Inventories returns a stable-sorted, table-ready summary of the
// scanned skills. Detached from Report so callers can format it as
// table or JSON without re-walking the repo.
func Inventories(r Report) []Inventory {
	out := make([]Inventory, 0, len(r.Skills))
	for _, s := range r.Skills {
		out = append(out, Inventory{
			Name:       s.Identity.Name,
			Runtime:    s.Identity.Runtime,
			Version:    s.Identity.Version,
			SourcePath: s.Identity.SourcePath,
			NumShell:   len(s.Behavior.ShellCommands),
			NumURLs:    len(s.Behavior.NetworkURLs),
			NumPaths:   len(s.Behavior.FileReads) + len(s.Behavior.FileWrites),
		})
	}
	return out
}

