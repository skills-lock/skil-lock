# SkilLock

**Lock the *behavior* of your AI Skills. See exactly what changed in every Pull Request.**

`skil-lock` pins the capability surface — shell commands, network URLs, file paths — of every Claude Code and Codex Skill in your repository. On every PR, a GitHub Action posts a comment showing **what changed**.

Hash pinning catches tampering. SkilLock catches *what the skill is now doing*.

[![License: Apache 2.0](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](./LICENSE)
[![Spec: v0.1](https://img.shields.io/badge/skills.lock-v0.1-green.svg)](./SPEC.md)

---

## What it actually does

When AI coding agents like [Claude Code](https://code.claude.com/docs/en/skills) or [Codex](https://developers.openai.com/codex/skills) install Skills, those skills can run shell commands, hit the network, and read or write files in your repo.

SkilLock records that capability surface in a committed `skills.lock` file. Every PR re-scans, computes the delta, and posts something like this:

```
### SkilLock — capability changes

| Skill | Change | Capability | Detail | Reason |
|---|---|---|---|---|
| code-review | added | shell_commands | curl | — |
| code-review | added | network_urls | https://api.evil.example.com | host not in allowed_domains |
| code-review | added | file_reads | ./.env | matches protected_paths |

**BLOCK: 3 of 3 entries at severity >= medium**

Paste into `.skil-lock-approvals.yaml` to approve:

```yaml
schema_version: "0.1"
approvals:
  - skill: code-review
    delta: {added_shell_command: "curl"}
    reviewer: "REPLACE_ME"
    reviewed_at: 2026-05-20T14:00:00Z
    reason: "REPLACE_ME"
```
```

Approve by pasting four lines into the override file, push, the check turns green.

## 60-second install

In your repo (where `.claude/skills/` or `.codex/skills/` lives):

```bash
# 1. Install (pick one):

# Option A: via go install (needs Go 1.22+)
go install github.com/skills-lock/skil-lock/cmd/skil-lock@v0.1.0

# Option B: precompiled binary (Linux amd64; see Releases for other platforms)
curl -sL https://github.com/skills-lock/skil-lock/releases/download/v0.1.0/skil-lock_0.1.0_linux_amd64.tar.gz | tar -xz

# 2. Accept your current skills as the approved baseline
skil-lock init --baseline .

# 3. Commit the lockfile
git add skills.lock
git commit -m "Pin approved AI Skill behavior"
```

To run on every PR, add `.github/workflows/skil-lock.yml`:

```yaml
name: SkilLock
on: pull_request
permissions:
  contents: read
  pull-requests: write
jobs:
  skil-lock:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v6
      - uses: skills-lock/skil-lock-action@v0.1.0
        with:
          pin-binary: v0.1.0
```

For non-Linux, see the [releases page](https://github.com/skills-lock/skil-lock/releases) for macOS (Intel + Apple Silicon) and Windows builds.

## Why behavior, not hash?

A hash tells you *something* changed. It does not tell you *what*. When a reviewer sees `content_hash: sha256:abc → sha256:def` they have to read the entire diff to understand what's different.

SkilLock records the surfaces that matter for security review:

- **Shell commands** — does this skill now run `curl`? `rm`? `bash`?
- **Network URLs** — what hosts does it reach? Did a new one appear?
- **File reads / writes** — does it read `.env` now? Write to `dist/`?
- **Allowed tools** — what Claude/Codex tools did the author grant?
- **Bundled scripts** — what shipped alongside the markdown?

A reviewer sees `added file_reads: ./.env` and immediately knows what to ask.

## How it compares

| Tool | Style | What it pins | License |
|---|---|---|---|
| **SkilLock** (this) | Post-install, PR workflow | **Behavior surface** (shell, URLs, paths) | Apache 2.0 |
| Snyk Agent Scan | Pre-install scanner | n/a (on-demand scan) | Commercial |
| Mondoo Skills Check | Pre-install scanner | n/a (on-demand scan) | Commercial |
| SkillFortify | Post-install | Hash + coarse capabilities | Elastic 2.0 |
| pcomans/skills-lock | Post-install | Git commit SHA only | MIT |
| `gh skill --pin` | Built into GitHub CLI | Tag / SHA | (GitHub CLI license) |

If you want known-bad pattern scanning before you install a skill, use Snyk or Mondoo. If you want a *committed file* that lets reviewers see capability changes in every PR, use this.

## What's in v0.1

- CLI: `scan`, `lock`, `init --baseline`, `list`, `diff`, `ci`
- Runtimes: **Claude Code** and **Codex** (same `SKILL.md` format)
- Three deterministic detectors: shell execution, external network, protected-path reads/writes
- `skills.lock` — committed baseline, schema spec'd in [SPEC.md](./SPEC.md)
- `.skil-lock.yaml` — policy (warn vs block, protected paths, allowed domains)
- `.skil-lock-approvals.yaml` — override audit trail (reviewer + reason + timestamp)
- GitHub Action with PR-comment renderer

## What's NOT in v0.1 (intentionally)

To keep the scope narrow and the positioning clean:

- No runtime guard / Claude Code hooks integration — different problem
- No Cursor / Windsurf / MCP parsers — different file formats; expand based on demand
- No AI-assisted detection — three deterministic detectors only
- No known-bad pattern database — that's Mondoo's lane
- No web dashboard or registry

See [`SPEC.md`](./SPEC.md) for the full file-format specification. The out-of-scope list above is the canonical statement of what v0.1 will and will not do.

## Project status

- Phase 0–2 complete (CLI + GitHub Action shipped)
- Currently in launch prep (Phase 3)
- v0.1.0-rc1 available; v0.1.0 follows the public launch

## License

Apache 2.0 — see [LICENSE](./LICENSE). Contributions are covered by a one-time CLA via [cla-assistant.io](https://cla-assistant.io) (see [CONTRIBUTING.md](./CONTRIBUTING.md)).

## Security

Report vulnerabilities privately via [GitHub Security Advisories](https://github.com/skills-lock/skil-lock/security/advisories/new). See [SECURITY.md](./SECURITY.md). Do not file public issues for vulnerabilities.

## Trademarks

`SkilLock` and `skil-lock` are not affiliated with or endorsed by Skil power tools (a brand owned by Chervon Group). The name comes from "Skill Lock" and refers to AI Skills, not to power tools.

`Claude` and `Claude Code` are trademarks of Anthropic PBC. `Codex` is a trademark of OpenAI, OpCo, LLC. References to these names in this project are descriptive (nominative fair use) and do not imply affiliation with or endorsement by either company.
