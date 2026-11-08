# Spec: config watch in sdd serve, and `sdd move`

All decisions in this spec are settled.

## A. Config watch in `sdd serve`

1. `sdd serve` follows every config layer without a reconnect: user-global config (`~/.config/sdd/config.yaml`), the project's `.sdd/config.yaml` and `.sdd/config.local.yaml`, and in split layout the graph project's config files. Files created after serve started count too, and so do files that vanish.
2. Mechanism: before every MCP call and before each tick of the background sync timer, serve stats each config file (size, modification time, existence). If any changed, appeared or vanished, serve rebuilds its runtime before answering. Standard library only, no file-watching dependency, no polling goroutine of its own.
3. Engine sessions live in `.sdd/sessions` (`pkg/local/session_locations.go:45`); a rebuild keeps them.
4. A config file left broken (parse error) makes every call fail with the file name and the parse error until fixed. Serve never keeps the old config silently.
5. Running engine sessions follow the new config on the next call, mid-move too (live on every step). This supersedes the "bound at session open" part of graph decision 20261029-150854-d-tac-b2f (dialogue mode). Settled detail:
   1. The next serve after a change carries a one-time notice per change: the changed keys with old and new values; secret values (API keys, tokens) never appear, only the key name.
   2. Every answer event of the session event log records the dialogue mode in force at that moment.
   3. A gate a procedure instance already passed does not re-run; later gates evaluate under the new config.
6. Graph location changed (`graph_dir`, `project_dir`, `graph_repo`): if the new graph folder is missing or empty, or the move is partial (entries left in the folder serve used before, or the new folder holds fewer of them), every MCP tool call fails, naming the state, until settled.
7. Today serve resolves the graph dir and config once at start (`internal/cliapp/serve.go`, `resolveGraphDir`, `loadConfig`, `buildLocalApplication`); the dialogue mode is resolved when an engine session opens (`pkg/application/workflow.go:280`, `pkg/application/application.go:141`).

## B. `sdd move` (CLI only, no MCP verb)

1. Forms:
   - `sdd move --graph-dir <dir>`: move the graph to another folder inside this repository, or back into this repository from a separate one.
   - `sdd move --project-dir <path>`: move the graph out to a checkout on disk (created and `git init`-ed if it does not exist; a non-empty repository without a graph is accepted).
   - `sdd move --graph-repo <repo id>`: move the graph out to a repository sdd clones and pushes itself (`internal/repos/graph_clone.go`).
   - The target must hold no graph yet; otherwise refuse.
   - Same flag meanings as `sdd init` (`internal/cliapp/app.go`, flags `graph-dir`, `project-dir`, `graph-repo`).
2. History: inside one repository a plain `git mv` (history follows renames). Into or out of a separate repository: carry the history with `git subtree split --prefix=<graph dir>` and push the result into the target (the same in the other direction for a move back). Original commits stay untouched; nothing is rewritten. Where `git subtree` is missing, refuse and name the cause. Measured: 2,067 graph commits split in 75 s on this repository.
3. End state equals a fresh `sdd init` of the target layout plus the moved content:
   - committed keys beyond the pointer keys (`internal/model/config.go`, `PointerKeys`, `GraphRepoPointerKeys`) move into the graph project's `.sdd/config.yaml`; the code repository's config becomes the pointer config exactly as init writes it (and the reverse on a move back);
   - `meta.json` moves with the graph;
   - a `--project-dir` target checkout gets init's install step (skills, MCP registration) with the code repository's skill scope and agents, committed with the move; an sdd-owned `--graph-repo` clone gets none, nobody works inside it. Rejected: printing `sdd init` as a last step, which leaves the checkout without sdd tools until someone runs it;
   - a checkout the graph leaves (a former `project_dir`) ends with no sdd files: config, `meta.json`, `.sdd/.gitignore`, the skills and the MCP registration init installed there are removed in the move commit. A skill whose content no longer matches its `sdd-content-hash` stamp refuses the move before anything changes, naming the file. Rejected: turning it into a pointer to the graph's new home (ties a repository to a project it may not belong to) and keeping skills and MCP (sdd tools that fail on every call there). Accepted cost: a checkout that also holds other work loses its sdd tools;
   - root `.gitignore` and `.gitattributes` need no rewrite (section D);
   - machine-local data (`.sdd/sessions`, `.sdd/staged-blobs`, `.sdd/stats`, machine-local keys of `.sdd/config.local.yaml`) moves to where the new layout reads it;
   - search index: the move creates `<cache>/index/<new repo key>` as a relative symlink to the store's real directory (resolved through any existing link, so links never chain); the store is neither copied nor renamed, so agents still resolving the old key keep reading and writing the same store, and its locks (files inside the store) stay shared. A key that already exists as a real directory is left alone. Every store open through a link touches a use marker `<cache>/index/.links/<hash of key>` (holding the key name); any store open removes links, with their markers, unused for 30 days. A real directory is never removed by the sweep. `sdd index gc` and store listings resolve links and count each real store once. Rejected: lazy rebuild (a full re-embed after every move), copying (a second path into the index that every format change must follow, and the old-location store drifts), keying by the oldest entry ID. Accepted cost: a checkout idle for over 30 days that still resolves a swept key re-embeds lazily;
   - default graph folder in a repository holding only the graph is `graph/` (`model.GraphOnlyGraphDir`, `model.DefaultGraphDirFor`).
4. Refuse while the graph folder holds uncommitted changes. sdd makes its own commits in both repositories (sdd's own commit format, `sdd: ...`).
5. Before publishing (push), fetch; if the source graph moved on since the history split, publish nothing. An sdd-owned clone (`graph_repo`) is pulled and split again, at most 3 attempts. A person's checkout (`project_dir` or the code repository) is never pulled: the move refuses with nothing changed and names `sdd sync --pull`. Rejected: pulling a checkout too, since a merge into the person's working branch can conflict and leave it half-merged mid-move. Accepted cost: one manual pull and rerun in a checkout.
6. The move raises `minimum_version` in `meta.json` (`internal/model/schema.go:79`), so builds without move records refuse the moved graph. A released build writes its own version, or keeps a higher recorded one: the mover knows move records, so the floor is never too low. A dev build writes `MoveRecordsMinimumVersion`. Rejected: a guessed upstream release (`v0.18.0-alpha.4`) as floor for released builds, wrong if that release ships without move records. Accepted cost: coworkers upgrade to at least the mover's build, even where an older build already knows move records.

## C. Move record and stray detection (no tombstone file)

1. Location reference: `<kind>:<address>`, options as query parameters, parsed by the adapter that owns the kind. Kinds now: `git-dir:<dir>` (folder in this repository), `git-path:<path>?dir=<graph dir>` (checkout on disk), `git-repo:<repo id>?dir=<graph dir>&branch=<branch>` (sdd-cloned repository). Later kinds (for example `neo4j:`, `hosted:`) must fit without a format change. An unknown kind is refused, naming the kind.
2. Versioned record with fixed fields:

```
sdd-move: v1
sdd-move-id: <ULID>
sdd-move-from: <location reference>
sdd-move-to: <location reference>
sdd-move-revision: <source revision the move was cut at>
sdd-move-entries: <number of entries moved>
```

   Readers ignore unknown fields and refuse an unknown version.
3. Git stores write the record as trailers on the move commit (in both repositories where both change). Other stores would keep the same fields in their own medium; each store adapter gets a read method for its move records. The stray check is pure and lives in `internal/model`.
4. Stray: an entry-shaped file (`YYYY/MM/DD-HHmmss-<type>-<layer>-<suffix>`) in a former location (learned from move records) whose id the current graph does not hold. sdd checks on start, after each pull or sync, and on each config change. While strays exist, CLI and MCP refuse, naming the files and the `sdd move` call that carries them over with their history (re-running the same `sdd move` carries stragglers).
5. A former `project_dir` whose checkout does not exist on this machine is skipped (no local work can strand there).
6. Accepted cost: a forge that squashes or rewrites the move commit loses its trailers.

## D. Ignore and attribute files that never depend on `graph_dir`

Owner decisions, settled.

1. `.sdd-runtime/` (graph lock, titles lock, lineage, publication temp file) stays inside the graph folder. Wherever sdd creates it (`pkg/local/local_graphstore.go`, `internal/syncstate/syncstate.go`), sdd also writes `.sdd-runtime/.gitignore` holding `*`, so git ignores the folder in every layout and after every move. Rejected: moving it to `.sdd/runtime/` (the publish rename would cross folders, losing the write confinement and failing across filesystems).
2. Init writes a committed `.sdd/.gitignore` listing `tmp/`, `config.local.yaml`, `index/`, `stats/`, `sessions/`, `staged-blobs/`, in every repository that has a `.sdd/` (project, work repository, graph repository, sdd clone). The repository root `.gitignore` gets no sdd lines.
3. Init writes a committed `<graph dir>/.gitattributes` holding `EntryPathGlob merge=<EntryMergeDriver>` (fixed pattern, no graph_dir prefix), replacing the root `.gitattributes` line (`internal/handlers/handler_init.go:55`, `internal/repos/graph_clone.go:165`). It travels with the graph through `git mv` and the history split.
4. Init removes from the root `.gitignore` and `.gitattributes` only the exact lines earlier builds derived (`initGitignoreEntries`, `EntryMergeAttributeLine` for the recorded graph_dir); lines added by hand stay.
5. `sdd move` drops its root-file rewrite (`handler_move_graph.go` around the ignore and attribute planning); the files above need no rewrite. The move still writes `.sdd/.gitignore` where it creates a new `.sdd/`.
6. This replaces the `.gitignore` part of graph decision 20261029-132740-d-tac-g32 (both repositories get the same root lines).

## E. Tests owed

1. Machine-wide session and staging stores follow a move: CLI test with `t.Setenv("XDG_STATE_HOME", ...)`, an open session before `sdd move --project-dir`, the session found under the new repository key after it (`moveSessionStores`, `machineSessionStores`).
2. A failed history replay aborts: the target repository holds a non-entry file at a path the replay creates; the move fails naming the conflict, the target has no `.git/rebase-apply` and a clean tree (`internal/git/graph_move.go`, `am --abort`).
3. Each new guard broken once and seen red on its own assertion, then restored.
4. The `git subtree` refusal stays covered by `TestRequireSubtree` (fake git on PATH).
