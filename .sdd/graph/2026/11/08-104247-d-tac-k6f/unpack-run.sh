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
offline_merge() { env GIT_NO_LAZY_FETCH=1 git -C clone -c user.name=t -c user.email=t@t merge --no-stat --no-edit --no-overwrite-ignore -m m origin/main; }
# Usage tail: $3 = extra git -c option for the no-filter fetch, or "none"
extra="${3:-none}"
fetchopt=()
[ "$extra" != none ] && fetchopt=(-c "$extra")

echo "== A. clean clone, no-filter fetch (option: $extra)"
writer 1 5; local_commit 1
step "A fetch no-filter" git ${fetchopt[@]+"${fetchopt[@]}"} -C clone fetch -q --no-filter origin
step "A merge offline" offline_merge

echo "== B. upstream fetched blobless and left unmerged, then more upstream, no-filter fetch"
writer 2 5; local_commit 2
step "B fetch blobless" git -C clone fetch -q origin
writer 3 5
step "B fetch no-filter" git ${fetchopt[@]+"${fetchopt[@]}"} -C clone fetch -q --no-filter origin
step "B merge offline" offline_merge
git -C clone merge --abort 2>/dev/null || true
step "B backfill" git -C clone backfill --sparse HEAD..origin/main
step "B merge offline after backfill" offline_merge

echo "== C. after B, ordinary round"
writer 4 5; local_commit 4
step "C fetch no-filter" git ${fetchopt[@]+"${fetchopt[@]}"} -C clone fetch -q --no-filter origin
step "C merge offline" offline_merge
