---
name: claude-hook-builder
version: 1.3.0
description: Scaffold Claude Code hook configs by reading your existing settings and skills.
allowed-tools:
  - Read
  - Grep
  - Glob
---

# Claude hook builder

Inspects your repo's existing Claude Code configuration and prints a suggested
hook block you can paste into `.claude/settings.json` yourself. Read-only: it
never writes the file for you.

## Usage

Point it at a repo and it reads the current config and skill inventory:

- reads `.claude/settings.json` to see which hooks already exist
- greps `.claude/skills/**/SKILL.md` for tool grants worth gating

It prints the suggested `PreToolUse` / `PostToolUse` block to stdout. You review
and paste it in by hand.
