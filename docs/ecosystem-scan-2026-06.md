# Capability scan of the public Claude Code skills ecosystem (June 2026)

An AI skill is a Markdown file your coding agent reads and obeys. GitHub code search currently finds **74,192 `SKILL.md` files installed under `.claude/skills/` in public repositories**. We pulled a sample of 461 of those repositories (plus the official Anthropic, OpenAI, and Trail of Bits catalogs), ran a static capability scan over every skill, and aggregated what they can actually do.

**Sample: 392 repositories with parseable skills, 17,065 skills (12,280 unique by content hash).** Repositories ranged from personal dotfiles to projects with 56k+ stars. Aggregate statistics only — this report names no repository and no skill.

## The numbers

| Capability | Skills | Share |
|---|---|---|
| Read files | 11,780 | 69.0% |
| Reference network URLs | 8,287 | 48.6% |
| Ship bundled scripts/files | 6,970 | 40.8% |
| **Execute shell commands** | **6,615** | **38.8%** |
| Shell + network + file access in one skill | 4,184 | 24.5% |
| Write files | 1,853 | 10.9% |
| Use `curl` or `wget` | 828 | 4.9% |
| **Declare `Bash` in `allowed-tools` frontmatter** | **690** | **4.0%** |
| Read sensitive-looking paths (`.env`, `.ssh`, `.aws`, keys) | 364 | 2.1% |

Most common shell verbs across skills: `grep`, `npm`, `git`, `python`, `curl`, `cat`, `pip`, `npx`, `mkdir`, `bash`, `jq`, `uv`, `rm`, `node`, `gh`.

## Three observations

**1. Capability is implicit, not declared.** 38.8% of skills execute shell commands, but only 4.0% declare `Bash` in their `allowed-tools` frontmatter. The frontmatter — the only part that looks like a manifest — tells you almost nothing. The capability lives in the prose and the fenced code blocks, which is exactly the part nobody re-reads when a skill gets "a small docs update."

**2. A quarter of skills hold the full toolkit.** 24.5% combine shell execution + network access + file access in a single skill. None of that is malicious by itself — a deploy helper legitimately needs all three. But the difference between a deploy helper and an exfiltration chain is only the argument values: which host, which file. A reviewer who approved the skill once will not notice when one of those values changes in a later diff.

**3. `.env` reads are normal — and that's the problem.** 364 skills (2.1%) read paths like `.env`, `.ssh`, or `.aws` credentials files. Spot-checking shows most read *their own* config (`.claude/skills/<name>/.env`) — legitimate. But today's review process gives you no way to distinguish "reads its own .env" from "started reading yours" between two versions of the same skill, because nobody diffs skill *behavior* — they diff Markdown prose.

## Why this repository cares

Skills are dependencies. We learned this lesson with packages: you don't re-audit `node_modules` by hand on every update — you pin a lockfile and review the diff. Skills need the same primitive: a committed record of the capability surface you approved (shell verbs, hosts, file paths), and a CI gate that shows the *capability delta* on every PR. That is what `skil-lock` does; the [skills.lock spec](../SPEC.md) is CC BY 4.0 and usable without this tool. The data point stands on its own, whatever tooling you choose: **the capability surface of installed skills is large, mostly undeclared, and currently unreviewed.**

## Methodology and caveats

- Sample = first 500 GitHub code-search hits for `filename:SKILL.md path:.claude/skills` (461 unique repositories, 457 scanned successfully) + 3 official catalogs scanned separately. Code-search ordering is not a uniform random sample of the 74k population.
- Capability extraction is the same static analysis `skil-lock scan` performs: shell verbs from fenced code blocks and bundled scripts, URLs and file paths as written. Runtime-assembled commands (variables, `base64`, `eval`) and natural-language instructions are NOT counted — the true capability surface is strictly larger than these numbers.
- Counts are per skill, with deduplicated tokens and junk filtering. 12,280 of 17,065 skills are unique by content hash (skills get vendored across repositories).
- "Sensitive-looking paths" matches path-like strings only (`.env*`, `.ssh`, `.aws`, `id_rsa`/`id_ed25519`, `.netrc`, `.npmrc`, `.git-credentials`, `.gnupg`); code fragments are excluded. Reading such a path is often legitimate — the stat measures exposure surface, not malice.
- Scan date: June 2026.
