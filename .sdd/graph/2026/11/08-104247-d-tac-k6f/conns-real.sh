#!/bin/bash
# Lazy fetches a --no-filter fetch starts in a blobless .sdd-cone clone of
# this repository: thin-pack delta bases outside the cone are missing.
# Usage: conns-real.sh <empty work dir> <source repo> <branch> <since>
set -euo pipefail
work="$1"; src="$2"; branch="$3"; since="$4"
mkdir -p "$work"
git clone -q --bare --no-local "$src" "$work/src.git"
git -C "$work/src.git" config uploadpack.allowFilter true
git -C "$work/src.git" config uploadpack.allowAnySHA1InWant true
base=$(git -C "$work/src.git" rev-list -1 --before="$since" "$branch")
git -C "$work/src.git" update-ref refs/heads/probe-base "$base"
echo "commits: $(git -C "$work/src.git" rev-list --count "$base..$branch")"

lazy_count() { grep '"event":"child_start"' "$1" | grep -c '"fetch".*"--stdin"' || true; }
uploads() { grep '"event":"child_start"' "$1" | grep -c 'git-upload-pack\|"upload-pack"' || true; }

for mode in plain unpacklimit1 filtered; do
  dir="$work/clone-$mode"
  git clone -q --filter=blob:none --no-checkout --single-branch -b probe-base "file://$work/src.git" "$dir"
  git -C "$dir" sparse-checkout set --cone .sdd
  git -C "$dir" checkout -q probe-base
  trace="$work/trace-$mode.json"
  case "$mode" in
    plain) GIT_TRACE2_EVENT="$trace" git -C "$dir" fetch -q --no-filter origin "$branch:refs/remotes/origin/$branch" ;;
    unpacklimit1) GIT_TRACE2_EVENT="$trace" git -C "$dir" -c fetch.unpackLimit=1 fetch -q --no-filter origin "$branch:refs/remotes/origin/$branch" ;;
    filtered) GIT_TRACE2_EVENT="$trace" git -C "$dir" fetch -q origin "$branch:refs/remotes/origin/$branch" ;;
  esac
  mtrace="$work/trace-$mode-merge.json"
  rc=0
  GIT_TRACE2_EVENT="$mtrace" git -C "$dir" -c user.name=t -c user.email=t@t merge -q --no-stat --no-edit "origin/$branch" || rc=$?
  printf '%-13s fetch: upload-pack=%s lazy=%s   merge: exit=%s lazy=%s\n' "$mode" "$(uploads "$trace")" "$(lazy_count "$trace")" "$rc" "$(lazy_count "$mtrace")"
done
