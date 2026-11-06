#!/usr/bin/env bash
# Proof matrix for routing graph entry merges through a custom driver per git call
# via --attr-source, without .gitattributes edits or per-clone driver config.
# Run from the project root: bash .tmp/seats/main/attr-source-proof.sh
set -u
root=.tmp/seats/main/attr-proof

setup() {
  # setup <name>: repo with entry.md and sub/entry.md, branches main and side
  # diverging on both files.
  mkdir -p "$root/$1/repo"
  cd "$root/$1/repo" || exit 1
  git init -q -b main
  git config user.email probe@example.invalid
  git config user.name probe
  mkdir -p sub
  printf 'line\n' > entry.md
  printf 'line\n' > sub/entry.md
  git add -A
  git commit -qm "test: base"
  git checkout -qb side
  printf 'side\n' > entry.md
  printf 'side\n' > sub/entry.md
  git commit -qam "test: side"
  git checkout -q main
  printf 'main\n' > entry.md
  printf 'main\n' > sub/entry.md
  git commit -qam "test: main"
}

leave() {
  cd - > /dev/null || exit 1
}

tree_with_root_attrs() {
  # tree_with_root_attrs <content>: tree holding only a root .gitattributes.
  local blob
  blob=$(printf '%s' "$1" | git hash-object -w --stdin)
  printf '100644 blob %s\t.gitattributes\n' "$blob" | git mktree
}

echo "== git: $(git --version)"

echo
echo '== T1 info/attributes vs --attr-source'
setup t1
printf '*.md merge=binary\n' > .git/info/attributes
tree=$(tree_with_root_attrs $'*.md merge=probe\n')
git --attr-source="$tree" check-attr merge -- entry.md
leave

echo
echo '== T2 nested worktree sub/.gitattributes, root-only crafted tree'
setup t2
printf '*.md merge=binary\n' > sub/.gitattributes
git add sub/.gitattributes
git commit -qm "test: nested attrs"
tree=$(tree_with_root_attrs $'*.md merge=probe\n')
git --attr-source="$tree" check-attr merge -- entry.md sub/entry.md
echo 'without --attr-source:'
git check-attr merge -- sub/entry.md
leave

echo
echo '== T3 crafted tree carrying nested sub/.gitattributes, sdd line only at root'
setup t3
sub_blob=$(printf '*.md merge=binary\n' | git hash-object -w --stdin)
sub_tree=$(printf '100644 blob %s\t.gitattributes\n' "$sub_blob" | git mktree)
root_blob=$(printf '*.md merge=probe\n' | git hash-object -w --stdin)
tree=$(printf '100644 blob %s\t.gitattributes\n040000 tree %s\tsub\n' "$root_blob" "$sub_tree" | git mktree)
git --attr-source="$tree" check-attr merge -- entry.md sub/entry.md
echo 'same, sdd line appended to the nested file too:'
sub_blob=$(printf '*.md merge=binary\n*.md merge=probe\n' | git hash-object -w --stdin)
sub_tree=$(printf '100644 blob %s\t.gitattributes\n' "$sub_blob" | git mktree)
tree=$(printf '100644 blob %s\t.gitattributes\n040000 tree %s\tsub\n' "$root_blob" "$sub_tree" | git mktree)
git --attr-source="$tree" check-attr merge -- entry.md sub/entry.md
leave

echo
echo '== T4 committed root .gitattributes clash, merge via --attr-source'
setup t4
printf '*.md merge=binary\n' > .gitattributes
git add .gitattributes
git commit -qm "test: attrs"
tree=$(tree_with_root_attrs $'*.md merge=binary\n*.md merge=probe\n')
if git --attr-source="$tree" -c 'merge.probe.driver=echo called %P >> ../driver.log; cp %B %A' merge -q --no-edit side; then echo merged; else echo conflict; fi
echo "driver calls: $(wc -l < ../driver.log | tr -d ' ')"
echo "entry.md: $(cat entry.md), sub/entry.md: $(cat sub/entry.md)"
leave

echo
echo '== T5 git pull --no-rebase from a peer folder path with --attr-source'
setup t5
leave
mkdir -p "$root/t5/peer"
git clone -q "$root/t5/repo" "$root/t5/peer/clone"
cd "$root/t5/peer/clone" || exit 1
git config user.email probe@example.invalid
git config user.name probe
printf 'peer\n' > entry.md
git commit -qam "test: peer"
tree=$(tree_with_root_attrs $'*.md merge=probe\n')
if git --attr-source="$tree" -c 'merge.probe.driver=echo called %P >> ../driver.log; cp %B %A' pull -q --no-rebase --no-edit ../../repo side; then echo merged; else echo conflict; fi
echo "driver calls: $(wc -l < ../driver.log 2>/dev/null | tr -d ' ')"
echo "entry.md: $(cat entry.md)"
leave

echo
echo '== T6 attribute names a driver no config defines (plain user git merge)'
setup t6
printf '*.md merge=sdd-entry\n' > .git/info/attributes
if git merge -q --no-edit side; then echo merged; else echo conflict; fi
echo "entry.md first line: $(head -1 entry.md)"
leave

echo
echo '== T7 unreachable crafted tree after gc --prune=now'
setup t7
tree=$(tree_with_root_attrs $'*.md merge=probe\n')
if git cat-file -e "$tree"; then echo 'before gc: present'; fi
git gc -q --prune=now
if git cat-file -e "$tree" 2>/dev/null; then echo 'after gc: present'; else echo 'after gc: gone'; fi
echo 'check-attr with the pruned tree:'
git --attr-source="$tree" check-attr merge -- entry.md
echo "exit: $?"
leave

echo
echo '== T8 cost, 50 runs each'
setup t8
TIMEFORMAT='%R s'
echo 'build crafted tree (hash-object -w + mktree):'
time (for run in $(seq 50); do tree_with_root_attrs $'*.md merge=probe\n' > /dev/null; done)
tree=$(tree_with_root_attrs $'*.md merge=probe\n')
echo 'check-attr, worktree attributes:'
time (for run in $(seq 50); do git check-attr merge -- entry.md > /dev/null; done)
echo 'check-attr, --attr-source:'
time (for run in $(seq 50); do git --attr-source="$tree" check-attr merge -- entry.md > /dev/null; done)
echo 'cat-file -e existence check:'
time (for run in $(seq 50); do git cat-file -e "$tree"; done)
leave
