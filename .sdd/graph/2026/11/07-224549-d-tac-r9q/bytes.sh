#!/bin/bash
# Bytes a fetch brings into a blobless .sdd-cone clone of this repository:
# --filter=blob:none (today) plus the merge's lazy fetch, against --no-filter.
# Usage: bytes.sh <empty work dir> <source repo> <branch> <since...>
set -euo pipefail
work="$1"; src="$2"; branch="$3"; shift 3
mkdir -p "$work"
git clone -q --bare --no-local "$src" "$work/src.git"
git -C "$work/src.git" config uploadpack.allowFilter true
git -C "$work/src.git" config uploadpack.allowAnySHA1InWant true

pack_kib() { git -C "$1" count-objects -v | awk '/^size-pack:/ {print $2}'; }

for since in "$@"; do
  base=$(git -C "$work/src.git" rev-list -1 --before="$since" "$branch")
  commits=$(git -C "$work/src.git" rev-list --count "$base..$branch")
  graph_files=$(git -C "$work/src.git" diff --name-only "$base" "$branch" -- .sdd | wc -l | tr -d ' ')
  all_files=$(git -C "$work/src.git" diff --name-only "$base" "$branch" | wc -l | tr -d ' ')
  for mode in filtered unfiltered; do
    dir="$work/clone-${since// /-}-$mode"
    git -C "$work/src.git" update-ref refs/heads/probe-base "$base"
    git clone -q --filter=blob:none --no-checkout --single-branch -b probe-base "file://$work/src.git" "$dir"
    git -C "$dir" sparse-checkout set --cone .sdd
    git -C "$dir" checkout -q probe-base
    before=$(pack_kib "$dir")
    if [ "$mode" = filtered ]; then
      git -C "$dir" fetch -q origin "$branch:refs/remotes/origin/$branch"
      git -C "$dir" -c user.name=t -c user.email=t@t merge -q --no-stat --no-edit "origin/$branch"
    else
      git -C "$dir" fetch -q --no-filter origin "$branch:refs/remotes/origin/$branch"
      GIT_NO_LAZY_FETCH=1 git -C "$dir" -c user.name=t -c user.email=t@t merge -q --no-stat --no-edit "origin/$branch"
    fi
    after=$(pack_kib "$dir")
    printf 'since %-10s commits=%-4s changed files: graph=%-4s all=%-5s %-10s +%s KiB\n' "$since" "$commits" "$graph_files" "$all_files" "$mode" "$((after - before))"
  done
done
