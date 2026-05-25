#!/usr/bin/env python3
"""Generate an asciinema v2 cast file for the SkilLock demo GIF.

Pipeline: this script → demo.cast → `agg` → demo.gif
"""
import json
import os
import time

WIDTH = 120
HEIGHT = 32

events = []
t = 0.0

# ANSI helpers
RESET = "\x1b[0m"
GREEN = "\x1b[1;32m"
CYAN = "\x1b[1;36m"
GREY = "\x1b[90m"
RED = "\x1b[1;31m"
YELLOW = "\x1b[1;33m"
BOLD = "\x1b[1m"

def emit(s: str, dt: float = 0.0):
    global t
    t += dt
    events.append([round(t, 4), "o", s])

def prompt():
    emit(f"{GREEN}${RESET} ", dt=0.18)

def type_cmd(cmd: str, cps: int = 32):
    delay = 1.0 / cps
    for ch in cmd:
        emit(ch, dt=delay)

def enter():
    emit("\r\n", dt=0.06)

def output(text: str, lead: float = 0.20, post: float = 0.0):
    global t
    if lead > 0:
        t += lead
    text = text.replace("\n", "\r\n")
    events.append([round(t, 4), "o", text])
    if post > 0:
        t += post

def sleep(d: float):
    global t
    t += d

# Tape ---------------------------------------------------------------

# Title banner
emit(f"{CYAN}# SkilLock - pin approved AI skill behavior, block drift in CI{RESET}\r\n", dt=0.0)
sleep(0.7)
emit(f"{GREY}# one skill in .claude/skills/changelog-summary/. no lockfile yet.{RESET}\r\n", dt=0.0)
sleep(0.4)

# 1. Pin the baseline
prompt()
type_cmd("skil-lock init --baseline .")
enter()
output("baseline written: skills.lock (1 skills)\r\n", lead=0.30, post=0.9)

# 2. Drift — narrate, don't type the messy heredoc
prompt()
type_cmd(f"{GREY}# (someone edits the SKILL.md to add a curl + external POST){RESET}")
enter()
sleep(0.9)

# 3. CI catches it
prompt()
type_cmd("skil-lock ci")
enter()
block_output = (
    f"### {BOLD}SkilLock - capability delta{RESET}\r\n\r\n"
    "Comparing `skills.lock` (baseline) vs `<working tree>` (current).\r\n\r\n"
    "| Skill              | Capability     | Δ | Detail                                  | Reason                       |\r\n"
    "|--------------------|----------------|---|-----------------------------------------|------------------------------|\r\n"
    f"| changelog-summary  | shell_commands | {YELLOW}+{RESET} | `curl`                                  | matches require_approval     |\r\n"
    f"| changelog-summary  | network_urls   | {YELLOW}+{RESET} | https://internal.example.com/notify     | host not in allowed_domains  |\r\n\r\n"
    f"{BOLD}Verdict:{RESET} {RED}BLOCK: 2 of 2 entries at severity >= medium{RESET}\r\n\r\n"
    f"{GREY}To approve, paste the snippet shown by ci into .skil-lock-approvals.yaml.{RESET}\r\n"
)
output(block_output, lead=0.35, post=3.0)

# 4. Reviewer pastes the approval
prompt()
type_cmd(f"{GREY}# reviewer pastes the snippet, fills in their reason, and re-runs{RESET}")
enter()
sleep(0.8)

# 5. CI passes after approval
prompt()
type_cmd("skil-lock ci --approvals .skil-lock-approvals.yaml")
enter()
pass_output = (
    f"{GREY}approved: skill=changelog-summary reviewer=alice reason=\"release notify ping\"{RESET}\r\n"
    f"{GREY}approved: skill=changelog-summary reviewer=alice reason=\"release notify ping\"{RESET}\r\n"
    f"### {BOLD}SkilLock - no capability deltas{RESET}\r\n\r\n"
    "Baseline `skills.lock` matches current `<working tree>`.\r\n\r\n"
    f"{BOLD}Verdict:{RESET} {GREEN}PASS: no capability deltas{RESET}\r\n"
)
output(pass_output, lead=0.35, post=2.5)

prompt()
sleep(0.8)

# --- Write cast --------------------------------------------------------
header = {
    "version": 2,
    "width": WIDTH,
    "height": HEIGHT,
    "timestamp": int(time.time()),
    "env": {"SHELL": "/bin/bash", "TERM": "xterm-256color"},
    "title": "SkilLock demo",
}

out = os.environ.get("OUT", "/tmp/skilock-demo.cast")
with open(out, "w") as f:
    f.write(json.dumps(header) + "\n")
    for ev in events:
        f.write(json.dumps(ev) + "\n")

print(f"wrote {out}: {len(events)} events, total {t:.1f}s")
