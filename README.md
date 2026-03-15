# kubectl-crashloop

`kubectl-crashloop` is a focused `kubectl` plugin and standalone CLI for inspecting pod crash history. It merges warning Events, `LastTerminationState`, and previous logs into one readable terminal report, with JSON output for automation.

## Features

- Direct pod UX: `kubectl crashloop POD` or `kubectl-crashloop POD`
- Human-first terminal output styled with `lipgloss` and `lipgloss/table`
- Deterministic `demo` command for README screenshots, VHS tapes, and release notes
- JSON output for scripts and incident tooling
- Cross-platform release packaging with GoReleaser and Krew metadata generation

## Quick Start

```bash
go run . demo
go run . my-pod -n production
go run . my-pod -n production -o json
```

## Usage

```bash
kubectl-crashloop POD [-n namespace] [--context CTX] [--kubeconfig PATH] [-c container] [--tail 5] [--limit 10] [-o table|json] [--color auto|always|never]
```

Examples:

```bash
kubectl crashloop payments-api-6d9c9b77d9-x2n5k -n production
kubectl-crashloop payments-api-6d9c9b77d9-x2n5k -n production -c api --tail 10
kubectl-crashloop payments-api-6d9c9b77d9-x2n5k -n production -o json
kubectl-crashloop demo
```

## Permissions

This plugin needs namespace-scoped read access to pods, previous logs, and warning Events.

Required verbs:

```text
- get   pods
- get   pods/log
- list  events
```

Example Role:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: kubectl-crashloop
rules:
  - apiGroups: [""]
    resources: ["pods"]
    verbs: ["get"]
  - apiGroups: [""]
    resources: ["pods/log"]
    verbs: ["get"]
  - apiGroups: [""]
    resources: ["events"]
    verbs: ["list"]
```

## Development

Prerequisites:

- Go 1.26
- `kubectl`
- `goreleaser` for release validation
- `vhs` if you want to record the README demo

Common commands:

```bash
go test ./...
go vet ./...
go run . demo
goreleaser check
goreleaser build --snapshot --clean
```

`demo` forces color output and a fixed 100-column render width so VHS output stays stable across environments.

## Release Flow

1. Tag a release, for example `v0.1.0`.
2. GitHub Actions runs GoReleaser and uploads cross-platform archives and checksums.
3. Generate the Krew manifest:

```bash
./scripts/generate-krew-manifest.sh v0.1.0
```

4. Validate locally before opening the Krew PR:

```bash
kubectl krew install --manifest=./plugin.yaml --archive=./dist/kubectl-crashloop_darwin_arm64.tar.gz
```

## Manual Smoke Test

Point the CLI at a pod with at least one restart:

```bash
kubectl-crashloop my-pod -n production
kubectl-crashloop my-pod -n production -o json
```

Check that:

- Warning Events appear when available
- Baseline `LastTerminationState` appears even if Events have expired
- Previous logs attach to the matching container row
- JSON output contains no ANSI escape sequences
