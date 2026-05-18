---
name: malformed
version: 1.0
allowed-tools: [Bash, Read
---

# Broken YAML

The frontmatter has an unclosed sequence; yaml.v3 should refuse it and the
parser should return ErrMalformedFrontmatter.
