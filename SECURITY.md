# Security Policy

## Reporting a Vulnerability

**Please do not file a public GitHub Issue.** Vulnerabilities should be reported privately so we can fix and disclose responsibly.

### Preferred: GitHub Security Advisories

Use the **"Report a vulnerability"** button at:
<https://github.com/skills-lock/skil-lock/security/advisories/new>

This opens a private channel with the maintainers, gives you a CVE-eligible identifier, and tracks the fix to disclosure.

If GHSA is unavailable for any reason, open a public issue with no
vulnerability details, and request a private channel in the first
comment; a maintainer will follow up. (A dedicated email contact is
not configured for v0.1 — GHSA is the supported path.)

## Scope

In scope:

- The `skil-lock` CLI (this repository)
- The `skil-lock-action` GitHub Action
- The `skills.lock` file format specification

Out of scope:

- Vulnerabilities in dependencies (report upstream; we'll bump after the upstream fix)
- Vulnerabilities in the SKILL.md format itself (report to the runtime vendor — Anthropic for Claude Code, OpenAI for Codex)
- Social engineering, physical access, or denial-of-service against shared infrastructure

## Disclosure

- We aim to acknowledge reports within **72 hours**.
- Fix and coordinated public disclosure target: **90 days** from acknowledgement, or sooner if a fix lands faster.
- Reporters who wish to be credited will be named in the GHSA and release notes.

## Supply Chain

Released binaries are built and signed via GoReleaser in GitHub Actions from tagged commits. Verify with the published checksums on the corresponding release page.
