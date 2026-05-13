# SkilLock

> **Status: early development, private.** This repo is the canonical home of the `skil-lock` CLI and the `skills.lock` file format. There is no install yet; the first release will be tagged `v0.1.0`. Watch this repo for updates.

**`skil-lock`** pins approved AI Skill behavior and blocks unapproved drift in CI.

*package-lock + Dependabot + PR security review, for AI Skills.*

## What it does (target v0.1)

When AI coding agents like Claude Code or Codex install [Skills](https://code.claude.com/docs/en/skills), those skills can run shell commands, hit the network, and read or write files in your repo. **`skil-lock` records the approved behavior surface in a committed `skills.lock` file** — and a GitHub Action posts a PR comment showing exactly which capabilities changed in every diff.

- **`skills.lock`** — the artifact. Records each Skill's shell commands, network URLs, file reads/writes, and bundled scripts.
- **`.skil-lock.yaml`** — the policy. Set `mode: warn` or `block`, define protected paths and allowed domains.
- **`.skil-lock-approvals.yaml`** — the audit trail. Reviewer + reason + timestamp for every approved behavior delta.
- **GitHub Action** — runs on every PR, posts a capability-delta comment, sets check status.

## Why not just hash-pin?

Hashes detect tampering. They don't tell a reviewer *what changed*. `skil-lock` records behavior, so a PR comment can say "this skill now also runs `curl` and reads `.env`" instead of "the hash changed."

## v0.1 scope

- Runtimes: **Claude Code + Codex** (same SKILL.md format)
- Three deterministic detectors: shell execution, external network, protected-path reads
- CLI: `scan`, `lock`, `init --baseline`, `list`, `diff`, `verify`, `ci`
- GitHub Action with PR-comment renderer

Out of scope for v0.1: runtime guard, Cursor/Windsurf/MCP parsers, AI-assisted detection, dashboards. See [`PRODUCT.md`](./PRODUCT.md) §16 for the full out-of-scope list.

## Project status

- **Phase 0** — design partner validation
- **Phase 1** — CLI v0.1 build
- **Phase 2** — GitHub Action + PR-comment renderer
- **Phase 3** — public launch + `SPEC.md`
- **Day 30** — kill criterion gate

## License

[Apache 2.0](./LICENSE). Contributions covered by a one-time CLA via cla-assistant.io (see [`CONTRIBUTING.md`](./CONTRIBUTING.md)).

## Security

See [`SECURITY.md`](./SECURITY.md). Report vulnerabilities via [GitHub Security Advisories](https://github.com/skills-lock/skil-lock/security/advisories/new), not public issues.
