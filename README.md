# kubectl-crashloop

`kubectl-crashloop` is a focused `kubectl` plugin and standalone CLI for inspecting pod crash history. It merges warning Events, `LastTerminationState`, and container logs into one readable terminal report. When previous logs are unavailable, it can fall back to labeled current-container logs. JSON output is available for automation.

## Highlights

- Direct pod UX for a single pod: `kubectl crashloop POD` or `kubectl-crashloop POD`
- Best-effort log correlation: previous logs when available, labeled current-log fallback when not
- Human-first terminal output styled with `lipgloss` and `lipgloss/table`
- Deterministic `demo` command for README screenshots, VHS tapes, and release notes
- JSON output for scripts and incident tooling
- Cross-platform release packaging with GoReleaser and Krew metadata generation

## Install

### Krew

Krew is the recommended way to install the plugin. Once Krew is installed, install crashloop with:

If you do not have Krew yet, install it first from the [official Krew installation guide](https://krew.sigs.k8s.io/docs/user-guide/setup/install/).

```bash
kubectl krew install crashloop
```

After that, use it through `kubectl`:

```bash
kubectl crashloop version
kubectl crashloop demo
kubectl crashloop my-pod -n production
```

### GitHub Releases

Each release publishes archives for macOS, Linux, and Windows. Download the archive that matches your OS and architecture from the [GitHub Releases page](https://github.com/RohanKaran/kubectl-crashloop/releases), then unpack it and place `kubectl-crashloop` somewhere on your `PATH`.

Release artifact names:

- `kubectl-crashloop_darwin_amd64.tar.gz`
- `kubectl-crashloop_darwin_arm64.tar.gz`
- `kubectl-crashloop_linux_amd64.tar.gz`
- `kubectl-crashloop_linux_arm64.tar.gz`
- `kubectl-crashloop_windows_amd64.zip`

Example for a tarball:

```bash
tar -xzf kubectl-crashloop_<os>_<arch>.tar.gz
```

Move `kubectl-crashloop` to a directory in your `PATH`, for example `/usr/local/bin`, `~/bin`, or `~/.local/bin`.

Example for Windows:

Unpack `kubectl-crashloop.exe` from the `.zip` archive and add its directory to your `PATH`.

### From Source

If you have Go 1.26 or newer:

```bash
go install github.com/rohankaran/kubectl-crashloop@latest
kubectl-crashloop version
```

Ensure your Go bin directory, such as `$(go env GOPATH)/bin` or `~/go/bin`, is in your `PATH`.

## Usage

Use `kubectl crashloop` when installed through Krew, or `kubectl-crashloop` when running the standalone binary.

```bash
kubectl crashloop POD [-n namespace] [--context CTX] [--kubeconfig PATH] [-c container] [--tail 5] [--limit 10] [-o table|json] [--color auto|always|never]
kubectl-crashloop POD [-n namespace] [--context CTX] [--kubeconfig PATH] [-c container] [--tail 5] [--limit 10] [-o table|json] [--color auto|always|never]
```

This command is pod-scoped. Pass a pod name, not a Deployment name. If you want to inspect a Deployment, first pick one replica pod and run the tool against that pod.

Common flags:

- `-n, --namespace` optionally overrides the namespace from your current kubeconfig context. If no namespace is configured, the command falls back to `default`.
- `--context` chooses the kubeconfig context.
- `--kubeconfig` points at an alternate kubeconfig file.
- `-c, --container` limits output to one container.
- `--tail` controls how many previous log lines are fetched per container. Default: `5`.
- `--limit` caps the number of crash entries shown. Default: `10`.
- `-o, --output` switches between `table` and `json`. Default: `table`.
- `--color` controls ANSI color output. Default: `auto`.

Examples:

```bash
kubectl crashloop payments-api-6d9c9b77d9-x2n5k
kubectl crashloop payments-api-6d9c9b77d9-x2n5k -n production
kubectl crashloop payments-api-6d9c9b77d9-x2n5k -n production --context staging
kubectl crashloop demo
kubectl-crashloop payments-api-6d9c9b77d9-x2n5k -n production -c api --tail 10
kubectl crashloop payments-api-6d9c9b77d9-x2n5k -n production -o json
kubectl-crashloop demo
kubectl-crashloop version
```

`demo` is a built-in subcommand that prints a stable, non-cluster-dependent report for screenshots and docs. `version` prints build metadata.

## Scope And Limits

- Single-pod view: the command does not aggregate crash history across every pod in a Deployment, ReplicaSet, or StatefulSet.
- Best-effort crash context: it uses Kubernetes pod status, warning Events, and current/previous log APIs.
- Not a durable log store: older restart logs can still disappear from the node runtime. For retained historical logs across many restarts, use external log aggregation.

## Permissions

This plugin needs namespace-scoped read access to pods, current/previous logs, and warning Events.

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

- Go 1.26+
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
3. The release workflow runs `krew-release-bot`, which opens or updates the Krew PR automatically.
4. If you need to inspect the rendered Krew manifest locally, generate it manually from [krew-template.yaml](krew-template.yaml):

```bash
./scripts/generate-krew-manifest.sh v0.1.0
```

5. Validate locally before debugging a Krew PR:

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
- Previous logs attach to the matching container row when available
- Current logs appear as a labeled fallback when previous logs are unavailable
- JSON output contains no ANSI escape sequences
