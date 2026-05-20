# Contributing to skil-lock

Thanks for your interest. The project is early; the surface is intentionally narrow (see the "What's NOT in v0.1" section in the [README](./README.md) for the list). Issues and PRs that fit the locked scope are very welcome.

## Quick start

Requirements: Go 1.22+, `golangci-lint`.

```bash
git clone git@github.com:skills-lock/skil-lock.git
cd skil-lock
go test ./...
go build ./...
```

## Development loop

```bash
# Run a single test
go test ./internal/parser -run TestClaudeCodeFrontmatter -v

# Lint locally (matches CI)
golangci-lint run

# Run the CLI against a fixture
go run ./cmd/skil-lock scan ./testdata/skills/pdf-extractor
```

## Pull requests

1. **Open an issue first** for non-trivial changes — keeps scope discussions out of PR review.
2. **One logical change per PR.** Squash-merge is the default.
3. **Tests required.** Detectors need positive + negative fixtures. Parsers need round-trip tests.
4. **Sign your commits.** SSH or GPG. `git config --global commit.gpgsign true`. Branch protection rejects unsigned commits on `main`.
5. **CLA.** The first PR from a new contributor triggers a one-time CLA via [cla-assistant.io](https://cla-assistant.io) (web-based, ~30 seconds).
6. **Docs.** If you change the lockfile schema, policy schema, CLI surface, or detector behavior, update [`SPEC.md`](./SPEC.md) and the relevant README section in the same PR.

## Reporting bugs / security issues

- **Bugs:** [open an issue](https://github.com/skills-lock/skil-lock/issues/new/choose) with a reproduction.
- **Security vulnerabilities:** see [`SECURITY.md`](./SECURITY.md). Do **not** file a public issue.

## License

By contributing, you agree your contributions will be licensed under the [Apache License 2.0](./LICENSE).
