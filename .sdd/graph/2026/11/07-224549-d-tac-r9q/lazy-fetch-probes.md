# Lazy fetches in the sdd-owned clone

Both probes build scratch repositories and reach no network. `ssh-run.sh` serves a bare repository through a stand-in `ssh` that runs `git-upload-pack` locally and logs one line per connection, and it counts what the gate would count: `child_start` events in `GIT_TRACE2_EVENT` whose `argv[0]` names `ssh` or `git-remote-http(s)`. The clone is made as sdd makes its own: `--filter=blob:none`, cone `.sdd`, git 2.56.0. `bytes.sh` clones this repository the same way at an older commit, fetches up to `fza-fork`, merges, and reads the growth of `size-pack`.

## Connections per step

Each scenario adds commits upstream that change 5 entries, add 5 entries and change one code file, plus a local commit, unless stated otherwise.

| Step | Real connections | Gate records |
|---|---|---|
| fetch, as today | 1 | 1 |
| `merge-tree --write-tree` conflict prediction | 0 | 0 |
| merge, as today | 2 | 0 |
| merge with the gate's trace environment | 2 | 2 |
| merge, fast-forward | 2 | 2 with trace |
| merge, 50 changed and 50 new entries | 2 | 2 with trace |
| merge, only a code file changed upstream | 1 | 1 with trace |
| merge `--no-stat` | 1 | 1 with trace |
| merge `--no-stat`, only a code file changed | 0 | 0 |
| merge with `GIT_NO_LAZY_FETCH=1` | 0, exit 128, HEAD, index and tree unchanged | 0 |
| fetch `--filter=blob:limit=1g`, then merge `--no-stat` with `GIT_NO_LAZY_FETCH=1` | 2 + 0 | 2 |
| fetch `--no-filter`, then merge `--no-stat` with `GIT_NO_LAZY_FETCH=1` | 1 + 0 | 1 |
| `ls-files --unmerged`, `diff --name-only`, `rev-list --parents`, `status`, `show` of an old entry revision | 0 | 0 |

The merge's two connections are a batched fetch of the changed entries' contents and, after the merge commit, a fetch of the changed code files' contents for the diffstat; `--no-stat` removes the second. With `GIT_NO_LAZY_FETCH=1` the merge fails with `warning: lazy fetching disabled; some objects may not be available` and `fatal: could not fetch <oid> from promisor remote`.

## Bytes per fetch

Fetch and merge into a partial sparse clone of this repository, from the commit before the given point up to `fza-fork`. Filtered is today's fetch plus the merge's lazy fetches; unfiltered is `--no-filter` and an offline merge.

| Commits | Changed files (graph / all) | Filtered | Unfiltered |
|---|---|---|---|
| 317 | 1,518 / 1,844 | 2,565 KiB | 4,475 KiB |
| 715 | 1,633 / 2,447 | 3,336 KiB | 6,157 KiB |
| 773 | 1,657 / 2,481 | 3,430 KiB | 6,363 KiB |

The probes are [ssh-run.sh](./ssh-run.sh) and [bytes.sh](./bytes.sh).
