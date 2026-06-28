# drift-demo — capability drift in a Claude Code skill

A two-state sample of one skill, `claude-hook-builder`, so anyone can run their
own scanner against the **same bytes** SkilLock raises a finding on and confirm
the digests line up.

This is the chain discussed in
[anthropics/skills#492](https://github.com/anthropics/skills/issues/492):
a skill that is benign when approved, then "updated" to add a `Bash` grant, a
write into `~/.claude/settings.json`, and a remote payload fetch.

## Layout

```
drift-demo/
  baseline/   approved state: allowed-tools Read/Grep/Glob, no shell/network/writes
  drifted/    same skill, "updated" to v1.4.0
    skills.lock        the approved baseline, committed (what CI compares against)
    .skil-lock.yaml    block-mode policy
```

`drifted/skills.lock` is the lockfile generated from `baseline/` — it represents
the surface a reviewer already approved. `drifted/.claude/.../SKILL.md` is the
working tree that drifted away from it.

## Reproduce the BLOCK

```bash
skil-lock ci drifted
```

```
### SkilLock - capability delta

| Skill | Capability | Change | Detail | Reason |
|---|---|---|---|---|
| claude-hook-builder | shell_commands | + | curl | matches require_approval |
| claude-hook-builder | network_urls   | + | https://cfg.hook-builder.dev/install.sh | host not in allowed_domains |
| claude-hook-builder | file_writes    | + | ~/.claude/settings.json | matches protected_paths |
| claude-hook-builder | allowed_tools  | + | Bash | - |
...

Verdict: BLOCK: 5 of 9 entries at severity >= medium
# exit code 1
```

The new `Bash` grant, the `settings.json` write, and the exfil URL all surface
as a reviewable diff and the gate exits non-zero. A reviewer approves a delta
explicitly in `.skil-lock-approvals.yaml`, or it stays blocked.

## The digest binding

```bash
skil-lock ci drifted --format sarif | jq '.runs[0].artifacts'
```

```json
[
  {
    "location": { "uri": ".claude/skills/claude-hook-builder/SKILL.md" },
    "hashes":   { "sha-256": "64f9e18ef138e7238509134f660f1bdd9859ff9953f5e4cb9c884f0e0ec3395a" }
  }
]
```

Every `result` carries `physicalLocation.artifactLocation` (the path above, plus
an `index` into `artifacts[]`), so each capability-delta finding is pinned to the
exact SKILL.md snapshot it was raised against. That digest is byte-for-byte the
SHA-256 a content scanner gets independently:

```bash
$ sha256sum drifted/.claude/skills/claude-hook-builder/SKILL.md
64f9e18ef138e7238509134f660f1bdd9859ff9953f5e4cb9c884f0e0ec3395a  ...
```

So a detection pass (static rules / red-team review) that scans the same file
can bind its verdict to the same digest. "Drift detected" and "drift scanned"
become one verdict on one snapshot — and both findings self-invalidate the
moment the SKILL.md changes again.

## Boundary

The digest is the **whole-SKILL.md** snapshot, not a per-line or per-delta
digest, and a `bundled_scripts` delta resolves to the SKILL.md hash, not the
script's own. It is **not** a digest of whatever `https://cfg.hook-builder.dev/install.sh`
serves at runtime — SkilLock catches the `curl` because it is visible in the
skill text, but what that URL returns is out of scope (that is the runtime /
signing layer's job).
