#!/usr/bin/env bash
# Proof: treeless partial clone (--filter=tree:0) + cone sparse on .sdd against the
# synthetic origin sparse-clone-proof.sh builds; pack size, commit count, and what a
# merge pull and a history read fetch on demand.
# Run from the project root after sparse-clone-proof.sh: bash .tmp/seats/main/treeless-proof.sh
set -euo pipefail
base=.tmp/seats/main/sparse-proof
abs="$PWD/$base"
origin_url="file://$abs/origin.git"

top=$(git -C "$base/origin.git" rev-parse --absolute-git-dir)
if [ "$top" != "$abs/origin.git" ]; then
  echo "ABORT: origin resolves to $top"
  exit 1
fi

pack_size() {
  git -C "$1" count-objects -v | sed -n 's/^size-pack: //p'
}

echo '== treeless clone + sparse .sdd'
time git clone -q --filter=tree:0 --no-checkout --sparse "$origin_url" "$base/treeless"
if [ "$(git -C "$base/treeless" rev-parse --show-toplevel)" != "$abs/treeless" ]; then
  echo 'ABORT: treeless clone missing'
  exit 1
fi
echo "pack after clone (KiB): $(pack_size "$base/treeless")"
git -C "$base/treeless" sparse-checkout set --cone .sdd
git -C "$base/treeless" checkout -q main
echo "pack after checkout (KiB): $(pack_size "$base/treeless")"
du -sh "$base/treeless" "$base/treeless/.git"
echo "commits known: $(git -C "$base/treeless" rev-list --count main)"

echo
echo '== merge pull of the commit the write path added to origin'
git -C "$base/treeless" config user.email probe@example.invalid
git -C "$base/treeless" config user.name probe
printf -- '---\ntype: signal\n---\nlocal\n' > "$base/treeless/.sdd/graph/2099/02/local.md"
git -C "$base/treeless" add -- .sdd/graph/2099/02/local.md
git -C "$base/treeless" commit -qm "test: local entry" -- .sdd/graph/2099/02/local.md
echo "pack before pull (KiB): $(pack_size "$base/treeless")"
time git -C "$base/treeless" pull -q --no-rebase --no-edit origin main
echo "pack after pull (KiB): $(pack_size "$base/treeless")"

echo
echo '== history read: log of one entry over all commits (fetches trees on demand)'
time git -C "$base/treeless" log --oneline -- .sdd/graph/2099/02/churn.md | wc -l
echo "pack after history read (KiB): $(pack_size "$base/treeless")"
