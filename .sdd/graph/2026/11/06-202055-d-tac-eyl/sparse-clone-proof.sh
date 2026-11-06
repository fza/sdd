#!/usr/bin/env bash
# Proof: an sdd-owned clone holding only .sdd/ (step 1) and, when graph_dir lies
# elsewhere, that directory too (step 2), via partial clone + cone sparse checkout.
# Compares disk use with a full clone, then commits, publishes and pulls through it.
# Origin is a synthetic one-commit repo: this repo's graph plus internal/ as code.
# Run from the project root: bash .tmp/seats/main/sparse-clone-proof.sh
set -euo pipefail
base=.tmp/seats/main/sparse-proof
abs="$PWD/$base"

own_repo() {
  # own_repo <dir>: stop unless <dir> is the top of its own repository, so no
  # command can fall through to an enclosing repository.
  local top
  top=$(git -C "$1" rev-parse --show-toplevel)
  if [ "$top" != "$(cd "$1" && pwd -P)" ]; then
    echo "ABORT: $1 resolves to $top"
    exit 1
  fi
}

mkdir -p "$base/seed"
git -C "$base/seed" init -q -b main
own_repo "$base/seed"
mkdir -p "$base/seed/.sdd" "$base/seed/src" "$base/seed/docs"
cp .sdd/config.yaml "$base/seed/.sdd/config.yaml"
cp -R .sdd/graph "$base/seed/.sdd/graph"
cp -R internal "$base/seed/src/internal"
cp -R docs/. "$base/seed/docs/"
git -C "$base/seed" config user.email probe@example.invalid
git -C "$base/seed" config user.name probe
git -C "$base/seed" add -A
git -C "$base/seed" commit -qm "test: seed"
# History: 300 commits, each rewriting a code file outside .sdd and a graph file.
mkdir -p .tmp/seats/main/sparse-proof/seed/.sdd/graph/2099/02
for run in $(seq 300); do
  head -c 20000 /dev/urandom | base64 > .tmp/seats/main/sparse-proof/seed/src/churn.txt
  printf -- '---\ntype: signal\n---\nchurn %s\n' "$run" > .tmp/seats/main/sparse-proof/seed/.sdd/graph/2099/02/churn.md
  git -C "$base/seed" add -A
  git -C "$base/seed" commit -qm "test: churn $run"
done
git clone -q --bare "$base/seed" "$base/origin.git"
git -C "$base/origin.git" config uploadpack.allowFilter true
git -C "$base/origin.git" config uploadpack.allowAnySHA1InWant true
origin_url="file://$abs/origin.git"

echo "== origin content: $(git -C "$base/origin.git" ls-tree -r --name-only main | wc -l | tr -d ' ') files, graph $(git -C "$base/origin.git" ls-tree -r --name-only main -- .sdd/graph | wc -l | tr -d ' ')"

echo
echo '== full clone'
time git clone -q "$origin_url" "$base/full"
own_repo "$base/full"
du -sh "$base/full" "$base/full/.git"

echo
echo '== step 1: blobless, no checkout, cone sparse on .sdd'
time git clone -q --filter=blob:none --no-checkout --sparse "$origin_url" "$base/sparse"
own_repo "$base/sparse"
git -C "$base/sparse" sparse-checkout set --cone .sdd
git -C "$base/sparse" checkout -q main
du -sh "$base/sparse" "$base/sparse/.git"
echo "top-level entries: $(ls -A "$base/sparse" | tr '\n' ' ')"
echo "graph files: $(find "$base/sparse/.sdd/graph" -name '*.md' | wc -l | tr -d ' ')"

echo
echo '== step 2: graph_dir outside .sdd (docs/ stands in), added to the cone'
git -C "$base/sparse" sparse-checkout add docs
echo "top-level entries: $(ls -A "$base/sparse" | tr '\n' ' ')"
du -sh "$base/sparse" "$base/sparse/.git"

echo
echo '== downloaded pack size per clone kind (size-pack, KiB)'
pack_size() {
  git -C "$1" count-objects -v | sed -n 's/^size-pack: //p'
}
git clone -q --depth=1 --filter=blob:none --no-checkout --sparse "$origin_url" "$base/shallow"
own_repo "$base/shallow"
git -C "$base/shallow" sparse-checkout set --cone .sdd
git -C "$base/shallow" checkout -q main
echo "full:                       $(pack_size "$base/full")"
echo "blobless + sparse .sdd:     $(pack_size "$base/sparse")"
echo "shallow 1 + blobless + sparse: $(pack_size "$base/shallow")"
echo "commits known: full $(git -C "$base/full" rev-list --count main), blobless $(git -C "$base/sparse" rev-list --count main), shallow $(git -C "$base/shallow" rev-list --count main)"

echo
echo '== write path'
git -C "$base/sparse" config user.email probe@example.invalid
git -C "$base/sparse" config user.name probe
mkdir -p "$base/sparse/.sdd/graph/2099/01"
printf -- '---\ntype: signal\n---\nprobe\n' > "$base/sparse/.sdd/graph/2099/01/01-000000-s-tac-zzz.md"
git -C "$base/sparse" add -- .sdd/graph/2099/01/01-000000-s-tac-zzz.md
git -C "$base/sparse" commit -qm "test: probe entry" -- .sdd/graph/2099/01/01-000000-s-tac-zzz.md
git -C "$base/origin.git" fetch -q "$abs/sparse" main:main
echo "origin has sparse-made entry: $(git -C "$base/origin.git" cat-file -t main:.sdd/graph/2099/01/01-000000-s-tac-zzz.md)"
git -C "$base/full" config user.email probe@example.invalid
git -C "$base/full" config user.name probe
git -C "$base/full" pull -q --no-rebase origin main
printf 'outside the cone\n' > "$base/full/src/probe.txt"
printf -- '---\ntype: signal\n---\nfrom full\n' > "$base/full/.sdd/graph/2099/01/01-000001-s-tac-yyy.md"
git -C "$base/full" add -- src/probe.txt .sdd/graph/2099/01/01-000001-s-tac-yyy.md
git -C "$base/full" commit -qm "test: elsewhere"
git -C "$base/origin.git" fetch -q "$abs/full" main:main
git -C "$base/sparse" pull -q --no-rebase origin main
echo "sparse got the other entry: $(test -f "$base/sparse/.sdd/graph/2099/01/01-000001-s-tac-yyy.md" && echo yes || echo no)"
echo "sparse worktree holds src/: $(test -e "$base/sparse/src" && echo yes || echo no)"
echo "sparse status: $(git -C "$base/sparse" status --short | wc -l | tr -d ' ') changed paths"
du -sh "$base/sparse" "$base/sparse/.git"
