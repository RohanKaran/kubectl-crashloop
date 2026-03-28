# Contributing to kubectl-crashloop

Thanks for your interest in improving `kubectl-crashloop`. This project is a focused Go CLI and `kubectl` plugin for inspecting pod crash history, so the most helpful contributions usually improve crash inspection quality, terminal UX, JSON output, packaging, docs, or tests.

## Before you start

- Check the existing issues before starting work.
- Open or comment on an issue first for larger features, behavior changes, or workflow changes so the approach can be aligned before you invest time in implementation.
- Keep pull requests focused. Small, reviewable changes are easier to validate and ship.

## Local setup

Prerequisites:

- Go 1.26+
- `kubectl`
- `goreleaser` if you want to validate release packaging
- `vhs` if you want to refresh terminal demos or screenshots

Clone the repo, create a branch for your work, and use the commands below while iterating:

```bash
go test ./...
go test -race ./...
go vet ./...
go run . demo
```

If your change affects packaging or release metadata, also run:

```bash
goreleaser check
goreleaser build --snapshot --clean
```

## What to include in a contribution

- Add or update tests when behavior changes.
- Update `README.md` when flags, output, install steps, examples, or documented workflows change.
- Refresh demo-related assets or commands when terminal output changes in a way that affects screenshots, VHS tapes, or release notes.
- Keep output stable and readable for both terminal users and JSON consumers.

## Pull request expectations

- Describe what changed and why.
- Note the commands you ran to validate the change.
- Link the related issue when there is one.
- Call out any documentation, demo, or release impact.

## Security reports

Do not open public issues for vulnerabilities. Please use the private reporting flow described in [SECURITY.md](SECURITY.md).

## Licensing

By submitting a contribution, you agree that your work will be licensed under the Apache 2.0 license used by this repository.
