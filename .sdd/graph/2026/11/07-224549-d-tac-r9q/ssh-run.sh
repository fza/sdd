#!/bin/bash
# Lazy blob fetches in a blobless sparse clone reached over ssh, counted the
# way the git gate counts them (child_start whose argv[0] first word is ssh).
# Usage: ssh-run.sh <empty work dir> <graph dir to seed>
set -euo pipefail
work="$1"
graph_src="$2"
mkdir -p "$work/bin"
cd "$work"

cat > bin/ssh <<'EOF'
#!/bin/bash
# Fake ssh: run the remote command locally, log one line per connection.
echo "$(date +%s) ${@: -1}" >> "$(dirname "$0")/../connections.log"
exec sh -c "${@: -1}"
EOF
chmod +x bin/ssh
ssh_cmd="$work/bin/ssh -o BatchMode=yes"
url="ssh://fake$work/remote.git"

git init -q --bare -b main remote.git
git -C remote.git config uploadpack.allowFilter true
git -C remote.git config uploadpack.allowAnySHA1InWant true
git clone -q "file://$work/remote.git" seed
mkdir -p seed/.sdd seed/src
cp -R "$graph_src" seed/.sdd/graph
for i in $(seq 1 200); do echo "code $i" > "seed/src/file$i.txt"; done
git -C seed add -A
git -C seed -c user.name=t -c user.email=t@t commit -q -m seed
git -C remote.git fetch -q "$work/seed" HEAD:refs/heads/main

GIT_SSH_COMMAND="$ssh_cmd" git clone -q --filter=blob:none --no-checkout "$url" clone
git -C clone config core.sshCommand "$ssh_cmd"
git -C clone sparse-checkout set --cone .sdd
git -C clone checkout -q main
entries=( $(cd clone && ls .sdd/graph/2026/10/*.md) )

# Same rule as gitgate readTrace: child_start, first word of argv[0], basename.
gate_count() {
  local trace="$1"
  [ -f "$trace" ] || { echo "ssh=0 https=0 promisor_fetch=0 (no trace)"; return; }
  local ssh https promisor
  ssh=$(grep '"event":"child_start"' "$trace" | sed -E 's/.*"argv":\["([^"]*)".*/\1/' | awk '{print $1}' | xargs -n1 basename 2>/dev/null | grep -cx ssh || true)
  https=$(grep '"event":"child_start"' "$trace" | sed -E 's/.*"argv":\["([^"]*)".*/\1/' | awk '{print $1}' | xargs -n1 basename 2>/dev/null | grep -cxE 'git-remote-https?' || true)
  promisor=$(grep '"event":"child_start"' "$trace" | grep -c '"fetch".*"--filter=blob:none"\|"fetch".*"--no-tags".*"--stdin"' || true)
  echo "ssh=$ssh https=$https promisor_fetch=$promisor"
}
conns() { wc -l < connections.log | tr -d ' '; }
step_no=0
step() {
  local label="$1"; shift
  step_no=$((step_no + 1))
  local before after rc=0 trace="$work/trace-$step_no.json"
  before=$(conns)
  GIT_TRACE2_EVENT="$trace" "$@" > "$work/out-${label// /-}.txt" 2>&1 || rc=$?
  after=$(conns)
  printf '%-44s exit=%-3s real_connections=%-2s gate_sees: %s\n' "$label" "$rc" "$((after - before))" "$(gate_count "$trace")"
  if [ -f "$trace" ]; then
    grep '"event":"child_start"' "$trace" | grep -E '"argv":\["(git","fetch|[^"]*ssh)' | sed -E 's/.*"argv":(\[[^]]*\]).*/     child: \1/' | cut -c1-220 || true
  fi
}
untraced() {
  local label="$1"; shift
  local before after rc=0
  before=$(conns)
  "$@" > "$work/out-${label// /-}.txt" 2>&1 || rc=$?
  after=$(conns)
  printf '%-44s exit=%-3s real_connections=%-2s gate_sees: nothing (no trace env)\n' "$label" "$rc" "$((after - before))"
}
writer() {
  local n="$1" count="$2"
  git clone -q "file://$work/remote.git" "w$n"
  mkdir -p "w$n/.sdd/graph/2026/11"
  for j in $(seq 1 "$count"); do
    echo "edit $n $j" >> "w$n/${entries[$j]}"
    echo "new $n $j" > "w$n/.sdd/graph/2026/11/new-$n-$j.md"
  done
  echo "code edit $n" >> "w$n/src/file1.txt"
  git -C "w$n" add -A
  git -C "w$n" -c user.name=t -c user.email=t@t commit -q -m "upstream $n"
  git -C remote.git fetch -q "$work/w$n" HEAD:refs/heads/main
}
local_commit() {
  echo "local $1" > "clone/.sdd/graph/2026/11/local-$1.md"
  git -C clone add -A
  git -C clone -c user.name=t -c user.email=t@t commit -q -m "local $1"
}
mergeargs=(-c user.name=t -c user.email=t@t merge --no-edit --no-overwrite-ignore -m m origin/main)
predictargs=(-c core.quotePath=false merge-tree --write-tree --name-only --no-messages HEAD origin/main)

mkdir -p clone/.sdd/graph/2026/11
: > connections.log
echo "== 1. as sdd runs today: gated fetch, ungated predict and merge (diverged, 5 edits + 5 new)"
writer 1 5; local_commit 1
step "fetch (gated)" git -C clone fetch -q origin
untraced "merge-tree predict" git -C clone "${predictargs[@]}"
untraced "merge" git -C clone "${mergeargs[@]}"

echo "== 2. proposed: predict and merge with the gate's trace env"
writer 2 5; local_commit 2
step "fetch (gated)" git -C clone fetch -q origin
step "merge-tree predict" git -C clone "${predictargs[@]}"
step "merge" git -C clone "${mergeargs[@]}"

echo "== 3. merge alone, no predict before it (diverged)"
writer 3 5; local_commit 3
step "fetch (gated)" git -C clone fetch -q origin
step "merge" git -C clone "${mergeargs[@]}"

echo "== 4. fast-forward merge, no local commit"
writer 4 5
step "fetch (gated)" git -C clone fetch -q origin
step "merge (fast-forward)" git -C clone "${mergeargs[@]}"

echo "== 5. 50 edits + 50 new: still one batch?"
writer 5 50; local_commit 5
step "fetch (gated)" git -C clone fetch -q origin
step "merge" git -C clone "${mergeargs[@]}"

echo "== 6. code-only upstream change (outside the cone)"
git clone -q "file://$work/remote.git" w6
echo "code only" >> w6/src/file2.txt
git -C w6 add -A; git -C w6 -c user.name=t -c user.email=t@t commit -q -m "code only"; git -C remote.git fetch -q "$work/w6" HEAD:refs/heads/main
local_commit 6
step "fetch (gated)" git -C clone fetch -q origin
step "merge" git -C clone "${mergeargs[@]}"

echo "== 7. GIT_NO_LAZY_FETCH=1: predict and merge refused?"
writer 7 5; local_commit 7
step "fetch (gated)" git -C clone fetch -q origin
head_before=$(git -C clone rev-parse HEAD)
step "merge-tree predict, no lazy fetch" env GIT_NO_LAZY_FETCH=1 git -C clone "${predictargs[@]}"
step "merge, no lazy fetch" env GIT_NO_LAZY_FETCH=1 git -C clone "${mergeargs[@]}"
echo "   HEAD moved: $([ "$(git -C clone rev-parse HEAD)" = "$head_before" ] && echo no || echo yes); MERGE_HEAD: $([ -f clone/.git/MERGE_HEAD ] && echo present || echo absent); status lines: $(git -C clone status --porcelain | wc -l | tr -d ' ')"
echo "   predict output: $(head -c 300 "$work/out-merge-tree-predict,-no-lazy-fetch.txt" | tr '\n' ' ')"
echo "   merge output:   $(head -c 300 "$work/out-merge,-no-lazy-fetch.txt" | tr '\n' ' ')"
step "merge after, lazy fetch allowed" git -C clone "${mergeargs[@]}"

echo "== 8. local reads sdd runs after a merge"
step "ls-files --unmerged" git -C clone ls-files --unmerged
step "diff --name-only --diff-filter=D" git -C clone diff --name-only --diff-filter=D HEAD~1...HEAD
step "rev-list --parents" git -C clone rev-list --parents -n 1 HEAD
step "status --porcelain" git -C clone status --porcelain
step "show old entry revision" git -C clone show "HEAD~3:${entries[1]}"

echo "== 9. merge --no-stat (diverged, entries + one code file)"
writer 9 5; local_commit 9
step "fetch (gated)" git -C clone fetch -q origin
step "merge --no-stat" git -C clone -c user.name=t -c user.email=t@t merge --no-stat --no-edit --no-overwrite-ignore -m m origin/main

echo "== 10. merge --no-stat, code-only upstream change"
git clone -q "file://$work/remote.git" w10
echo "code only 10" >> w10/src/file3.txt
git -C w10 add -A; git -C w10 -c user.name=t -c user.email=t@t commit -q -m "code only 10"
git -C remote.git fetch -q "$work/w10" HEAD:refs/heads/main
local_commit 10
step "fetch (gated)" git -C clone fetch -q origin
step "merge --no-stat" git -C clone -c user.name=t -c user.email=t@t merge --no-stat --no-edit --no-overwrite-ignore -m m origin/main

echo "== 11. blobs outside the cone fetched after scenarios 1-10"
echo "   src blobs missing: $(git -C clone rev-list --objects --missing=print HEAD -- src | grep -c '^?' || true) of $(git -C clone ls-tree -r HEAD -- src | wc -l | tr -d ' ') in tree"

echo "== 12. fetch new commits with all their contents, then merge offline"
writer 12 5; local_commit 12
step "fetch --filter=blob:limit=1g" git -C clone fetch -q --filter=blob:limit=1g origin
step "merge --no-stat, no lazy fetch" env GIT_NO_LAZY_FETCH=1 git -C clone -c user.name=t -c user.email=t@t merge --no-stat --no-edit --no-overwrite-ignore -m m origin/main

echo "== 13. same with --no-filter"
writer 13 5; local_commit 13
step "fetch --no-filter" git -C clone fetch -q --no-filter origin
step "merge --no-stat, no lazy fetch" env GIT_NO_LAZY_FETCH=1 git -C clone -c user.name=t -c user.email=t@t merge --no-stat --no-edit --no-overwrite-ignore -m m origin/main
echo "   src/file1.txt at upstream present: $(GIT_NO_LAZY_FETCH=1 git -C clone cat-file -e "origin/main:src/file1.txt" 2>/dev/null && echo yes || echo no)"
