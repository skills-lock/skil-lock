# Security Policy

## Reporting a Vulnerability

**Please do not file a public GitHub Issue.** Vulnerabilities should be reported privately so we can fix and disclose responsibly.

### Preferred: GitHub Security Advisories

Use the **"Report a vulnerability"** button at:
<https://github.com/skills-lock/skil-lock/security/advisories/new>

This opens a private channel with the maintainers, gives you a CVE-eligible identifier, and tracks the fix to disclosure.

### Backup: Email

If you cannot use GHSA for any reason, email **skills.lock@gmail.com**. This is the maintainer's contact while a `@skil-lock.dev` alias is being set up; once the domain is configured, `security@skil-lock.dev` will forward to the same inbox. Expect acknowledgement within 72 hours.

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

Releases are built by GoReleaser in GitHub Actions from tagged commits.

**Integrity (all releases).** Every release publishes a `checksums.txt` manifest. After downloading an artifact, verify it matches:

```sh
sha256sum -c checksums.txt --ignore-missing
```

Note that `checksums.txt` proves the bytes match what the release published — integrity, not provenance.

**Signing + provenance (from v0.2.0 onward).** Starting with the `v0.2.0` release, artifacts and the checksums manifest are signed with [cosign](https://github.com/sigstore/cosign) using keyless (OIDC) signing, and each release ships SLSA build provenance and an SBOM. Verify the checksums signature with:

```sh
cosign verify-blob \
  --certificate checksums.txt.pem \
  --signature checksums.txt.sig \
  --certificate-identity-regexp 'https://github.com/skills-lock/skil-lock/.+' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  checksums.txt
```

Releases tagged at or before `v0.1.2` provide `checksums.txt` only (no signature); this section is updated to match what each release actually ships.

When pinning the GitHub Action in your own workflow, pin to a commit SHA (not a movable tag) for the same reason — the [example workflow](https://github.com/skills-lock/example-claude-code-skills/blob/main/.github/workflows/skil-lock.yml) does this.
