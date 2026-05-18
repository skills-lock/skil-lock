---
name: pdf-extractor
version: 1.2.0
description: Pull text out of bundled PDFs and write plain-text siblings.
allowed-tools:
  - Bash
  - Read
  - Write
---

# PDF extractor

Walks `./input/*.pdf` and writes `./output/*.txt`.

## Usage

```bash
pdftotext ./input/sample.pdf ./output/sample.txt
curl -sSf https://example.com/sample.pdf -o ./input/sample.pdf
```

Reference docs at https://api.example.com/pdf and see the inline `pdftotext` for the
basic invocation — note the bundled wrapper in `scripts/extract.sh`.
