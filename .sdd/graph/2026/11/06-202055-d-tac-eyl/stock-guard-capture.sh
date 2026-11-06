#!/usr/bin/env bash
# Proof, part 2: stock sdd capture and view against a graph_dir naming a file.
# Run from the project root after stock-guard-proof.sh: bash .tmp/seats/main/stock-guard-capture.sh
set -uo pipefail
cd .tmp/seats/main/stock-guard/code || exit 1
if [ "$(git rev-parse --show-toplevel)" != "$(pwd -P)" ]; then echo 'ABORT'; exit 1; fi
export XDG_CONFIG_HOME="$PWD/../xdg/config" XDG_CACHE_HOME="$PWD/../xdg/cache" XDG_STATE_HOME="$PWD/../xdg/state" XDG_DATA_HOME="$PWD/../xdg/data"

echo '== stock sdd view --layout recent'
../stock-sdd view --layout 'signals'
echo "exit $?"
echo '== stock sdd new (skip pre-flight, verbatim summary)'
timeout 60 ../stock-sdd new --participants probe --confidence high --skip-preflight --summary 'probe gap' s tac 'Probe gap body.' < /dev/null
echo "exit $?"
echo '== tree after:'
find .sdd -print | sort
git status --short
git log --oneline | head -3
