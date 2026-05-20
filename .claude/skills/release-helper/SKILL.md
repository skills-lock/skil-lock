---
name: release-helper
description: |
  Use when preparing a new tagged release of skil-lock. Inspects recent
  commits since the last tag, drafts release notes against the
  conventional-commit prefixes in this repo (feat, fix, build, docs,
  test, chore), and shows the version bump that GoReleaser will apply.
  Does not push tags or publish releases — those steps stay manual.
---

# release-helper

Helper for cutting a new tagged release. Surfaces the commits the
release would include and drafts notes in the same format the existing
GitHub release pages use.

## When to use

Invoke this skill from `~/Projects/skil-lock/` (the repository root)
just before tagging a release. The skill is read-only with respect to
the repository; it never modifies tracked files.

## What it does

1. Find the previous tag:

```bash
git describe --tags --abbrev=0
```

2. List the commits since then:

```bash
git log "${PREV_TAG}..HEAD" --pretty=format:'%h %s'
```

3. Group commits by conventional-commit prefix (`feat:`, `fix:`,
   `build:`, `docs:`, `test:`, `chore:`) into a draft markdown
   document.

4. Read the existing changelog so the draft slots in cleanly:

```bash
cat CHANGELOG.md
```

5. Print the proposed next tag based on the prefixes seen
   (`fix:` only → patch bump; `feat:` present → minor bump;
   anything labeled `BREAKING CHANGE:` → major bump).

## What it does not do

- Never runs `git tag`, `git push`, or `gh release create` — those
  remain manual so a human reviews the draft.
- Does not touch the `.goreleaser.yml` file.
- Does not call any external network endpoint.

## Constraints for the agent

Run only the commands listed above (`git describe`, `git log`,
`cat CHANGELOG.md`). Do not interpret unrelated arguments; if the user
asks for something outside the scope of preparing a release draft,
ask them to clarify instead of running additional shell commands.
