# Full-content fetch in the sdd-owned clone

The probes build scratch repositories and reach no network, with git 2.56.0. `unpack-run.sh` and `backfill-run.sh` serve a bare repository through a stand-in `ssh` that logs one line per connection, as in the probes of 20261107-224549-d-tac-r9q. `conns-real.sh` clones this repository blobless with cone `.sdd` at an older commit, fetches 179 commits of `fza-fork` over the file transport, and counts `git-upload-pack` starts and promisor fetches (`fetch ... --stdin`) in `GIT_TRACE2_EVENT`.

## This repository, 179 commits

| Fetch | Fetch connections | Of these, delta-base fetches | Merge `--no-stat` connections |
|---|---|---|---|
| `--no-filter` | 2 | 1 | 0 |
| `-c fetch.unpackLimit=1 --no-filter` | 2 | 1 | 0 |
| blobless, as today | 1 | 0 | 1 |

## Stand-in remote

Each step adds upstream commits that change 5 entries, add 5 entries and change one code file, plus a local commit.

| Case | Step | Connections |
|---|---|---|
| clean clone | `--no-filter` fetch | 1 |
| | merge with `GIT_NO_LAZY_FETCH=1` | 0 |
| upstream fetched blobless, unmerged, more upstream | `--no-filter` fetch | 6, five of them one per missing delta base |
| | the same with `-c fetch.unpackLimit=1` | 2 |
| | merge with `GIT_NO_LAZY_FETCH=1` | 0, exit 128 |
| | `git backfill --sparse HEAD..origin/main` | 1 |
| | merge with `GIT_NO_LAZY_FETCH=1` | 0, exit 0 |
| next ordinary round | `--no-filter` fetch, merge | 1, 0 |
| blobless fetch | `git backfill --sparse HEAD..origin/main` | 1 |
| `--no-filter` fetch | `git backfill --sparse HEAD..origin/main` | 0 |
| code-only upstream change, blobless fetch | `git backfill --sparse HEAD..origin/main` | 0 |
| nothing new upstream | `git backfill --sparse HEAD..origin/main` | 0 |

A fetch below `fetch.unpackLimit` objects goes through `unpack-objects`, which fetches each missing delta base on its own; `index-pack --fix-thin` fetches them in one batch.

The probes are [unpack-run.sh](./unpack-run.sh), [backfill-run.sh](./backfill-run.sh) and [conns-real.sh](./conns-real.sh).
