# SkilLock — docs

This directory holds documentation assets that live alongside the binary
source.

## Demo GIF

`demo.gif` is the README hero showing the full clean → drift → BLOCK →
approve → PASS arc. ~20 seconds, dracula theme, 120 cols × 32 rows.

### Regenerating

The GIF is produced from a hand-authored asciinema v2 cast file, which is
in turn produced by `build_cast.py`. No human typing involved — the cast
is fully scripted so it can be re-run after every UX tweak.

```bash
# 1. Produce the cast file
python3 docs/build_cast.py
# (writes /tmp/skilock-demo.cast by default; override with OUT=...)

# 2. Render to GIF
agg \
  --theme dracula \
  --font-size 13 \
  --cols 120 \
  --rows 32 \
  --fps-cap 15 \
  --last-frame-duration 4 \
  /tmp/skilock-demo.cast \
  docs/demo.gif
```

`agg` is the asciinema GIF renderer (`asciinema/agg` on GitHub). One
static binary, no chrome / ttyd / ffmpeg required.

### Why scripted instead of recorded

A recorded session bakes the recorder's typing speed and prompt style
into the artifact. The scripted approach keeps the GIF
deterministic — same cast file always produces the same GIF — which
matters for README hero shots that need to look identical across
re-renders.

If you change the wedge framing in the README, edit `build_cast.py`,
re-render, commit both files (the script and the rendered output). Don't
edit `demo.gif` directly.
