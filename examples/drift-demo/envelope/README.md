# Scan-report envelope — reference vector

This directory is the canonical conformance fixture for the multi-scanner
**scan-report envelope** defined in [`SPEC.md` §14.3](../../../SPEC.md#143-multi-scanner-report-envelope-interop-profile).

The envelope is a thin SARIF v2.1.0 profile that lets independent skill
scanners — drift detectors, content scanners, agentic-threat-rule engines —
emit reports that a consumer can **merge on the SHA-256 content digest**
without recomputing anything, while keeping each finding attributable to the
layer that produced it.

## Files

| File | What it is |
|---|---|
| [`skil-lock.sarif.json`](skil-lock.sarif.json) | Real, unedited output of `skil-lock ci drifted --format sarif` over the [`drifted/`](../drifted) skill. The `drift` layer of the envelope. |

The profile schema is [`schemas/scan-envelope-v0.1.json`](../../../schemas/scan-envelope-v0.1.json).

**Regenerating.** This file is tool output, not a hand-maintained document,
and a fixture that silently describes an older document shape than the tool
now emits is worse than no fixture. `make vector` rewrites it; at release
time it is regenerated with the tag being cut (`make vector VERSION=0.2.5`),
because the report carries version-pinned links into `SPEC.md` — see
[§14.5](../../../SPEC.md#145-how-to-read-a-clean-run). The copy committed
here is a development build, so `driver.version` reads `dev` and those links
resolve to `main`; a released build pins them to its own tag.

## The join key

Every result in the vector resolves — via `artifactLocation.index` — to this
one artifact:

```
.claude/skills/claude-hook-builder/SKILL.md
sha-256: 64f9e18ef138e7238509134f660f1bdd9859ff9953f5e4cb9c884f0e0ec3395a
```

That digest is the byte-for-byte SHA-256 of the file, independently
reproducible:

```bash
sha256sum drifted/.claude/skills/claude-hook-builder/SKILL.md
# 64f9e18ef138e7238509134f660f1bdd9859ff9953f5e4cb9c884f0e0ec3395a

jq -r '.runs[0].artifacts[0].hashes["sha-256"]' envelope/skil-lock.sarif.json
# 64f9e18ef138e7238509134f660f1bdd9859ff9953f5e4cb9c884f0e0ec3395a
```

Any other scanner that runs over the same `drifted/` SKILL.md and emits the
same `sha-256` is, by construction, join-compatible with this report — a
registry can merge the two on that digest with no coordination between the
tools.

## Conformance check (any tool, any layer)

A SARIF document is envelope-conformant for this fixture when, for every
result:

1. it resolves to an `artifacts[]` entry carrying a `sha-256`, via
   `artifactLocation.index`;
2. it carries a non-empty `properties.layer`; and
3. the run names its `tool.driver.name` + `version`.

A `jq` smoke check — prints `true` when every result is conformant, `false`
otherwise:

```bash
jq '
  .runs[0] as $r
  | ($r.artifacts // []) as $arts
  | [ $r.results[]
      | (((.properties.layer? // "") | type) == "string"
         and ((.properties.layer? // "") | length) > 0) as $hasLayer
      | ((.locations // []) | length == 0
         or ( (.[0].physicalLocation.artifactLocation.index) as $i
              | $i != null
                and (($arts[$i].hashes["sha-256"]? // "") | test("^[0-9a-f]{64}$")) )) as $bound
      | ($hasLayer and $bound)
    ] | all
' envelope/skil-lock.sarif.json
```

## Adding another layer

If you maintain a content scanner or an ATR engine, emit the same shape over
`drifted/` with your own `properties.layer` (`content`, `atr`, …) and the
same `artifacts[].hashes["sha-256"]`. All layers will then conformance-pass
on one fixture, and a fourth tool can drop in later with no further
coordination. Issues and proposals: please open a PR or comment on the spec.
