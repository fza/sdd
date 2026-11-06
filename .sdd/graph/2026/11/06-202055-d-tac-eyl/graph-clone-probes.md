# Probes behind the sdd-owned graph clone mechanics

All probes ran with git 2.56.0 on macOS in throwaway repositories. The scripts are attached beside this record and run from the project root.

## Partial sparse clone (`sparse-clone-proof.sh`, `treeless-proof.sh`)

Origin: a synthetic repository holding a copy of this project's graph (1,906 files under `.sdd/graph`) plus `internal/` as code and 300 further commits, each rewriting a 20 KB code file and one graph file. 301 commits in total.

| Clone | Pack size (KiB) | Commits known |
|---|---|---|
| full | 12,644 | 301 |
| `--filter=blob:none --sparse`, cone `.sdd` | 5,249 | 301 |
| `--depth=1 --filter=blob:none --sparse` | 3,728 | 1 |
| `--filter=tree:0 --sparse`, after clone | 225 | 303 |
| `--filter=tree:0 --sparse`, after checkout | 3,952 | 303 |

- Blobless sparse clone: the worktree held only `.git` and `.sdd` with all graph files. `git sparse-checkout add docs` added a second directory in place.
- Write path through the blobless sparse clone: a committed entry reached the origin; a commit made elsewhere touching code and graph merge-pulled clean, the entry arrived, the code file never appeared, `git status` showed nothing.
- Treeless clone: `git log --oneline -- <one graph file>` over 300 commits took 77.5 s with the origin on local disk, and the pack grew to 37,229 KiB, because each commit fetched its tree separately.

## Merge driver routing (`attr-source-proof.sh`, `info-attributes-proof.sh`, `scope-proof.sh`)

| Case | Result |
|---|---|
| `-c core.attributesFile=<file>` beside a committed `*.md merge=binary` | `merge: binary`, driver not called, conflict |
| `--attr-source=<crafted tree>` beside a committed `.gitattributes` | driver called, clean merge |
| `--attr-source` with `.git/info/attributes` setting `merge=binary` | `merge: binary`; info/attributes outranks the tree |
| `--attr-source` with a nested `sub/.gitattributes` not in the tree | worktree attribute files ignored entirely |
| `--attr-source` naming a tree removed by `git gc --prune=now` | `merge: unspecified`, exit 0, no error |
| `git pull --no-rebase <peer path>` with `--attr-source` and `-c` driver | driver called |
| sdd line appended to `.git/info/attributes` after a clashing line, committed root and nested `.gitattributes` setting `binary` | `merge: probe` for both files, driver called, clean merge, no driver in `.git/config` |
| attribute naming a driver no config defines, plain `git merge` | git's text merge, conflict markers |
| pattern `.sdd/graph/[0-9][0-9][0-9][0-9]/[0-9][0-9]/[0-9][0-9]-*.md` | entry routed; `.sdd/graph/notes.md` and `docs/readme.md` unspecified, merged by git |
| driver exiting 1 on a change git merges cleanly | conflict (`UU`), no fallback to git's merge |

Cost per call, 50 runs each: building a crafted tree (`hash-object -w` and `mktree`) 15 ms, `check-attr` 6.5 ms with or without `--attr-source`, `cat-file -e` 7 ms.

## Rename upstream (`rename-proof.sh`)

An entry renamed on one branch while its `title` line changed on the other merged clean with the default merge; the changed title landed at the new path.

## Stock sdd against the guard file (`stock-guard-proof.sh`, `stock-guard-capture.sh`)

Stock sdd built from `main`, run in a code repository whose committed config holds `graph_repo` and `graph_dir: .sdd/GRAPH-ELSEWHERE.md`, with that file committed:

| Command | Result |
|---|---|
| every command | `ignoring unknown config key key=graph_repo` |
| `sdd view --layout signals` | exit 0, empty graph |
| `sdd search --term x` | exit 0, only the embedded base facts |
| `sdd new … s tac …` | `creating directories: mkdir …/.sdd/GRAPH-ELSEWHERE.md: not a directory`, exit 1, nothing written or committed |
