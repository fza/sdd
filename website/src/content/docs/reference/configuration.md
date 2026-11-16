---
title: Configuration
description: Every sdd configuration key, where it lives, and how to set up the language model and embedding providers.
---

sdd reads its settings from three YAML files and merges them. `sdd config` shows the merged result and the file each value comes from.

## Configuration files

| Layer | File | Applies to |
| --- | --- | --- |
| User-global | `~/.config/sdd/config.yaml`, or `$XDG_CONFIG_HOME/sdd/config.yaml` when that variable is set | You, in every project on this machine |
| Project | `.sdd/config.yaml` | Everyone on the project. Committed to git. |
| Local | `.sdd/config.local.yaml` | You, in this checkout only. `sdd init` lists it in `.sdd/.gitignore`. |

A later row overrides an earlier one: local wins over project, project wins over user-global, and user-global wins over the built-in default. A file sets only the keys it names and inherits the rest. Maps merge entry by entry, so `llm.api_keys.openai` in the local file keeps `llm.api_keys.anthropic` from the user-global file. A list replaces the whole list below it. Command-line flags such as `--model` or `--embedding-provider` override every file for that one command.

### Which keys go where

| Keys | Belong in | Notes |
| --- | --- | --- |
| `participant`, `llm.*`, `embedding.*` | User-global | Any file works. Use the local file to override a value for one project. A value in the project file applies to everyone on the project and overrides their user-global value. |
| `sync.default.*`, `sync.hosts.*` | User-global | The [git limits](#sync-and-sessions) hold for every project on the machine. In a project file sdd ignores them with a warning naming the file and the key, and `sdd config set --local` refuses them. |
| `connected.*` | Any file | Whether you capture into connected graphs. See [connected repositories](#connected-repositories). |
| `inbound.*` | Project | Whether the graph accepts captures from other graphs. sdd reads it from the committed `.sdd/config.yaml` only, drops it from a local file, and `sdd config set` refuses it. |
| `sessions.retention` | User-global | sdd currently ignores it in the project files. |
| `cache.retention`, `index.replaced_retention` | User-global | The cache is shared by every project on the machine. sdd ignores them in the project files, and `sdd config set --local` refuses them. |
| `graph_dir`, `default_branch`, `repo_id`, `language`, `skill_scope`, `supported_agents`, `dependencies` | Project | These describe the repository. sdd ignores them in the user-global file. |
| `repos` | User-global | sdd ignores it in the project files. |

:::caution[Keep API keys out of the project file]
`.sdd/config.yaml` is committed and shared. sdd reads `api_keys` from any file and does not stop you, so put keys in the user-global or the local file. `sdd config set` writes only to those two.
:::

sdd skips a key it does not know, including a key in a file where it does not belong. Every command then warns `ignoring unknown config key` and names the key and the file, and `sdd config` lists these keys at the end. A file written by a newer sdd version therefore still works with an older one.

### A graph in a repository of its own

The graph can live in an sdd project of its own, beside the repository where the work happens. The work repository's history then carries only the work, and every `sdd:` commit lands in the project's repository. Point the work repository at the project with `project_dir`:

```yaml
# work repository: .sdd/config.yaml, committed
project_dir: ../app-sdd       # folder holding the project's .sdd/, relative to this repository's root
skill_scope: project
supported_agents: [claude]
```

```yaml
# work repository: .sdd/config.local.yaml, gitignored
project_dir: /home/ada/src/app-sdd   # this machine's checkout of the project; absolute paths only here
```

```text
app/                         # only work commits
  .sdd/config.yaml           # project_dir, skill_scope, supported_agents
  .sdd/config.local.yaml     # optional: project_dir for this machine
  .claude/skills/            # sdd init installs and commits these here
  .mcp.json
  src/

app-sdd/                     # only sdd: commits
  .sdd/config.yaml           # graph_dir, repo_id, default_branch, language, dependencies, ...
  .sdd/config.local.yaml     # participant and other personal settings
  .sdd/meta.json
  .sdd/graph/
```

- The project holds every setting, the graph, `meta.json`, sessions and stats. The work repository's `.sdd/config.yaml` may set only `project_dir`, `skill_scope` and `supported_agents`, and its `.sdd/config.local.yaml` only `project_dir`. sdd refuses any other key there and names the file and the key.
- `project_dir` must name an initialised project that sets no `project_dir` of its own, in either of its files.
- `sdd config set --local project_dir <path>` writes the work repository's own `.sdd/config.local.yaml`. In a repository holding its own project it is refused: point a repository with `sdd init --project-dir`.
- Commands run in the work repository act on the project: captures commit there, `sdd serve` serves it, the write gate reads its `meta.json`, `sdd repo add` declares the dependency there, and `sdd sync --pull` pulls its repository.
- The work stays in the work repository: `sdd init` installs the skills and registers the MCP server there, `sdd wip start` branches it, and closure evidence comes from its history.
- A `graph_dir` leading into another repository is refused. Make that graph a project of its own and point at it with `project_dir`. A `graph_dir` leading out of the project into no repository is refused too, by `sdd init` as by every later command, and so is a `project_dir` naming a folder inside the work repository or a folder containing it.

`sdd init --project-dir` writes the pointer and sets up a missing project:

```bash
sdd init --project-dir ../app-sdd                  # missing project: set up there, then point here
sdd init --project-dir ../app-sdd --language de    # the new project's settings; asked on a terminal otherwise
sdd init                                           # repeat in the work repository: follows the recorded project_dir
```

- A folder outside any repository is created when missing and gets its own repository through `git init`. A folder that is a repository root, such as an empty clone, is used as is.
- An absolute `--project-dir` is recorded relative to the repository, between the real folders. One outside the repository's parent folder is refused: pass it relative, or set the absolute path as `project_dir` in `.sdd/config.local.yaml`.
- The project folder must not lie inside the work repository or inside another repository, and must not contain the work repository.
- `--graph-dir` and `--language` configure the new project. Beside an existing project they are refused: run `sdd init` with them in the project.
- Only a typed `--project-dir` sets a project up. A recorded `project_dir` naming a missing folder is refused with advice to clone the project there, so a teammate's clone never grows a second, empty graph.
- `--bump` is refused in a work repository. Run it in the project.

### A graph repository sdd clones

With `graph_repo`, the graph lives in a repository of its own on a remote, and sdd keeps the only checkout of it. You keep no checkout of the graph by hand:

```yaml
# code repository: .sdd/config.yaml, committed
graph_repo: github.com/acme/app-sdd   # repo ID: host/path, no scheme, user or .git
graph_dir: .sdd/GRAPH-ELSEWHERE.md    # guard against sdd versions without graph_repo
skill_scope: project
supported_agents: [claude]
```

```bash
sdd init --graph-repo git@github.com:acme/app-sdd.git
```

- A fresh `sdd init` in a terminal asks where the graph should live: in this repository, in a repository of its own on a remote (`graph_repo`), or in a local folder without a remote (`project_dir`). It never asks again once the answer is recorded.
- `sdd init --graph-repo <address>` derives the repo ID from the address and clones the graph repository. The repository must exist on the remote, empty or set up. sdd sets up `.sdd/` there when it is empty, and pushes. Only after the push succeeds does it commit the pointer config, the guard file, the skills and the MCP registration in the code repository.
- The clone lives under `~/.local/share/sdd/repos/<repo_id>/`, or `$XDG_DATA_HOME/sdd/repos/<repo_id>/` when that variable is set. `sdd repo path <repo_id>` prints the folder. The clone holds every commit but fetches file contents only for `.sdd/` and a `graph_dir` outside it. A connected repository with the same `repo_id` shares it.
- The clone belongs to sdd. Before every sync round and every push, sdd restores the tracked files, removes untracked ones and drops commits it did not write, so a hand edit there is lost. The file `SDD_MANAGED__EDITS_ARE_DISCARDED.md` at the clone's root says so.
- The local layer is the code repository's `.sdd/config.local.yaml`. It may set any key, and `sdd config set --local` writes there, never into the clone.
- `graph_repo` wins over a `project_dir` beside it, and sdd ignores the `graph_dir` beside it. An sdd version without `graph_repo` reads that `graph_dir` as the graph folder and finds a file, so every capture fails with `not a directory` and writes nothing.
- A graph repository set up through `--graph-repo` or `--project-dir` gets `graph_dir: graph`. A recorded `graph_dir` is always kept.
- The clone [syncs on its own](#sync). When the upstream branch was force-pushed, the clone follows it and never force-pushes back. Entries the new upstream lacks are set aside under `.git/sdd-set-aside/` in the clone. One whose references all resolve and that passes pre-flight's mechanical checks returns on its own; for the rest, the sync notice names how to capture each again or discard it.

## The sdd config command

| Command | What it does |
| --- | --- |
| `sdd config` | Lists every effective value with its source: `default`, `global`, `project` or `local`. Masks API keys. Add `--format json` for JSON. |
| `sdd config get <key>` | Prints one effective value without formatting, for use in scripts. |
| `sdd config get <prefix>` | Lists the values under a prefix, such as `llm`. Without an argument, lists all values. |
| `sdd config set <key> <value>` | Writes the key to the user-global file and creates the file if needed. Works outside a project. |
| `sdd config set --local <key> <value>` | Writes the key to `.sdd/config.local.yaml`. Works only inside an sdd project. |
| `sdd config set [--local] --repo <repo_id> connected.write <value>` | Writes `connected.repos.<repo_id>.write`, keeping a `repo_id` with dots as one key. |
| `sdd config unset [--local] <key>` | Removes the key from the user-global file, or with `--local` from the local file. |

- Keys are dotted paths, such as `llm.api_keys.openai`.
- `set` refuses a key that does not belong in the target file, such as `graph_dir` without `--local`.
- `set` refuses an empty value, since `unset` removes a key, and a value outside a setting's fixed set, such as `embedding.provider voyage` or `skill_scope machine`. The refusal names the allowed values, and the file stays unchanged.
- `set` writes `8`, `0.5`, `true` and `false` as numbers and booleans, and everything else as text. Edit list values, such as `supported_agents`, in the file.
- `set` and `unset` change only the named key and keep the rest of the file, comments included.
- No command writes `.sdd/config.yaml` directly. `sdd init` creates it, `sdd repo add` and `sdd repo remove` change `dependencies`, and you edit the rest by hand and commit it.

:::note[A running server follows config changes]
Your agent starts the sdd server, `sdd serve`. Before each tool call it checks the config files. After a change it answers with the new configuration and names the settings that changed; a file that fails to load refuses every call until it is fixed. This includes the `llm` settings: the next call goes to the language model they name. sdd commands in a terminal read the configuration on every run.
:::

## Keys

Durations use the units `s`, `m` and `h`, such as `90s`, `5m` or `1h30m`.

### Project

`sdd init` writes these keys to `.sdd/config.yaml`.

| Key | Default | Meaning |
| --- | --- | --- |
| `graph_dir` | `.sdd/graph`, or `graph` in a graph repository `sdd init` sets up through `--graph-repo` or `--project-dir` | Directory of the entry files, relative to the repository root, or an absolute path inside the repository. |
| `project_dir` | None | Set only in a work repository: the sdd project holding its graph. See [a graph in a repository of its own](#a-graph-in-a-repository-of-its-own). |
| `graph_repo` | None | Set only in a code repository: the `repo_id` of the repository holding its graph, which sdd clones and syncs. See [a graph repository sdd clones](#a-graph-repository-sdd-clones). |
| `default_branch` | The branch checked out during `sdd init` | A session that names no branch works on the branch checked out in the project directory, and on this branch when none is checked out. `sdd serve` does not start without it. |
| `repo_id` | Derived from the `origin` remote | The project's identity, such as `github.com/acme/platform`. Other graphs reference an entry here as `<repo_id>:<entry-id>`. Empty for a repository without a remote. Do not change it by hand. |
| `language` | English | Language of the entry text, as a locale code such as `en`, `de` or `de-DE`. Translated SDD vocabulary ships for `de`. |
| `skill_scope` | Asked by `sdd init` | `project` installs the skill into the repository, such as `.claude/skills/`. `user` installs it into your home directory, such as `~/.claude/skills/`. |
| `supported_agents` | `[claude]` | The agents `sdd init` installs the skill for: `claude`, `codex` or both. |
| `dependencies` | None | The `repo_id`s of other graphs this graph references. See [connected repositories](#connected-repositories). |

### Participant

| Key | Default | Meaning |
| --- | --- | --- |
| `participant` | None | Your name, recorded as the author of the entries you capture. `sdd init` asks for it, offers your git `user.name`, and writes it to `.sdd/config.local.yaml` unless another file already sets it. Every command warns while it is missing. |

### Language model

| Key | Default | Meaning |
| --- | --- | --- |
| `llm.provider` | `claude-cli` | `anthropic`, `openai`, `ollama` or `claude-cli`. See [language model providers](#language-model-providers). |
| `llm.model` | `claude-sonnet-4-6` | Model name as the provider spells it. sdd uses the default with any provider, so set a model whenever you change the provider. |
| `llm.api_keys.<provider>` | None | API key for a provider. Required for `anthropic` and `openai`. Optional for `ollama`, which receives it as a bearer token. |
| `llm.endpoint` | The vendor's public API | Base URL of a gateway or compatible service, for `openai` and `anthropic` only. Accepts the host, a URL ending in `/v1`, or the full URL ending in `/chat/completions` for `openai` or `/messages` for `anthropic`. |
| `llm.ollama_endpoint` | `http://localhost:11434` | Ollama server for the `ollama` provider. |
| `llm.timeout` | `2m` | Time limit for one model call. |
| `llm.params.<name>` | None | Provider settings that sdd adds unchanged to every request body, such as `reasoning_effort` set to `low`. `claude-cli` ignores them. |
| `llm.rate_limit_rps` | See below | Maximum requests per second to `anthropic` or `openai`. Raise it on a higher account tier or for a gateway that allows more. |
| `llm.retry.max_attempts` | `5` | Attempts per call, counting the first. `1` turns retries off. |
| `llm.retry.base_delay` | `2s` | Wait before the first retry. The wait grows with each further retry. |
| `llm.retry.max_delay` | `60s` | Longest wait between two retries. |
| `llm.concurrency` | `4` | Not applied in this version. `sdd summarize --all` takes its worker count from `--concurrency`, which defaults to 4. |

sdd retries a call that failed with a temporary error, such as a rate limit or a dropped connection, and waits as long as the provider asks when it names a time. `claude-cli` ignores the rate limit and retry keys, since the `claude` command retries on its own.

Default rate limits:

| Provider | Model name contains | Requests per second |
| --- | --- | --- |
| `anthropic` | `opus` | 0.5 |
| `anthropic` | `haiku` | 2 |
| `anthropic` | anything else | 1 |
| `openai` | `mini` or `nano` | 2 |
| `openai` | anything else | 1 |
| `ollama`, `claude-cli` | any | no limit |

### Embedding

| Key | Default | Meaning |
| --- | --- | --- |
| `embedding.provider` | None | `openai` or `ollama`. Without a provider, search matches keywords only. |
| `embedding.model` | None | Embedding model, such as `text-embedding-3-small` or `qwen3-embedding:8b`. Required with a provider. |
| `embedding.api_keys.openai` | None | API key, required for `openai`. sdd does not fall back to `llm.api_keys.openai`. |
| `embedding.endpoint` | `https://api.openai.com` | Base URL of an OpenAI-compatible embeddings service, for `openai` only. sdd appends `embedding.path`, so leave the path off. |
| `embedding.path` | `/v1/embeddings` | Request path sdd appends to `embedding.endpoint`, for `openai` only. Starts with `/` and holds no scheme or host; sdd refuses anything else. `ollama` ignores it. |
| `embedding.ollama_endpoint` | `http://localhost:11434` | Ollama server for the `ollama` provider. Independent of `llm.ollama_endpoint`. |
| `embedding.timeout` | `2m` | Time limit for one request. |
| `embedding.batch_size` | `100` for `openai`, `64` for `ollama` | Texts sent in one request. |
| `embedding.dimensions` | The model's own size | Shorter vectors, for `openai` models that support it, such as `text-embedding-3-small`. `ollama` ignores it. |
| `embedding.rate_limit_rps` | `5` for `text-embedding-3` models, `3` for other `openai` models | Maximum requests per second to `openai`. `ollama` has no limit. |
| `embedding.query_template` | None | Template for search queries. See [query and document templates](#query-and-document-templates). |
| `embedding.document_template` | None | Template for entries when sdd indexes them. |

### Sync and sessions

Every git fetch and push sdd starts, from any sdd process on the machine, passes one gate that holds it to the git limits below. They hold per remote host and live in the user-global file only. A remote that is a local path or a `file://` URL is never limited.

| Key | Default | Meaning |
| --- | --- | --- |
| `sync.default.fetch_cooldown` | `5m` | Least time between two fetches from one remote. A value below `1m` is ignored. sdd lengthens it when the remotes on one host would otherwise exceed the hourly limit. |
| `sync.default.push_cooldown` | `30s` | Least time between two pushes to one remote. A value below `10s` is ignored. |
| `sync.default.retries` | `2` | Retries of a fetch or push whose transport failed, within its cooldown. A refused login, a rate limit, a rejected push or a merge conflict is never retried. |
| `sync.default.concurrency` | `2` | Git network operations that run at the same time against one host. |
| `sync.default.lease_pause` | `10m` | How long a clone's [lease](#leases) holds after the last call that renewed it. A value below `1m` is ignored. |
| `sync.default.read_wait` | `15s` | How long a call waits for the sync round its lease start runs. A value below `1s` is ignored. |
| `sync.default.ssh.per_hour` | `600` | SSH operations per hour against one host. |
| `sync.default.https.per_hour` | `600` | HTTPS operations per hour against one host, with credentials. |
| `sync.default.https.per_hour_unauthenticated` | `60` | HTTPS operations per hour against one host, without credentials. |
| `sync.default.ssh.per_minute` | A tenth of `ssh.per_hour` | SSH operations per minute against one host. Once reached, every operation waits for the next minute. |
| `sync.default.https.per_minute` | A tenth of `https.per_hour` | The same for HTTPS with credentials. |
| `sync.default.https.per_minute_unauthenticated` | A tenth of `https.per_hour_unauthenticated` | The same for HTTPS without credentials. |
| `sync.hosts.<host>.<limit>` | The `sync.default` value | Any limit above for one remote host, such as `sync.hosts.github.com.https.per_hour`. |
| `sessions.retention` | `14d` | When `sdd serve` starts, it deletes the logs of sessions that ended longer ago than this. Accepts days, such as `14d`, or a duration, such as `336h`. Set it in the user-global file. An invalid value stops `sdd serve` from starting. |

`sdd config set` refuses a value below its minimum; read from a file, such a value is ignored with a warning. A git limit in a project file is ignored with a warning naming the file and the key.

`sync.cooldown` no longer exists. sdd reads past it in any file without a warning, and `sdd config` does not list it. `sdd config set` refuses it: `git operations sdd starts are limited per remote host under sync.default and sync.hosts in the global config`.

#### Leases

The clones sdd keeps under `~/.local/share/sdd/repos/`, those of connected repositories and the project's own `graph_repo` clone, fetch automatically only while a lease holds. A graph inside the code repository has no lease and never syncs automatically. Every `sdd serve` on the machine shares one lease per clone. A lease starts or renews:

- for the home project's graph, on `start_session`, `start_procedure`, `resume_session` and a read naming no repository;
- for a project, on every procedure call working in it: `start_procedure`, `next`, `park` and `abandon`;
- for a repository a call names: `search` or `view` with `repos` or `all_repos`; `search`, `show`, `view`, `read_attachment` or `info` with `project`; a capture ref `<repo-id>:<entry-id>`; a capture into a dependency.

`registry` reads no repository and renews nothing. A lease ends `lease_pause` after its last renewal.

While a lease holds, the timer of `sdd serve` fetches the clone once per effective fetch cooldown, described under [git credits](#git-credits). Without a lease it fetches nothing. Commits waiting in a clone are pushed whether a lease holds or not.

When a call starts a lease and the clone's effective fetch cooldown has passed, one sync round runs at once and the call waits for it at most `read_wait`. Past that, the call reads the clone as it stands and its sync notice says the refresh still runs and when the clone last fetched; the round finishes in the background. A lease starting inside the cooldown reads the clone as it stands and fetches nothing. The CLI commands that read connected repositories, `sdd show`, `sdd view`, `sdd search`, `sdd export` and `sdd new` with a ref into one, start or renew the leases of the repositories they read the same way and print the same warning. A command ends only once its rounds have finished.

#### Git credits

sdd counts every git network operation it starts in a bucket per host: SSH, HTTPS with credentials, or HTTPS without. Automatic fetches may use 90 percent of each bucket's hourly limit; the rest is kept for pushes and for syncs you command. The clones sdd keeps under `~/.local/share/sdd/repos/` share the fetches: the fetch cooldown of each repository is at least one hour times the number of those clones in its bucket, divided by 90 percent of the bucket's hourly limit. Repositories you work in yourself and removed clones do not count. With the defaults this lengthens the cooldown from 46 clones on, or from 5 over HTTPS without login. An HTTPS repository whose login sdd does not know yet counts in both HTTPS buckets and takes the longer cooldown.

Once a clone may fetch again, each span of one cooldown that passes without a fetch earns its host and protocol one credit. The credit expires one hour after its span began. A credit lets one operation run inside its cooldown:

- The push of commits sdd just wrote, such as a capture into a connected repository right after its last push.
- A sync you command with `sdd repo sync` or the `sync_repos` tool, its fetch or its push.

Other reads and the rounds `sdd serve` runs on its own never spend a credit. Even with one, fetches of one repository stay at least 1 minute apart and its pushes at least 10 seconds apart, the hourly and per-minute limits and `concurrency` still apply, and the cooldown after a failed operation holds. Without a credit the operation waits for its cooldown.

#### Sync

How sdd syncs a graph depends on where the graph lives.

A graph inside the code repository syncs only when you ask. A capture stays a local commit, and sdd never merges into your branch unasked. `sdd sync` fetches as far as the git limits admit, then prints where the branch stands:

```text
sync: fast-forward available, 3 commits behind
sync: merge is clean, 2 remote / 1 local
sync: merge would conflict in .sdd/graph/2026/11/15-091200-d-tac-abc.md, 2 remote / 1 local
sync: local ahead by 1, consider push
sync: fetch skipped, next allowed at 14:05
```

Without an upstream, `sdd sync` fails with the status line as its error. `sdd sync --pull` merges the upstream branch, never rebases, and commits the merge as `sdd: merge <upstream>`. Every other command except `sdd init` and `sdd cache` runs the same fetch in the background and reports when your branch is behind or ahead of its upstream.

A [`graph_repo` clone](#a-graph-repository-sdd-clones) syncs on its own:

- `sdd new`, `sdd summarize`, `sdd wip start`, `sdd wip done`, `sdd lint fix`, `sdd rewrite`, `sdd init` and `sdd repo add` push their commit before they return, and so does every capture through `sdd serve`. A failed push fails the command and keeps the commit.
- A sync round fetches, merges the upstream branch and pushes what waits. A rejected push merges the upstream branch and pushes once more. The timer of `sdd serve` runs a round while the clone's [lease](#leases) holds and its fetch is due.
- `sdd sync` runs a round at once, also while sync is paused.

A round that cannot finish leaves a record for the branch. Every tool result of your agent's `sdd serve` then carries a sync notice. The first session to see it gets the steps to fix it, and `sdd info` shows the record with the same steps. A merge conflict pauses automatic sync until you merge by hand; other blocks are retried each round.

#### Merge drivers

`sdd init` routes the entry files to the merge driver `sdd git-merge entry` in `<graph_dir>/.gitattributes`, and every command that touches the graph's git writes the driver into `.git/config`. When only one side of a merge changed an entry's `title` or `summary`, that side wins. Changed on both sides, the value whose UTF-8 bytes sort first wins, so every clone gets the same result in either merge direction. The body and the other keys merge as text. Without the driver in `.git/config`, or without `sdd` on `PATH`, git merges the file as text with conflict markers.

In the clones it keeps, sdd passes the drivers with every git call, and `sdd git-merge config` merges `.sdd/config.yaml` key by key: `dependencies` as a set, and a value changed on both sides takes the upstream value, with a notice. An upstream deletion of an entry wins over a local `title` or `summary` change. git calls both drivers; you never run them yourself.

### Cache

Both keys accept days, such as `90d`, or a duration, such as `2160h`. `sdd config set` refuses any other value. See [the search index](#the-search-index) for what `sdd cache prune` deletes.

| Key | Default | Meaning |
| --- | --- | --- |
| `cache.retention` | `90d` | `sdd cache prune` deletes an index store, a graph snapshot, an embedding calibration, a git gate record or a dropped repository's clone nothing used for longer than this. Also how long a checkout's record keeps the index versions it reads. |
| `index.replaced_retention` | `14d` | `sdd cache prune` deletes an index version replaced longer ago than this, versions of an older index format unused that long, and the index store of an embedding setup another one replaced. |

### Connected repositories

| Key | File | Default | Meaning |
| --- | --- | --- | --- |
| `dependencies` | Project | None | The `repo_id`s of the graphs this graph references. `sdd repo add` adds one and commits the change. `sdd repo remove` removes one. |
| `repos` | User-global | None | How this machine reaches each connected repository, as a list of `repo_id` and `clone_url` pairs. `sdd repo add` writes it. |

```yaml
# ~/.config/sdd/config.yaml
repos:
  - repo_id: github.com/acme/platform
    clone_url: git@github.com:acme/platform.git
```

sdd keeps a clone of each connected repository under `~/.local/share/sdd/repos/<repo_id>`, the same kind of clone as a [`graph_repo` clone](#a-graph-repository-sdd-clones). A read that needs it starts or renews the clone's [lease](#leases), which runs the clone's sync round once its effective fetch cooldown has passed. The [connected repositories guide](/sdd/guides/connected-repositories/#a-connected-graph-is-what-was-pushed) describes the round.

#### Captures into connected graphs

A capture into another graph needs the consent of both sides. Both settings are off by default.

| Key | File | Default | Meaning |
| --- | --- | --- | --- |
| `connected.write` | Any file | `false` | Whether you capture into connected graphs. |
| `connected.repos.<repo_id>.write` | Any file | `connected.write` | The same for one repository. It wins over `connected.write` after the layers merge. |
| `inbound.accept` | Project | `false` | Whether this graph accepts captures from other graphs. |
| `inbound.repos.<repo_id>.accept` | Project | `inbound.accept` | The same for captures from one repository. |

```yaml
# writing side: user-global, .sdd/config.yaml or .sdd/config.local.yaml
connected:
  write: false
  repos:
    github.com/acme/platform:
      write: true
```

```yaml
# receiving side: the graph's committed .sdd/config.yaml only
inbound:
  accept: false
  repos:
    github.com/acme/app:
      accept: true
```

```bash
sdd config set --repo github.com/acme/platform connected.write true
```

sdd checks both settings when a procedure that publishes into another graph starts, before the dialogue, and reads `inbound.accept` again on the fresh clone before it writes. The capture writes through the sdd-owned clone and pushes from there. A refusal names the closed side and how to open it:

```text
capture into a connected graph refused: connected.write does not allow writing into github.com/acme/platform; set it with `sdd config set --repo github.com/acme/platform connected.write true`
capture into a connected graph refused: github.com/acme/platform does not accept captures from github.com/acme/app; its committed .sdd/config.yaml needs inbound.accept or inbound.repos.github.com/acme/app.accept set to true
```

## Language model providers

sdd calls the language model three times when it records an entry:

- The writing check reads the draft and points out unclear text.
- The pre-flight check compares the entry with the graph.
- The summary step writes the entry's one-line summary.

:::caution[The model must support structured output]
The writing check and the pre-flight check ask the model for a fixed response format, a JSON schema the provider must enforce. sdd sends the schema through each provider's structured output feature and never falls back to reading free text, so a provider or model without structured output fails the checks. Ollama Cloud ignores the schema and does not work. The summary is plain text and works with any model.
:::

| Provider | Sends requests to | Schema sent as | API key |
| --- | --- | --- | --- |
| `anthropic` | The Anthropic Messages API, or `llm.endpoint` | `output_config.format` | `llm.api_keys.anthropic` |
| `openai` | OpenAI chat completions, or any compatible service at `llm.endpoint` | `response_format` of type `json_schema`, strict | `llm.api_keys.openai` |
| `ollama` | The Ollama server at `llm.ollama_endpoint` | The `format` field | Optional |
| `claude-cli` | The local `claude` command, run as `claude -p` | `--json-schema` | None, uses your Claude Code login |

A gateway or compatible service must honor the schema field in the third column for the checks to work.

### Anthropic

```bash
sdd config set llm.provider anthropic
sdd config set llm.model claude-sonnet-4-6
sdd config set llm.api_keys.anthropic <your-api-key>
```

```yaml
llm:
  provider: anthropic
  model: claude-sonnet-4-6
  api_keys:
    anthropic: <your-api-key>
```

### OpenAI

```bash
sdd config set llm.provider openai
sdd config set llm.model gpt-5-mini
sdd config set llm.api_keys.openai <your-api-key>
```

```yaml
llm:
  provider: openai
  model: gpt-5-mini
  api_keys:
    openai: <your-api-key>
```

### Opper and other OpenAI-compatible services

With `llm.endpoint` set, the `openai` provider sends its requests to any service that implements the OpenAI chat completions protocol. [Opper](https://opper.ai) is a gateway that serves hosted open-weight models this way. The SDD evals run this model through this route:

```bash
sdd config set llm.provider openai
sdd config set llm.endpoint https://api.opper.ai/v3/compat/chat/completions
sdd config set llm.model greenpt/glm-5.3-flash
sdd config set llm.api_keys.openai <your-opper-key>
```

```yaml
llm:
  provider: openai
  endpoint: https://api.opper.ai/v3/compat/chat/completions
  model: greenpt/glm-5.3-flash
  api_keys:
    openai: <your-opper-key>
```

The Opper key goes under `openai` because the provider is `openai`. The default limit of 1 request per second assumes OpenAI's lowest account tier. Raise `llm.rate_limit_rps` if the service allows more.

### Ollama

Runs models on your machine. The Ollama server must be running when sdd starts, or sdd stops with `provider unreachable`.

```bash
sdd config set llm.provider ollama
sdd config set llm.model gemma4:26b
```

```yaml
llm:
  provider: ollama
  model: gemma4:26b
  ollama_endpoint: http://localhost:11434
```

Ollama Cloud does not work, because it ignores the response schema.

### Claude Code CLI

`claude-cli` is the fallback when no provider is set. It runs every call through `claude -p` with your Claude Code login, so it needs no API key, only the `claude` command on your `PATH`. Each call starts a new Claude Code process, which is slow, and checks can hit the 2 minute limit. If they do, raise `llm.timeout` or set up another provider.

```bash
sdd config set llm.provider claude-cli
sdd config set llm.model claude-sonnet-4-6
```

```yaml
llm:
  provider: claude-cli
  model: claude-sonnet-4-6
```

## Embedding providers

An embedding model turns each entry into a vector, so search finds entries by meaning. Without one, search matches keywords. `sdd info` shows `Search: vector,text` with an embedding provider and `Search: text` without.

Set the embedding provider in the user-global file. A search across connected repositories embeds every repository, the current one included, with the user-global embedding settings, so all results share one vector space.

### OpenAI

```bash
sdd config set embedding.provider openai
sdd config set embedding.model text-embedding-3-small
sdd config set embedding.api_keys.openai <your-api-key>
```

```yaml
embedding:
  provider: openai
  model: text-embedding-3-small
  api_keys:
    openai: <your-api-key>
```

`embedding.api_keys.openai` is separate from `llm.api_keys.openai`. Set it even when the language model uses the same key.

### OpenAI-compatible services

For another service that implements the OpenAI embeddings API, set `embedding.endpoint` to its base URL without a path. sdd posts to `/v1/embeddings` under it. For a gateway that serves the route elsewhere, set `embedding.path` too, such as `/v3/compat/embeddings`. The `openai` provider always sends `embedding.api_keys.openai`, so for a service without authentication, set any value.

```bash
sdd config set embedding.provider openai
sdd config set embedding.endpoint http://localhost:8000
sdd config set embedding.model <model>
sdd config set embedding.api_keys.openai <key>
```

```yaml
embedding:
  provider: openai
  endpoint: http://localhost:8000
  model: <model>
  api_keys:
    openai: <key>
```

### Ollama

```bash
sdd config set embedding.provider ollama
sdd config set embedding.model qwen3-embedding:8b
```

```yaml
embedding:
  provider: ollama
  model: qwen3-embedding:8b
  ollama_endpoint: http://localhost:11434
  query_template: |-
    Instruct: Given a query phrase, retrieve related entries from a knowledge graph
    Query:{text}
```

The next section explains `query_template`.

### Query and document templates

Some embedding models expect an instruction or a prefix before the text. A template wraps the text, and `{text}` stands for it.

- `embedding.query_template` applies to search queries.
- `embedding.document_template` applies to entries when sdd indexes them.

Qwen3 embedding models expect an instruction on queries only, so set `query_template` as in the Ollama example above and leave `document_template` empty. Models with a prefix on both sides, such as E5 with `query: ` and `passage: ` or Nomic with `search_query: ` and `search_document: `, need both templates. OpenAI models need none.

A changed `query_template` applies to the next search. A changed `document_template`, provider, model or `dimensions` starts a new, empty index, which sdd fills on the next search or with `sdd index`.

### The search index

- `sdd index` embeds every entry the index does not hold yet. Run it once after you set up an embedding provider, or when you join a project with existing entries, so the first search does not have to wait.
- Every search by meaning, in the terminal or through your agent, first embeds the entries the index lacks, so new entries need no extra step.
- `sdd index --force` embeds every entry again.
- `sdd index --repo <repo_id>` or `sdd index --all-repos` also builds the indexes of connected repositories.

The index lives in `~/.cache/sdd/index/`, or `$XDG_CACHE_HOME/sdd/index/` when that variable is set, with one directory per project and embedding setup. A project's directory is named after its `repo_id`, or after a hash of the repository path when it has none.

A search waits at most 5 seconds for an index another process writes, then prints `sdd: search index busy for 5s; falling back to text search` and matches the query's words as text. A search that finds another process filling the index prints `sdd: another process is filling the search index; searching the index as it stands` and searches without waiting. `sdd index` waits for that fill, and searches through `sdd serve` skip it as the terminal does. A connected repository's index held past 5 seconds is searched by text. Without a terminal, a fill longer than 15 seconds prints its progress on stderr, at most every 15 seconds.

#### Input limit

Each embedding endpoint accepts a limited input, in tokens. On its first fill, sdd measures that limit with three requests: 1000 Latin characters, 1000 CJK characters and 20,000 Latin characters. An endpoint that cuts long input off or refuses it costs up to nine more requests, plus the repeats below. A request that fails without an HTTP error answer (a dropped connection, a server error, a timeout, a refused connection, a failed name lookup, a rate limit, an unknown error) is sent once more. A connection dropped twice, or a server error twice, counts as a refusal only if one more request at the last accepted size succeeds, and the next fill measures that limit once more, one process at a time and at most every 10 minutes: a limit at least as high is kept for good, a lower one replaces it, and a failed measurement keeps it to measure again. Any other repeated failure, or a failed request at the last accepted size, fails the fill without storing a limit. sdd then cuts every chunk to 90 percent of the limit, counted for the costliest text, and stores the result per embedding setup, `embedding.endpoint` and `embedding.path` in `calibration/` beside the index. An endpoint that reports no token counts gets a cap of 2048 characters. A changed cap embeds every entry once more. Searches keep finding every entry meanwhile.

#### Cleanup

`sdd cache prune` deletes what nothing needs and reports the bytes freed:

- after `index.replaced_retention`: replaced index versions, versions of older index formats, the index store of an embedding setup another one replaced
- after `cache.retention`: unused index stores, graph snapshots and calibrations, and the git gate's record of a remote no lease, fetch or push touched within that time and with no next allowed time ahead
- at once: graph snapshots of directories that no longer exist
- the clone of a repository `sdd repo remove` dropped under the same config file that `repos` no longer names, under `~/.local/share/sdd/repos/` or where an earlier sdd kept it under `~/.cache/sdd`, unless a sync holds it, it holds commits no remote-tracking branch reaches, or a fetch or push reached its remote within `cache.retention`; a kept clone is logged with its reason
- a dropped clone holding another clone, or the path of a repository `repos` names, stays, with a warning
- a clone missing from `repos` alone stays, `graph_repo` clones included, and so does every clone of a repository `repos` names
- every clone of a repository `repos` names gets `git gc --auto`, which packs it by git's own thresholds; a clone a sync holds waits for the next run
- with `repos` missing or empty, or the XDG directories set only in part, a run that would remove a clone removes none and fails
- leftovers of killed processes older than a day: git trace files, unfinished clones, and the folders of an older clone layout that hold only lock files

It never deletes a version any checkout read within `cache.retention`, and it skips an index store another process holds. One run per machine at a time, at most 5 minutes. `sdd info` shows the last run. `sdd cache prune` reports a failed run as its error.

`sdd index gc` lists the stored versions by group, `sdd index gc --drop <group>` deletes one group, and `--drop stale` deletes every version no search of this checkout reads.

## Environment variables

| Variable | Default | Effect |
| --- | --- | --- |
| `XDG_CONFIG_HOME` | `~/.config` | The user-global file is `$XDG_CONFIG_HOME/sdd/config.yaml`. |
| `XDG_CACHE_HOME` | `~/.cache` | Search indexes, embedding calibrations and graph snapshots live under `$XDG_CACHE_HOME/sdd`. |
| `XDG_DATA_HOME` | `~/.local/share` | The clones sdd keeps of connected repositories and of `graph_repo` graphs live under `$XDG_DATA_HOME/sdd/repos`. |
| `XDG_STATE_HOME` | `~/.local/state` | Session logs, the record of the last `sdd cache prune` and the git gate's record of each remote live under `$XDG_STATE_HOME/sdd`. Must be an absolute path. |
| `SDD_GRAPH_CACHE` | On | Every command keeps a snapshot of each graph it loads under `$XDG_CACHE_HOME/sdd/graph`, and reads it while no graph file changed. A cache dir sdd cannot write costs one warning and no failure. `off` reads every entry file, for sandboxes and read-only homes. |
| `SDD_TIMING` | Off | Any true value prints one line on stderr per ended phase of a command, `timing: <phase> <duration>ms at <offset>ms`. Same as `--timing`. |
| `SDD_SERVE_TOKEN` | None | The bearer token `sdd serve --transport http` requires on every request. Same as `--auth-token`. |
