// Package shell extracts the shell commands a parsed skill is likely to
// invoke. It is intentionally heuristic — there is no full shell parser
// here, only line-by-line tokenisation of obvious cases. False positives
// are preferable to false negatives because the lockfile gives the
// reviewer the final say.
//
// Inputs the detector mines (in priority order):
//  1. Bundled `.sh` and shebanged scripts under scripts/.
//  2. Fenced code blocks whose info string is a shell dialect.
//  3. The `allowed-tools` frontmatter list (as a sentinel — Bash there
//     means "shell may run" even when no script is bundled).
package shell

import (
	"sort"
	"strings"

	"github.com/skills-lock/skil-lock/internal/parser/claude"
)

// ShellSentinel is the value emitted when `allowed-tools` lists Bash
// but no specific command is discoverable from body or scripts. It tells
// reviewers that the skill is allowed to run shell at all — a coarse
// but visible signal.
const ShellSentinel = "bash"

// shellLangs is the set of fenced-code info strings we treat as shell.
var shellLangs = map[string]struct{}{
	"bash": {}, "sh": {}, "shell": {}, "zsh": {},
	"console": {}, "shell-session": {},
}

// builtinsAndKeywords are tokens that look like a leading word but are
// shell syntax, not a command. They are skipped during extraction.
var builtinsAndKeywords = map[string]struct{}{
	"if": {}, "then": {}, "else": {}, "elif": {}, "fi": {},
	"for": {}, "while": {}, "until": {}, "do": {}, "done": {},
	"case": {}, "esac": {}, "in": {},
	"function": {}, "return": {}, "break": {}, "continue": {},
	"local": {}, "declare": {}, "readonly": {}, "typeset": {},
	"export": {}, "unset": {}, "set": {}, "shopt": {},
	"trap": {}, "true": {}, "false": {}, "exit": {},
	"echo": {}, "printf": {}, "read": {},
	"cd": {}, "pwd": {}, "pushd": {}, "popd": {},
	".": {}, "source": {}, "exec": {}, "eval": {},
	"[": {}, "[[": {}, "test": {},
}

// shScriptExts identifies bundled scripts whose contents should be
// scanned as shell. Other languages (.py, .js, ...) are out of scope
// for v1 — the shell detector ignores them.
var shScriptExts = map[string]struct{}{
	".sh":   {},
	".bash": {},
	".zsh":  {},
}

// Detect returns the sorted, deduplicated set of shell commands the
// parsed skill is likely to invoke. The list is suitable for
// model.Behavior.ShellCommands.
func Detect(p claude.ParsedSkill) []string {
	cmds := map[string]struct{}{}

	for _, cb := range p.CodeBlocks {
		if _, ok := shellLangs[cb.Language]; !ok {
			continue
		}
		for _, c := range tokenize(cb.Content) {
			cmds[c] = struct{}{}
		}
	}

	for _, s := range p.Scripts {
		if !isShellScript(s) {
			continue
		}
		for _, c := range tokenize(s.Content) {
			cmds[c] = struct{}{}
		}
	}

	if len(cmds) == 0 && allowsBash(p.AllowedTools) {
		cmds[ShellSentinel] = struct{}{}
	}

	out := make([]string, 0, len(cmds))
	for c := range cmds {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}

// allowsBash returns true if `allowed-tools` includes a Bash-family
// tool. Comparison is case-insensitive because we can't trust authors
// to spell it the same way every time.
func allowsBash(tools []string) bool {
	for _, t := range tools {
		switch strings.ToLower(strings.TrimSpace(t)) {
		case "bash", "shell", "sh", "zsh":
			return true
		}
	}
	return false
}

// isShellScript returns true for bundled files we will scan as shell —
// either by extension or by leading shebang.
func isShellScript(s claude.Script) bool {
	lower := strings.ToLower(s.RelPath)
	for ext := range shScriptExts {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	if strings.HasPrefix(s.Content, "#!") {
		first := s.Content
		if i := strings.IndexByte(first, '\n'); i >= 0 {
			first = first[:i]
		}
		first = strings.ToLower(first)
		if strings.Contains(first, "bash") || strings.Contains(first, "/sh") ||
			strings.Contains(first, "zsh") {
			return true
		}
	}
	return false
}

// tokenize walks lines of shell source and returns the lead command of
// each. Lines that are blank, comments, here-doc bodies, or pure
// variable assignments are skipped.
func tokenize(src string) []string {
	var out []string
	for _, line := range strings.Split(joinContinuations(src), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// A line may be a pipeline ("foo | bar") or a sequence
		// ("foo && bar; baz"). Split on those separators and process
		// each segment as its own command.
		for _, seg := range splitOnSeparators(line) {
			cmd := leadCommand(strings.TrimSpace(seg))
			if cmd != "" {
				out = append(out, cmd)
			}
		}
	}
	return out
}

// joinContinuations merges POSIX backslash-newline continuations so a
// command spread across several lines is processed as one logical line.
// A line whose trimmed-right end is a single backslash continues onto the
// next; a trailing "\\" (escaped backslash) is left alone. Without this,
// multi-line `curl ... \` pipelines split their flags (-H, -d) and the
// trailing URL into bogus separate commands.
func joinContinuations(src string) string {
	lines := strings.Split(src, "\n")
	out := make([]string, 0, len(lines))
	var pending strings.Builder
	carrying := false
	for _, line := range lines {
		trimmed := strings.TrimRight(line, " \t")
		cont := strings.HasSuffix(trimmed, `\`) && !strings.HasSuffix(trimmed, `\\`)
		if cont {
			pending.WriteString(strings.TrimSuffix(trimmed, `\`))
			pending.WriteByte(' ')
			carrying = true
			continue
		}
		if carrying {
			pending.WriteString(line)
			out = append(out, pending.String())
			pending.Reset()
			carrying = false
			continue
		}
		out = append(out, line)
	}
	if carrying {
		out = append(out, strings.TrimRight(pending.String(), " "))
	}
	return strings.Join(out, "\n")
}

// splitOnSeparators splits a shell line on pipe / sequencing operators
// without touching the contents of single- or double-quoted regions.
// This is a coarse approximation; full shell parsing is out of scope.
func splitOnSeparators(line string) []string {
	var (
		segs       []string
		buf        strings.Builder
		quote      byte
		i          = 0
		separators = "|;&"
	)
	for i < len(line) {
		c := line[i]
		if quote != 0 {
			if c == quote {
				quote = 0
			}
			buf.WriteByte(c)
			i++
			continue
		}
		if c == '\'' || c == '"' {
			quote = c
			buf.WriteByte(c)
			i++
			continue
		}
		if strings.IndexByte(separators, c) >= 0 {
			// Collapse runs like "&&", "||", ";;".
			segs = append(segs, buf.String())
			buf.Reset()
			i++
			for i < len(line) && strings.IndexByte(separators, line[i]) >= 0 {
				i++
			}
			continue
		}
		buf.WriteByte(c)
		i++
	}
	if buf.Len() > 0 {
		segs = append(segs, buf.String())
	}
	return segs
}

// leadCommand returns the program name at the head of one pipeline
// segment, or "" if no command is found.
func leadCommand(seg string) string {
	tokens := strings.Fields(seg)
	for _, tok := range tokens {
		// Drop redirections like 1>&2 / >out.txt — they are decoration.
		if strings.ContainsAny(tok, "<>") {
			continue
		}
		// Variable assignment prefix `FOO=bar`: skip and keep looking.
		if eq := strings.IndexByte(tok, '='); eq > 0 && isAssignment(tok[:eq]) {
			continue
		}
		// Strip optional leading "sudo " / "env " modifiers in a loop —
		// we want the actual command being run.
		if tok == "sudo" || tok == "env" || tok == "exec" || tok == "command" {
			continue
		}
		if _, kw := builtinsAndKeywords[tok]; kw {
			return ""
		}
		// Strip a leading $(…) or `…` shouldn't happen at lead position
		// after Fields; if it does, treat as opaque.
		if strings.HasPrefix(tok, "$") || strings.HasPrefix(tok, "(") ||
			strings.HasPrefix(tok, "{") {
			return ""
		}
		// Strip path prefix so "/usr/bin/curl" → "curl".
		if i := strings.LastIndexByte(tok, '/'); i >= 0 {
			tok = tok[i+1:]
		}
		// Drop trailing punctuation that may have leaked in.
		tok = strings.TrimRight(tok, "\\")
		if tok == "" {
			return ""
		}
		return tok
	}
	return ""
}

// isAssignment is true when s is a valid shell variable name — meaning
// FOO=bar is an assignment, not a command "FOO=bar".
func isAssignment(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if r == '_' || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
			continue
		}
		if i > 0 && r >= '0' && r <= '9' {
			continue
		}
		return false
	}
	return true
}
