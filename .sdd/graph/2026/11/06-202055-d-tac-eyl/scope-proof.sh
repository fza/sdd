#!/usr/bin/env bash
# Proof: the entry glob scoped to the graph dir sends only entry files to the driver;
# other markdown merges with git's built-in text merge. A driver exiting non-zero
# yields a conflict, git does not fall back to its own merge.
# Run from the project root: bash .tmp/seats/main/scope-proof.sh
set -u
mkdir -p .tmp/seats/main/scope-proof/repo
cd .tmp/seats/main/scope-proof/repo || exit 1
git init -q -b main
git config user.email probe@example.invalid
git config user.name probe
mkdir -p .sdd/graph/2026/11 docs
printf 'a\nb\nc\nd\ne\n' > .sdd/graph/2026/11/06-120000-s-tac-abc.md
printf 'a\nb\nc\nd\ne\n' > .sdd/graph/notes.md
printf 'a\nb\nc\nd\ne\n' > docs/readme.md
git add -A
git commit -qm "test: base"
git checkout -qb side
sed -i '' 's/^e$/e-side/' .sdd/graph/2026/11/06-120000-s-tac-abc.md .sdd/graph/notes.md docs/readme.md
git commit -qam "test: side"
git checkout -q main
sed -i '' 's/^a$/a-main/' .sdd/graph/2026/11/06-120000-s-tac-abc.md .sdd/graph/notes.md docs/readme.md
git commit -qam "test: main"
printf '.sdd/graph/[0-9][0-9][0-9][0-9]/[0-9][0-9]/[0-9][0-9]-*.md merge=probe\n' > .git/info/attributes

echo '== check-attr:'
git check-attr merge -- .sdd/graph/2026/11/06-120000-s-tac-abc.md .sdd/graph/notes.md docs/readme.md

echo '== merge, driver logs its path then merges like git (merge-file):'
if git -c 'merge.probe.driver=echo called %P >> ../driver.log; git merge-file %A %O %B' merge -q --no-edit side; then echo merged; else echo conflict; fi
echo 'driver calls:'
cat ../driver.log
echo "notes.md first/last: $(head -1 .sdd/graph/notes.md) / $(tail -1 .sdd/graph/notes.md)"
echo "readme.md first/last: $(head -1 docs/readme.md) / $(tail -1 docs/readme.md)"

echo '== driver that refuses (exit 1), on a merge git would resolve cleanly:'
git reset -q --hard HEAD~1
if git -c 'merge.probe.driver=exit 1' merge -q --no-edit side; then echo merged; else echo conflict; fi
git status --short
git merge --abort
