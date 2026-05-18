// Package paths extracts file path references from a parsed skill and
// splits them into reads vs writes by lightweight heuristic on the
// surrounding shell context. The policy layer is what decides whether
// any of those paths intersect `protected_paths` — this package only
// surfaces what the skill touches.
//
// Heuristic boundaries:
//   - `> file` / `>> file` / `tee file` → write.
//   - `< file` and arguments to read-only commands (cat, grep, head,
//     tail, less, more, diff, wc, awk, cut, sort, file) → read.
//   - Arguments to read+write commands (sed -i, cp, mv) → write
//     conservatively, since a reviewer cares more about writes.
//   - Glob patterns (`./input/*.pdf`) are kept verbatim — the policy
//     glob-matcher handles `**` and `*` matching at lookup time.
package paths

import (
	"regexp"
	"sort"
	"strings"

	"github.com/skills-lock/skil-lock/internal/parser/claude"
)

// pathLike matches things that look like relative or absolute paths or
// shell globs. Plain words like "bash" or "name" are rejected because
// they have no separator and no glob marker.
var pathLike = regexp.MustCompile(`(?:\.{1,2}/|/)[A-Za-z0-9._*?/\-]+|[A-Za-z0-9._-]+\.[A-Za-z0-9]+|\*\*?/[A-Za-z0-9._*?/\-]+|[A-Za-z0-9._-]*\*[A-Za-z0-9._*?/\-]*`)

// readOnlyCmds: arguments after the verb are likely reads.
var readOnlyCmds = map[string]struct{}{
	"cat": {}, "grep": {}, "egrep": {}, "fgrep": {}, "head": {}, "tail": {},
	"less": {}, "more": {}, "wc": {}, "file": {}, "stat": {},
	"awk": {}, "cut": {}, "sort": {}, "uniq": {}, "diff": {}, "cmp": {},
	"jq": {}, "yq": {}, "xmllint": {}, "pdftotext": {},
}

// writeCmds: arguments are likely writes (or read+write — we surface
// them on the write side so a reviewer sees the side-effect).
var writeCmds = map[string]struct{}{
	"tee": {}, "touch": {}, "mkdir": {}, "rm": {}, "rmdir": {},
	"mv": {}, "cp": {}, "install": {}, "ln": {},
	"truncate": {}, "shred": {},
}

// Reads + Writes are the two halves of the result. Each is sorted and
// deduplicated. Suitable for model.Behavior.FileReads / FileWrites.
type Result struct {
	Reads  []string
	Writes []string
}

// Detect walks the parsed skill and returns split reads/writes.
func Detect(p claude.ParsedSkill) Result {
	reads := map[string]struct{}{}
	writes := map[string]struct{}{}

	addAll := func(m map[string]struct{}, vs []string) {
		for _, v := range vs {
			v = strings.TrimSpace(v)
			v = strings.Trim(v, `"'`)
			if v == "" {
				continue
			}
			if !looksLikePath(v) {
				continue
			}
			m[v] = struct{}{}
		}
	}

	for _, cb := range p.CodeBlocks {
		r, w := scanShellSource(cb.Content)
		addAll(reads, r)
		addAll(writes, w)
	}
	for _, s := range p.Scripts {
		if !isShellScriptName(s.RelPath) && !strings.HasPrefix(s.Content, "#!") {
			continue
		}
		r, w := scanShellSource(s.Content)
		addAll(reads, r)
		addAll(writes, w)
	}

	return Result{
		Reads:  sortedKeys(reads),
		Writes: sortedKeys(writes),
	}
}

func sortedKeys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// scanShellSource walks shell source line-by-line, classifying tokens
// as read or write based on surrounding context. Returns raw token
// strings; looksLikePath/path normalisation is the caller's problem.
func scanShellSource(src string) (reads, writes []string) {
	for _, line := range strings.Split(src, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		tokens := splitTokens(line)
		r, w := classifyTokens(tokens)
		reads = append(reads, r...)
		writes = append(writes, w...)
	}
	return reads, writes
}

// classifyTokens splits one tokenised shell line into read and write
// path lists. It is intentionally simple: walk left-to-right, track
// which verb we are currently inside, and react to redirection bytes.
func classifyTokens(tokens []string) (reads, writes []string) {
	if len(tokens) == 0 {
		return nil, nil
	}
	verb := strings.TrimPrefix(tokens[0], "sudo")
	verb = strings.TrimSpace(verb)
	// Strip leading path elements: "/usr/bin/curl" → "curl".
	if i := strings.LastIndexByte(verb, '/'); i >= 0 {
		verb = verb[i+1:]
	}

	_, isRead := readOnlyCmds[verb]
	_, isWrite := writeCmds[verb]

	for i := 1; i < len(tokens); i++ {
		t := tokens[i]
		switch t {
		case ">", "1>":
			if i+1 < len(tokens) {
				writes = append(writes, tokens[i+1])
				i++
			}
			continue
		case ">>", "1>>":
			if i+1 < len(tokens) {
				writes = append(writes, tokens[i+1])
				i++
			}
			continue
		case "<":
			if i+1 < len(tokens) {
				reads = append(reads, tokens[i+1])
				i++
			}
			continue
		}
		if strings.HasPrefix(t, ">") {
			writes = append(writes, strings.TrimLeft(t, ">"))
			continue
		}
		if strings.HasPrefix(t, "<") {
			reads = append(reads, strings.TrimLeft(t, "<"))
			continue
		}
		if strings.HasPrefix(t, "-") || strings.Contains(t, "=") {
			// Flag or VAR=value; not a path argument.
			continue
		}
		switch {
		case isWrite:
			writes = append(writes, t)
		case isRead:
			reads = append(reads, t)
		default:
			// Unknown verb: classify args as reads (conservative read
			// over false-positive write).
			reads = append(reads, t)
		}
	}
	return reads, writes
}

// splitTokens tokenises a shell line. Quoted regions become a single
// token with quotes preserved; the caller strips them.
func splitTokens(line string) []string {
	var (
		out   []string
		buf   strings.Builder
		quote byte
	)
	flush := func() {
		if buf.Len() > 0 {
			out = append(out, buf.String())
			buf.Reset()
		}
	}
	for i := 0; i < len(line); i++ {
		c := line[i]
		if quote != 0 {
			if c == quote {
				quote = 0
			}
			buf.WriteByte(c)
			continue
		}
		if c == '"' || c == '\'' {
			quote = c
			buf.WriteByte(c)
			continue
		}
		if c == ' ' || c == '\t' {
			flush()
			continue
		}
		buf.WriteByte(c)
	}
	flush()
	return out
}

// looksLikePath rejects single-word tokens that are obviously not file
// references — bare command names, sub-100ms expressions, integers,
// etc. The regex is the primary gate; this is a final sanity check.
func looksLikePath(s string) bool {
	if s == "" || len(s) > 512 {
		return false
	}
	if strings.HasPrefix(s, "-") {
		return false
	}
	// Filter out obvious non-paths: URL prefixes (handled by network
	// detector), env-var references, command substitutions.
	if strings.HasPrefix(s, "$") || strings.HasPrefix(s, "`") {
		return false
	}
	if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
		return false
	}
	if !pathLike.MatchString(s) {
		return false
	}
	// At least one of: path separator, glob marker, or filename
	// extension. Bare words like "name" are rejected.
	hasSep := strings.ContainsAny(s, "/")
	hasGlob := strings.ContainsAny(s, "*?")
	hasDot := strings.Contains(s, ".") && !strings.HasPrefix(s, ".")
	return hasSep || hasGlob || hasDot
}

// isShellScriptName is a path-extension check duplicated here from the
// shell package so detectors can be used independently. Keeping the
// two copies tiny is preferable to a shared util package.
func isShellScriptName(rel string) bool {
	rel = strings.ToLower(rel)
	return strings.HasSuffix(rel, ".sh") ||
		strings.HasSuffix(rel, ".bash") ||
		strings.HasSuffix(rel, ".zsh")
}
