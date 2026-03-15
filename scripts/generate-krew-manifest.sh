#!/usr/bin/env bash

set -euo pipefail

version="${1:?usage: generate-krew-manifest.sh <version> [checksums-file] [template-file] [output-file]}"
checksums_file="${2:-dist/checksums.txt}"
template_file="${3:-krew-template.yaml}"
output_file="${4:-plugin.yaml}"

checksum_for() {
  local artifact="$1"
  awk -v artifact="$artifact" '$2 == artifact { print $1 }' "$checksums_file"
}

darwin_amd64="$(checksum_for "kubectl-crashloop_darwin_amd64.tar.gz")"
darwin_arm64="$(checksum_for "kubectl-crashloop_darwin_arm64.tar.gz")"
linux_amd64="$(checksum_for "kubectl-crashloop_linux_amd64.tar.gz")"
linux_arm64="$(checksum_for "kubectl-crashloop_linux_arm64.tar.gz")"
windows_amd64="$(checksum_for "kubectl-crashloop_windows_amd64.zip")"

for value in "$darwin_amd64" "$darwin_arm64" "$linux_amd64" "$linux_arm64" "$windows_amd64"; do
  if [[ -z "$value" ]]; then
    echo "missing checksum in $checksums_file" >&2
    exit 1
  fi
done

sed \
  -e "s/__VERSION__/${version}/g" \
  -e "s/__DARWIN_AMD64_SHA__/${darwin_amd64}/g" \
  -e "s/__DARWIN_ARM64_SHA__/${darwin_arm64}/g" \
  -e "s/__LINUX_AMD64_SHA__/${linux_amd64}/g" \
  -e "s/__LINUX_ARM64_SHA__/${linux_arm64}/g" \
  -e "s/__WINDOWS_AMD64_SHA__/${windows_amd64}/g" \
  "$template_file" > "$output_file"

echo "wrote ${output_file}"
