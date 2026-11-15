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
| Local | `.sdd/config.local.yaml` | You, in this checkout only. `sdd init` adds it to `.gitignore`. |

A later row overrides an earlier one: local wins over project, project wins over user-global, and user-global wins over the built-in default. A file sets only the keys it names and inherits the rest. Maps merge entry by entry, so `llm.api_keys.openai` in the local file keeps `llm.api_keys.anthropic` from the user-global file. A list replaces the whole list below it. Command-line flags such as `--model` or `--embedding-provider` override every file for that one command.

### Which keys go where

| Keys | Belong in | Notes |
| --- | --- | --- |
| `participant`, `llm.*`, `embedding.*`, `sync.*` | User-global | Any file works. Use the local file to override a value for one project. A value in the project file applies to everyone on the project and overrides their user-global value. |
| `sessions.retention` | User-global | sdd currently ignores it in the project files. |
| `graph_dir`, `default_branch`, `repo_id`, `language`, `skill_scope`, `supported_agents`, `dependencies` | Project | These describe the repository. sdd ignores them in the user-global file. |
| `repos` | User-global | sdd ignores it in the project files. |
| `hooks` | Any file | The lists of all files run together. See [hooks](#hooks). |

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
  .sdd/config.yaml           # project_dir, skill_scope, supported_agents, hooks
  .sdd/config.local.yaml     # optional: project_dir and hooks for this machine
  .claude/skills/            # sdd init installs and commits these here
  .mcp.json
  src/

app-sdd/                     # only sdd: commits
  .sdd/config.yaml           # graph_dir, repo_id, default_branch, language, dependencies, ...
  .sdd/config.local.yaml     # participant and other personal settings
  .sdd/meta.json
  .sdd/graph/
```

- The project holds every setting, the graph, `meta.json`, sessions and stats. The work repository's `.sdd/config.yaml` may set only `project_dir`, `skill_scope`, `supported_agents` and [`hooks`](#hooks), and its `.sdd/config.local.yaml` only `project_dir` and `hooks`. sdd refuses any other key there and names the file and the key.
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

## The sdd config command

| Command | What it does |
| --- | --- |
| `sdd config` | Lists every effective value with its source: `default`, `global`, `project` or `local`. Masks API keys. Add `--format json` for JSON. |
| `sdd config get <key>` | Prints one effective value without formatting, for use in scripts. |
| `sdd config get <prefix>` | Lists the values under a prefix, such as `llm`. Without an argument, lists all values. |
| `sdd config set <key> <value>` | Writes the key to the user-global file and creates the file if needed. Works outside a project. |
| `sdd config set --local <key> <value>` | Writes the key to `.sdd/config.local.yaml`. Works only inside an sdd project. |
| `sdd config unset [--local] <key>` | Removes the key from the user-global file, or with `--local` from the local file. |

- Keys are dotted paths, such as `llm.api_keys.openai`.
- `set` refuses a key that does not belong in the target file, such as `graph_dir` without `--local`.
- `set` refuses an empty value, since `unset` removes a key, and a value outside a setting's fixed set, such as `embedding.provider voyage` or `skill_scope machine`. The refusal names the allowed values, and the file stays unchanged.
- `set` writes `8`, `0.5`, `true` and `false` as numbers and booleans, and everything else as text. Edit list values, such as `supported_agents`, in the file.
- `set` and `unset` change only the named key and keep the rest of the file, comments included.
- No command writes `.sdd/config.yaml` directly. `sdd init` creates it, `sdd repo add` and `sdd repo remove` change `dependencies`, and you edit the rest by hand and commit it.

:::note[Restart your agent after a change]
Your agent starts the sdd server, `sdd serve`, which reads the configuration once when it starts. Restart the agent after a change so the server picks it up. In Claude Code it is enough to run `/mcp` and restart the sdd server there. sdd commands in a terminal read the configuration on every run.
:::

## Keys

Durations use the units `s`, `m` and `h`, such as `90s`, `5m` or `1h30m`.

### Project

`sdd init` writes these keys to `.sdd/config.yaml`.

| Key | Default | Meaning |
| --- | --- | --- |
| `graph_dir` | `.sdd/graph` | Directory of the entry files, relative to the repository root, or an absolute path inside the repository. |
| `project_dir` | None | Set only in a work repository: the sdd project holding its graph. See [a graph in a repository of its own](#a-graph-in-a-repository-of-its-own). |
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
| `embedding.endpoint` | `https://api.openai.com` | Base URL of an OpenAI-compatible embeddings service, for `openai` only. sdd appends `/v1/embeddings`, so leave `/v1` off. |
| `embedding.ollama_endpoint` | `http://localhost:11434` | Ollama server for the `ollama` provider. Independent of `llm.ollama_endpoint`. |
| `embedding.timeout` | `2m` | Time limit for one request. |
| `embedding.batch_size` | `100` for `openai`, `64` for `ollama` | Texts sent in one request. |
| `embedding.dimensions` | The model's own size | Shorter vectors, for `openai` models that support it, such as `text-embedding-3-small`. `ollama` ignores it. |
| `embedding.rate_limit_rps` | `5` for `text-embedding-3` models, `3` for other `openai` models | Maximum requests per second to `openai`. `ollama` has no limit. |
| `embedding.query_template` | None | Template for search queries. See [query and document templates](#query-and-document-templates). |
| `embedding.document_template` | None | Template for entries when sdd indexes them. |

### Sync and sessions

| Key | Default | Meaning |
| --- | --- | --- |
| `sync.cooldown` | `15m` | Every sdd command except `sdd init` runs `git fetch` in the background at most once per this interval, and reports when your branch is behind or ahead of its upstream. An invalid value counts as `15m`. |
| `sessions.retention` | `14d` | When `sdd serve` starts, it deletes the logs of sessions that ended longer ago than this. Accepts days, such as `14d`, or a duration, such as `336h`. Set it in the user-global file. An invalid value stops `sdd serve` from starting. |

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

sdd keeps a clone of each connected repository under `~/.cache/sdd/<repo_id>`. A read that needs it pulls the clone at most once every 15 minutes. This interval is fixed.

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

For another service that implements `POST /v1/embeddings`, set `embedding.endpoint` to its base URL without `/v1`. The `openai` provider always sends `embedding.api_keys.openai`, so for a service without authentication, set any value.

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

The index only grows. It keeps every embedded version of every entry, so sdd versions that share it do not remove each other's work. `sdd index gc` lists the stored versions by group, `sdd index gc --drop <group>` deletes one group, and `--drop stale` deletes every version this checkout does not search.

## Hooks

A hook is a script sdd runs at an event of a running procedure, such as a prose linter on a capture draft. It reads the event as JSON on stdin and may answer with findings, which join the step's own.

| Key | Default | Meaning |
| --- | --- | --- |
| `event` | Required | The event, below. |
| `command` | Required | A file in the layer's hooks folder. Escape a space in its name with a backslash. |
| `args` | None | Arguments passed as they stand, without a shell. |
| `timeout` | `600` | Seconds before sdd stops the hook and its child processes and fails the call. |
| `findings` | `false` | The hook may answer with findings. Only an event with a findings channel accepts it. |

### Where hooks live

Every config file may declare `hooks`: the user-global file, and both `.sdd/config.yaml` and `.sdd/config.local.yaml` of the project. In a project whose graph lives in a [repository of its own](#a-graph-in-a-repository-of-its-own), the work repository's two files declare hooks as well. sdd runs the hooks of every file together. Hooks run without an approval step, as git runs the files of `.git/hooks/`: a project's committed hooks run on the machine of everyone working on it.

`command` names a file in the declaring layer's hooks folder:

| Declared in | Hooks folder |
| --- | --- |
| User-global config | `~/.config/sdd/hooks/` |
| `.sdd/config.yaml` | `.sdd/hooks/`, committed with the project |
| `.sdd/config.local.yaml` | `.sdd/hooks/`, its scripts in `local/` there, which `sdd init` adds to `.gitignore` |

sdd refuses an absolute path, `..`, a name it would have to look up on `PATH`, and anything a shell would interpret. A hook runs without a shell, with the hooks folder as its working directory, so `args` resolve from there too. Put an interpreter in the script's shebang and make the script executable. A hook in the committed file may not run a script from `local/`, which only your machine has.

### Events

| Event | Fires | Accepts findings |
| --- | --- | --- |
| `capture.assemble` | The draft is complete, before any paid call | Yes |
| `capture.guide` | The writing guide is done | Yes |
| `capture.preflight` | Pre-flight is done | Yes |
| `entry.written` | A capture wrote its entry | No |

Every step of every procedure raises `<procedure>.<step>` when it is done, also named `<procedure>.<step>.done`, and `<procedure>.<step>.start` when the procedure arrives at it. `*` stands for any procedure or step: `*.*` matches every step being done, `*.*.start` every arrival. Only the events in the table are supported; the others follow the procedures' steps and may change with them.

A failing `.start` hook holds its step: sdd runs it again on every call, and the step's work, such as the pre-flight call, waits until it passes.

### Running a hook

Hooks on one event run at once. A hook fails the call when it exits nonzero, outlives its timeout, or writes anything to stdout but nothing or `{"findings": [...]}`. A high finding holds the step like one of the step's own.

The payload carries `event`, `time`, `project`, `branch`, `session`, `procedure`, `instance`, `step` and `phase` (`start`, `done` or `round`); `transition`, the step the procedure moves to, when a step is done; `findings`, the step's findings so far, on a step with a findings channel; `draft` during a capture, with `type`, `kind`, `layer`, `refs`, `topics`, `body`, `attachments`, `body_hash`, `supersedes` and `closes` (entry ID lists); and `entry`, the written entry's ID, on `entry.written`. A finding has `severity` (`high`, `medium` or `low`), `category` and `observation`.

A Vale check on every capture draft:

```yaml
# .sdd/config.yaml
hooks:
  - event: capture.assemble
    command: vale.sh
    findings: true
    timeout: 60
```

```sh
#!/bin/sh
# .sdd/hooks/vale.sh
set -e
jq -r .draft.body | vale --config .vale.ini --ext .md --output JSON --no-exit |
  jq '{findings: [.[][] | {category: .Check, observation: .Message,
    severity: (if .Severity == "error" then "high" elif .Severity == "warning" then "medium" else "low" end)}]}'
```

## Environment variables

| Variable | Default | Effect |
| --- | --- | --- |
| `XDG_CONFIG_HOME` | `~/.config` | The user-global file is `$XDG_CONFIG_HOME/sdd/config.yaml`. |
| `XDG_CACHE_HOME` | `~/.cache` | Search indexes and clones of connected repositories live under `$XDG_CACHE_HOME/sdd`. |
| `XDG_STATE_HOME` | `~/.local/state` | Session logs live under `$XDG_STATE_HOME/sdd`. Must be an absolute path. |
| `SDD_SERVE_TOKEN` | None | The bearer token `sdd serve --transport http` requires on every request. Same as `--auth-token`. |
