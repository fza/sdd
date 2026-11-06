#!/usr/bin/env bash
# Proof: an entry renamed upstream while its title line changed locally merges
# cleanly, the change landing at the new path (ort rename detection).
# Run from the project root: bash .tmp/seats/main/rename-proof.sh
set -euo pipefail
mkdir -p .tmp/seats/main/rename-proof/repo/.sdd/graph/2026/11
cd .tmp/seats/main/rename-proof/repo
git init -q -b main
if [ "$(git rev-parse --show-toplevel)" != "$(pwd -P)" ]; then echo 'ABORT'; exit 1; fi
git config user.email probe@example.invalid
git config user.name probe
cp ../../../../../.sdd/graph/2026/07/13-214050-d-cpt-65i.md .sdd/graph/2026/11/06-120000-d-cpt-abc.md
git add -A
git commit -qm "test: base"
git checkout -qb upstream
mkdir -p .sdd/graph/2026/12
git mv .sdd/graph/2026/11/06-120000-d-cpt-abc.md .sdd/graph/2026/12/06-120000-d-cpt-abc.md
git commit -qm "test: rename"
git checkout -q main
sed -i '' 's/^title: Explicit mutation authority$/title: Changed title/' .sdd/graph/2026/11/06-120000-d-cpt-abc.md
git commit -qam "test: title"
if git merge -q --no-edit upstream; then echo merged; else echo conflict; fi
git status --short
echo "files: $(git ls-files .sdd/graph | tr '\n' ' ')"
grep '^title:' .sdd/graph/2026/12/06-120000-d-cpt-abc.md
