# `skills.lock` Specification — v0.1

**Status:** Draft. Public review welcome via issues in this repository.
**License:** This specification is offered under [CC BY 4.0](https://creativecommons.org/licenses/by/4.0/). The reference implementation (`skil-lock`) is offered under [Apache 2.0](./LICENSE).

## 1. Purpose

`skills.lock` is a committed file that records the approved capability surface of every AI Skill a repository depends on — what shell commands they run, what network endpoints they reach, what file paths they read and write. Reviewers compare the lockfile against the current state on every Pull Request and approve any drift before it lands on `main`.

This document specifies the on-disk format. The companion files `.skil-lock.yaml` (policy) and `.skil-lock-approvals.yaml` (override audit trail) are not part of this spec.

## 2. Scope

This spec covers:

- File location, filename, and discovery
- YAML schema for `schema_version: "0.1"`
- Field semantics
- Canonical serialisation rules
- Hash format
- Validation rules

It does **not** specify:

- How a tool *scans* a repository to produce the lockfile (implementation choice)
- Policy gating, review workflow, or CI integration (separate documents)
- Multi-runtime parsing rules beyond what's needed to populate `runtime:`

## 3. File location

The lockfile is named exactly `skills.lock` (lowercase, no extension) and lives at the repository root. There is **one** lockfile per repository.

`skills.lock` is tool-generated. Hand-editing is discouraged; tools may refuse to load a file whose `generated_by` line indicates non-tool authorship.

## 4. File format

The file is UTF-8-encoded YAML 1.2 with:

- LF line endings
- 2-space indentation
- A leading two-line `#` comment block (not part of the schema; recommended for human readers)

The comment block:

```yaml
# skills.lock — generated and maintained by skil-lock. Do not hand-edit.
# Commit this file. PR reviewers will see capability deltas inline.
```

Tools MAY emit a different comment block, but MUST emit one.

## 5. Top-level schema

```yaml
schema_version: "0.1"
generated_at: <RFC 3339 UTC, second precision>
generated_by: <tool-identifier> <version>
skills:
  <skill-name>: <skill-entry>
  ...
```

| Field | Type | Required | Notes |
|---|---|---|---|
| `schema_version` | string | yes | Must be the literal `"0.1"` for this spec. Tools MUST reject other values unless they support that version. |
| `generated_at` | string (RFC 3339) | yes | UTC, second precision (not nanosecond, to avoid churning the lockfile every run). Example: `2026-05-20T11:37:55Z`. |
| `generated_by` | string | yes | Tool identifier + version, space-separated. Example: `skil-lock 0.1.0`. |
| `skills` | map | yes | Keys are skill names. Map MUST be empty (`{}`) when no skills are present, not `null`. |

## 6. Skill entry schema

Each value under `skills:` is a map:

```yaml
<skill-name>:
  runtime: <claude|codex>
  source_path: <repo-relative path to SKILL.md>
  version: <string, may be empty>
  content_hash: sha256:<64 hex chars>
  behavior:
    shell_commands: [<string>, ...]
    network_urls: [<string>, ...]
    file_reads: [<string>, ...]
    file_writes: [<string>, ...]
    allowed_tools: [<string>, ...]
    bundled_scripts: [<string>, ...]
```

| Field | Type | Required | Notes |
|---|---|---|---|
| `runtime` | enum | yes | `claude` or `codex`. v0.2 may add more. |
| `source_path` | string | yes | Forward-slash, repo-relative path to the SKILL.md file. |
| `version` | string | optional | Free-form. Empty string when the skill's frontmatter does not declare a version. |
| `content_hash` | string | yes | `sha256:` prefix + 64 lowercase hex chars. Hash of the SKILL.md file bytes only (bundled scripts not included in the hash). |
| `behavior` | map | yes | Capability surface. All six sub-fields are required and MUST be present as lists, even when empty (`[]`). |

### 6.1 Behavior fields

All six fields are **lists of strings**, sorted ascending, deduplicated. Empty lists serialize as `[]`, never `null` or omitted.

| Field | Meaning | Examples |
|---|---|---|
| `shell_commands` | Distinct shell verbs the skill invokes | `git`, `curl`, `bash`, `node` |
| `network_urls` | URLs and host globs the skill reaches | `https://api.github.com/repos/*/releases`, `*.example.com` |
| `file_reads` | Repo-relative paths or globs the skill reads | `CHANGELOG.md`, `**/*.go` |
| `file_writes` | Repo-relative paths or globs the skill writes | `dist/**`, `release-notes-*.md` |
| `allowed_tools` | Anthropic/OpenAI tool names listed in the skill's `allowed-tools:` frontmatter (or equivalent) | `Bash`, `Read`, `Write`, `Grep` |
| `bundled_scripts` | Forward-slash paths to scripts shipped alongside the skill | `scripts/extract.sh` |

A tool MAY include path entries that are not literal files but pattern globs (`**/*.go`); the spec does not distinguish, because skills routinely use both forms. Reviewers reading the lockfile and writing policy rules in `.skil-lock.yaml` work with these strings verbatim.

### 6.2 Field ordering

Fields within each skill entry MUST appear in this order: `runtime`, `source_path`, `version`, `content_hash`, `behavior`. Within `behavior`, the order is the table above.

The skill map is sorted alphabetically by skill name.

This ordering produces stable diffs across regenerations and tool versions.

## 7. Hash

`content_hash` is `sha256:` followed by the lowercase hexadecimal SHA-256 of the SKILL.md file's raw bytes — *not* the normalized contents, *not* the parsed frontmatter + body separately. Including bundled scripts in the hash would force a lockfile update on every script change and was rejected; bundled scripts are surfaced via `bundled_scripts` instead and can be hashed separately by tools that want to.

## 8. Validation

A conforming lockfile MUST:

1. Parse as well-formed YAML 1.2.
2. Have `schema_version: "0.1"` at the top level.
3. Have a `skills:` map (possibly empty).
4. For each skill, satisfy section 6.

A conforming tool MAY warn (but MUST NOT fail) when it encounters:

- A `generated_by` field referencing an unfamiliar tool (interoperability is the goal)
- Extra fields under `skills.<name>` (forward compatibility — tools should tolerate fields they don't understand at their own schema_version)

A conforming tool MUST fail when it encounters:

- `schema_version` outside its supported range
- A skill with missing required fields per section 6

## 9. Versioning policy

`schema_version` follows a major.minor numbering. A bump to the major component is a breaking change (e.g., dropped or renamed fields); a bump to the minor is additive (new optional fields, new behavior categories).

v0.1 → v0.2 will likely add: more runtimes (Cursor, Windsurf, MCP servers), additional behavior categories (environment variable reads, signal handlers), and a `policy_hash` field linking a lockfile to the policy that approved it.

v0.x consumers SHOULD NOT assume forward compatibility; v1.0 introduces a stability promise.

## 10. OWASP AST10 mapping

The behavior categories map to OWASP Agentic Skills Top 10 (AST10) risk categories as follows. This mapping is informational and may evolve as AST10 ratifies further detail:

| `behavior` field | Primary AST10 category |
|---|---|
| `shell_commands` | Command Execution / Arbitrary Code Execution |
| `network_urls` | Data Exfiltration / Server-Side Request Forgery |
| `file_reads` | Secret / Sensitive File Exposure |
| `file_writes` | Tampering / Integrity Compromise |
| `allowed_tools` | Excessive Tool Permissions |
| `bundled_scripts` | Supply Chain Risk |

Tools targeting AST10 reporting MAY emit per-finding `taxonomy:` fields under `behavior` entries. The schema for this is reserved for v0.2.

## 11. Example

```yaml
# skills.lock — generated and maintained by skil-lock. Do not hand-edit.
# Commit this file. PR reviewers will see capability deltas inline.

schema_version: "0.1"
generated_at: 2026-05-20T11:37:55Z
generated_by: skil-lock 0.1.0
skills:
  pdf-extractor:
    runtime: claude
    source_path: .claude/skills/pdf-extractor/SKILL.md
    version: 1.2.0
    content_hash: sha256:a1b2c3d4e5f6789012345678901234567890abcdef1234567890abcdef123456
    behavior:
      shell_commands:
        - pdftotext
      network_urls: []
      file_reads:
        - ./input/*.pdf
      file_writes:
        - ./output/*.txt
      allowed_tools:
        - Bash
        - Read
        - Write
      bundled_scripts:
        - scripts/extract.sh
```

## 12. Reference test vectors

The reference test vectors for this spec are the golden files under `internal/lockfile/testdata/` in the [skil-lock repository](https://github.com/skills-lock/skil-lock). Tool authors implementing this spec SHOULD reproduce the byte-exact output of the golden file from the input fixture, then keep that as a regression test.

## 13. Conformance label

A tool that:

1. Reads any conforming v0.1 `skills.lock`
2. Writes a conforming v0.1 `skills.lock` for the same inputs (modulo `generated_by` and `generated_at`)

MAY claim **`skills.lock v0.1 conformant`** in its documentation and badges.

## 14. Interoperability — SARIF v2.1.0

The reference implementation can additionally emit a [SARIF v2.1.0](https://docs.oasis-open.org/sarif/sarif/v2.1.0/sarif-v2.1.0.html) document for each capability delta so findings can flow into [GitHub Code Scanning](https://docs.github.com/en/code-security/code-scanning) and other SARIF consumers.

This is **not** required by the spec — a conforming tool MAY produce SARIF output. When it does:

- Each `DiffEntry` becomes one `result`.
- The `ruleId` is derived from the capability key: `SKL-SHELL`, `SKL-NETWORK`, `SKL-FILE-READ`, `SKL-FILE-WRITE`, `SKL-TOOLS`, `SKL-SCRIPTS`.
- `level` maps from severity: `high → error`, `medium → warning`, `low|info → note`.
- `physicalLocation.artifactLocation.uri` resolves to the skill's `source_path` from the current lockfile; removed skills emit results with no `locations` field.

The SARIF output is a view of the same delta the markdown PR comment renders — both consume the same `Diff` produced from a baseline + current `skills.lock`.

## 15. Spec maintenance

Changes to this spec are tracked via Pull Requests on the [skil-lock repository](https://github.com/skills-lock/skil-lock). Material changes (breaking, deprecating, adding required fields) bump the `schema_version`.

This document is intentionally short. Implementation hints, edge-case discussions, and rationale live in the reference implementation's source comments and commit history.

## 16. Trademarks

`SkilLock` and `skil-lock` are not affiliated with or endorsed by Skil power tools (a brand owned by Chervon Group). `Claude` and `Claude Code` are trademarks of Anthropic PBC; `Codex` is a trademark of OpenAI, OpCo, LLC. Use of those names in this document is purely descriptive (nominative fair use); this spec does not imply affiliation with or endorsement by either company.
