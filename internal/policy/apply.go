package policy

import (
	"net/url"
	pathpkg "path"
	"path/filepath"
	"strings"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/skills-lock/skil-lock/internal/model"
)

// Apply mutates d in place, lifting per-entry severities according to
// pol. Three rules, applied in this order so the highest-severity rule
// wins (Apply only raises severity, never lowers it):
//
//  1. RequireApproval: any added entry whose Capability appears in
//     pol.RequireApproval is bumped to high — these categories are
//     declared as gated by the user.
//  2. AllowedDomains (non-empty): added network_urls whose host does
//     not match any allowed glob are bumped to high. An empty
//     AllowedDomains list disables this rule (no allowlist policy
//     in effect).
//  3. ProtectedPaths (non-empty): added file_reads / file_writes whose
//     value matches any protected glob are bumped to high. Matching
//     uses doublestar semantics so `secrets/**` and `**/*.pem` work.
//
// Patterns that fail to parse are treated as non-matches; the policy
// loader does not pre-validate glob syntax because the user-facing
// failure mode (no match) is preferable to a hard error on a typo.
func Apply(d *model.Diff, pol model.Policy) {
	requireApprovalSet := make(map[string]struct{}, len(pol.RequireApproval))
	for _, c := range pol.RequireApproval {
		requireApprovalSet[c] = struct{}{}
	}

	for i := range d.Entries {
		e := &d.Entries[i]
		if e.Change != model.ChangeAdded {
			continue
		}

		if _, ok := requireApprovalSet[e.Capability]; ok {
			liftTo(e, model.SeverityHigh, "matches require_approval")
		}

		if e.Capability == "network_urls" && len(pol.AllowedDomains) > 0 {
			if !domainAllowed(e.Value, pol.AllowedDomains) {
				liftTo(e, model.SeverityHigh, "host not in allowed_domains")
			}
		}

		if (e.Capability == "file_reads" || e.Capability == "file_writes") && len(pol.ProtectedPaths) > 0 {
			if pathProtected(e.Value, pol.ProtectedPaths) {
				liftTo(e, model.SeverityHigh, "matches protected_paths")
			}
		}
	}
}

// liftTo records that a policy rule fired on e. Severity is raised to
// target only if currently below it (raises, never lowers); the note is
// appended whether or not severity moved, so a reviewer can see every
// rule that flagged the entry.
func liftTo(e *model.DiffEntry, target model.Severity, note string) {
	if severityRank(e.Severity) < severityRank(target) {
		e.Severity = target
	}
	if strings.Contains(e.Note, note) {
		return
	}
	if e.Note == "" {
		e.Note = note
		return
	}
	e.Note = e.Note + "; " + note
}

func severityRank(s model.Severity) int {
	switch s {
	case model.SeverityHigh:
		return 4
	case model.SeverityMedium:
		return 3
	case model.SeverityLow:
		return 2
	case model.SeverityInfo:
		return 1
	}
	return 0
}

// domainAllowed returns true if the host parsed from rawURL matches any
// glob in patterns. Non-URL values (rare — detectors emit canonical
// URLs) are matched whole against the patterns as a fallback.
func domainAllowed(rawURL string, patterns []string) bool {
	host := hostFrom(rawURL)
	for _, p := range patterns {
		if ok, _ := filepath.Match(p, host); ok {
			return true
		}
		if host != rawURL {
			if ok, _ := filepath.Match(p, rawURL); ok {
				return true
			}
		}
	}
	return false
}

func hostFrom(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return rawURL
	}
	return u.Hostname()
}

// pathProtected reports whether rel matches any glob in patterns, using
// doublestar semantics so `**` spans directory components. Both sides are
// slash-cleaned first so a detected read of "./.env" matches a policy
// entry of ".env" (and "./secrets/**" matches "secrets/foo.pem"); without
// this, doublestar treats the literal "./" as a real path segment and the
// match silently fails. pathpkg.Clean is used (not filepath.Clean) so the
// separator stays "/" regardless of host OS.
func pathProtected(rel string, patterns []string) bool {
	rel = pathpkg.Clean(rel)
	for _, p := range patterns {
		if ok, _ := doublestar.Match(pathpkg.Clean(p), rel); ok {
			return true
		}
	}
	return false
}
