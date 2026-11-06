#!/usr/bin/env bash
# Proof: stock sdd (branch main) in a code repo whose committed graph_dir names a
# file fails loudly on read and capture instead of creating a graph there.
# Run from the project root: bash .tmp/seats/main/stock-guard-proof.sh
set -euo pipefail
base=.tmp/seats/main/stock-guard
mkdir -p "$base/src"
git archive main | tar -x -C "$base/src"
(cd "$base/src" && env -u GOROOT go build -o ../stock-sdd ./cmd/sdd)
.tmp/seats/main/stock-guard/stock-sdd --version || true

mkdir -p "$base/code/.sdd"
cd "$base/code"
git init -q -b main
if [ "$(git rev-parse --show-toplevel)" != "$(pwd -P)" ]; then echo 'ABORT'; exit 1; fi
git config user.email probe@example.invalid
git config user.name probe
printf 'graph_repo: github.com/acme/app-sdd\ngraph_dir: .sdd/GRAPH-ELSEWHERE.md\n' > .sdd/config.yaml
printf 'This project graph lives in github.com/acme/app-sdd.\n' > .sdd/GRAPH-ELSEWHERE.md
git add -A
git commit -qm "test: pointing code repo"
export XDG_CONFIG_HOME="$PWD/../xdg/config" XDG_CACHE_HOME="$PWD/../xdg/cache" XDG_STATE_HOME="$PWD/../xdg/state" XDG_DATA_HOME="$PWD/../xdg/data"

echo '== stock sdd view'
if ../stock-sdd view; then echo "exit 0"; else echo "exit $?"; fi
echo '== stock sdd search --term x'
if ../stock-sdd search --term x; then echo "exit 0"; else echo "exit $?"; fi
echo '== stock sdd new (signal)'
../stock-sdd new --help | head -40
echo '== tree after:'
find .sdd -print | sort
git status --short
