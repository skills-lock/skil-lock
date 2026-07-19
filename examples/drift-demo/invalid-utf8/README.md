# invalid-utf8 — a digest-basis discriminator

A third state next to [`baseline/`](../baseline) and [`drifted/`](../drifted),
requested in the multi-scanner envelope discussion
([anthropics/skills#492](https://github.com/anthropics/skills/issues/492),
[claude-code-skill-security-check#24](https://github.com/aliksir/claude-code-skill-security-check/issues/24)).

`baseline/` and `drifted/` are both well-formed UTF-8, so every scanner agrees
on their SHA-256 **regardless of whether it hashes the raw file bytes or a
UTF-8-decoded string**. That means they cannot catch the one place the four
envelope emitters silently disagree: what the digest is actually taken over.

This fixture's `SKILL.md` carries two invalid-UTF-8 byte sequences in an HTML
comment in the body:

- a lone `0x80` — an orphan UTF-8 continuation byte, and
- a lone surrogate `U+D800`, WTF-8-encoded as `ED A0 80`.

Everything else in the file is ordinary UTF-8.

## The two bases diverge here

```bash
sha256sum invalid-utf8/.claude/skills/claude-hook-builder/SKILL.md
# 0e5f6446b6c4e104a00a87655b759c4a5e5e6031b71f101a59e89156613d365b
```

| Basis | Digest | Who emits it |
|---|---|---|
| **raw octets** (normative) | `0e5f6446…613d365b` | skil-lock, skill-scanner, `sha256sum` |
| UTF-8-decode-then-hash | `eec1f132…57808c1d` | ATR, prompt-defense-audit (pre-fix) |

skil-lock's `content_hash` matches the raw-octet value byte-for-byte — it hashes
`os.ReadFile` output directly (`internal/parser/claude/parser.go`):

```bash
skil-lock scan invalid-utf8 --format json | \
  jq -r '.[0].content_hash // .skills[].content_hash'
# sha256:0e5f6446b6c4e104a00a87655b759c4a5e5e6031b71f101a59e89156613d365b
```

## Why raw octets is the sound basis

The decode-then-hash basis substitutes `U+FFFD` for each invalid sequence
*before* hashing, and that substitution is **many-to-one**. Two files that
differ only in their invalid bytes collapse to one digest:

| File | raw-octet digest | string digest |
|---|---|---|
| this fixture (`…80 ED A0 80…`) | `0e5f6446…` | `eec1f132…` |
| same file, lone byte flipped to `0x81` | `1546f3f1…` | `eec1f132…` |

Under the string basis the two files share a digest, so a clean verdict on one
can be replayed as authoritative for the other and the cross-scanner join
*succeeds while being wrong* — a worse failure than a mismatch, because nothing
surfaces it. Raw octets has no such degenerate case, and it is the only basis a
third party can recompute with `sha256sum` and no knowledge of any scanner's
tooling — which is the whole point of a cross-layer key.

The proposed RFC wording that removes the ambiguity:

> The digest MUST be SHA-256 over the artifact's raw octets as stored, before
> any decoding, rendered as 64-character lowercase hex, and MUST equal
> `sha256sum` of that artifact. Implementations MUST NOT hash a decoded,
> normalized, or trimmed string representation.
