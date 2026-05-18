// Package network extracts external URLs from a parsed skill. The
// detector is intentionally permissive — it surfaces every http(s) URL
// it can find in the body, code fences, and bundled scripts. The diff
// engine and policy layer are what classify a URL as allowed or not.
package network

import (
	"regexp"
	"sort"
	"strings"

	"github.com/skills-lock/skil-lock/internal/parser/claude"
)

// urlPattern matches http(s) URLs with a host and optional path. We
// stop at common trailing punctuation so URLs at the end of a sentence
// don't pick up the closing period or comma.
//
// Anchors:
//   - https? scheme.
//   - "://".
//   - host segment: letters, digits, dots, hyphens, optional `*` for
//     allowlist-style globs ("*.github.com") and optional port.
//   - optional path/query/fragment composed of URL-safe characters.
var urlPattern = regexp.MustCompile(`https?://[A-Za-z0-9.\-*]+(?::\d+)?(?:/[A-Za-z0-9._~!$&'()+,;=:@/%?#*\-]*)?`)

// trailingTrim is the set of bytes we strip from the right of a match
// because they are usually punctuation, not URL content.
const trailingTrim = ".,;:!?)]>}'\""

// Detect returns the sorted, deduplicated set of URLs referenced by
// the parsed skill. Suitable for model.Behavior.NetworkURLs.
func Detect(p claude.ParsedSkill) []string {
	urls := map[string]struct{}{}

	scan := func(s string) {
		for _, m := range urlPattern.FindAllString(s, -1) {
			cleaned := strings.TrimRight(m, trailingTrim)
			if cleaned == "" {
				continue
			}
			urls[cleaned] = struct{}{}
		}
	}

	scan(p.Body)
	for _, cb := range p.CodeBlocks {
		scan(cb.Content)
	}
	for _, s := range p.Scripts {
		scan(s.Content)
	}

	out := make([]string, 0, len(urls))
	for u := range urls {
		out = append(out, u)
	}
	sort.Strings(out)
	return out
}
