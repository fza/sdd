#!/usr/bin/env bash
# Proof: an sdd line appended to $GIT_DIR/info/attributes outranks committed root and
# nested .gitattributes and an earlier clashing line in the same file; the driver
# itself is passed per call with -c.
# Run from the project root: bash .tmp/seats/main/info-attributes-proof.sh
set -u
mkdir -p .tmp/seats/main/info-proof/repo
cd .tmp/seats/main/info-proof/repo || exit 1
git init -q -b main
git config user.email probe@example.invalid
git config user.name probe
mkdir -p sub
printf '*.md merge=binary\n' > .gitattributes
printf '*.md merge=binary\n' > sub/.gitattributes
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
printf '*.md merge=binary\n*.md merge=probe\n' > .git/info/attributes

echo '== check-attr with info/attributes (clash earlier in same file, root and nested committed binary):'
git check-attr merge -- entry.md sub/entry.md

echo '== merge with driver passed per call:'
if git -c 'merge.probe.driver=echo called %P >> ../driver.log; cp %B %A' merge -q --no-edit side; then echo merged; else echo conflict; fi
echo "driver calls: $(wc -l < ../driver.log | tr -d ' ')"
echo "entry.md: $(cat entry.md), sub/entry.md: $(cat sub/entry.md)"
echo "repo config holds driver: $(git config --get merge.probe.driver || echo no)"
