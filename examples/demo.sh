#!/usr/bin/env bash
# Demonstrates modtrust against a real, disposable environment: first a
# clean one, then one with every dangerous setting from the README turned
# on at once, using the real `go env -w` (not a mock) pointed at a
# throwaway GOENV file. Safe to run repeatedly; it never touches your real
# `go env -w` configuration.
set -euo pipefail

cd "$(dirname "$0")/.."
go build -o /tmp/modtrust-demo ./cmd/modtrust

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

echo "### A clean environment ###"
env -i PATH="$PATH" HOME="$HOME" GOENV="$WORK/clean-env" /tmp/modtrust-demo --no-sum
echo "(exit code: $?)"

echo
echo "### Every dangerous setting from the README, at once ###"
export GOENV="$WORK/dirty-env"
go env -w \
  GOSUMDB=off \
  GOPROXY=direct \
  GOPRIVATE='*' \
  GOINSECURE='*' \
  GOFLAGS=-mod=mod

set +e
env -i PATH="$PATH" HOME="$HOME" GOENV="$GOENV" GONOSUMCHECK=1 /tmp/modtrust-demo --no-sum --fail-on critical
code=$?
set -e
echo "(exit code: $code, would fail CI with --fail-on critical)"
