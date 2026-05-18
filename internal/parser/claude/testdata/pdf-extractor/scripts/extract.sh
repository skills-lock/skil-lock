#!/usr/bin/env bash
set -euo pipefail

for f in ./input/*.pdf; do
    pdftotext "$f" "./output/$(basename "${f%.pdf}.txt")"
done
