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

The global `--local-config <path>` flag points the local layer at another file for the whole command: reads resolve it, and `--local` writes land in it. A sandbox, a CI runner or a second identity can so supply its own local layer without touching the checkout.

```bash
sdd --local-config ~/ci-local.yaml config get participant
```

### Which keys go where

| Keys | Belong in | Notes |
| --- | --- | --- |
| `participant`, `llm.*`, `embedding.*` | User-global | Any file works. Use the local file to override a value for one project. A value in the project file applies to everyone on the project and overrides their user-global value. |
| `sessions.retention` | User-global | sdd currently ignores it in the project files. |
| `sync.default.*`, `sync.hosts.*` | User-global | The git limits. sdd reads them from the user-global file only. |
| `graph_dir`, `default_branch`, `repo_id`, `language`, `skill_scope`, `supported_agents`, `dependencies` | Project | These describe the repository. sdd ignores them in the user-global file. |
| `repos` | User-global | sdd ignores it in the project files. |
| `hooks` | Any file | The lists of all files run together. A hook from a project file runs only once you [confirm it](#confirming-a-hook). |

:::caution[Keep API keys out of the project file]
`.sdd/config.yaml` is committed and shared. sdd reads `api_keys` from any file and does not stop you, so put keys in the user-global or the local file. `sdd config set` writes only to those two.
:::

sdd skips a key it does not know, including a key in a file where it does not belong. Every command then warns `ignoring unknown config key` and names the key and the file, and `sdd config` lists these keys at the end. A file written by a newer sdd version therefore still works with an older one.

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
- `set` refuses an empty value, and a value sdd would refuse or ignore when it reads the file: one outside a setting's fixed set, such as `verify.mode light`, or a `verify` value the layers do not allow together, such as a local `verify.mode` below the shared one. The refusal names the allowed values, and the file stays unchanged.
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
| `graph_dir` | `.sdd/graph` | Directory of the entry files, relative to the repository root, or an absolute path. |
| `default_branch` | The branch checked out during `sdd init` | A session that names no branch works on the branch checked out in the project directory, and on this branch when none is checked out. `sdd serve` does not start without it. |
| `repo_id` | Derived from the `origin` remote | The project's identity, such as `github.com/acme/platform`. Other graphs reference an entry here as `<repo_id>:<entry-id>`. Empty for a repository without a remote. Do not change it by hand. |
| `language` | English | Language of the entry text, as a locale code such as `en`, `de` or `de-DE`. Translated SDD vocabulary ships for `de`. |
| `skill_scope` | Asked by `sdd init` | `project` installs the skill into the repository, such as `.claude/skills/`. `user` installs it into your home directory, such as `~/.claude/skills/`. |
| `supported_agents` | `[claude]` | The agents `sdd init` installs the skill for: `claude`, `codex` or both. |
| `dependencies` | None | The `repo_id`s of other graphs this graph references. See [connected repositories](#connected-repositories). |
| `project_dir` | None | Set in a work repository only: the folder holding the sdd project's `.sdd/`, relative to the repository root or absolute. See [a graph in its own repository](/sdd/guides/graph-repository/). |

### Participant

| Key | Default | Meaning |
| --- | --- | --- |
| `participant` | None | Your name, recorded as the author of the entries you capture. `sdd init` asks for it, offers your git `user.name`, and writes it to `.sdd/config.local.yaml` unless another file already sets it. Every command warns while it is missing. |

### Language model

| Key | Default | Meaning |
| --- | --- | --- |
| `llm.provider` | `claude-cli` | `anthropic`, `openai`, `ollama`, `claude-cli` or `codex`. See [language model providers](#language-model-providers). |
| `llm.model` | `claude-sonnet-4-6`, none for `codex` | Model name as the provider spells it. sdd uses the default with any provider but `codex`, so set a model whenever you change to another provider. Unset under `codex`, codex runs its own default model. |
| `llm.effort` | `medium` | Reasoning effort of `claude-cli`, passed to `--effort`, and of `codex`, passed to `model_reasoning_effort`, as written. The other providers ignore it and take an effort through `llm.params`. |
| `llm.api_keys.<provider>` | None | API key for a provider. Required for `anthropic` and `openai`. Optional for `ollama`, which receives it as a bearer token. |
| `llm.endpoint` | The vendor's public API | Base URL of a gateway or compatible service, for `openai` and `anthropic` only. Accepts the host, a URL ending in `/v1`, or the full URL ending in `/chat/completions` for `openai` or `/messages` for `anthropic`. |
| `llm.ollama_endpoint` | `http://localhost:11434` | Ollama server for the `ollama` provider. |
| `llm.timeout` | `2m` | Time limit for one model call. |
| `llm.extract_timeout` | `1m` | Time limit for the reformat call that follows a check response that does not parse. |
| `llm.params.<name>` | None | Provider settings that sdd adds unchanged to every request body, such as `reasoning_effort` set to `low`. `claude-cli` and `codex` ignore them. |
| `llm.structured_output` | `auto` | How the checks ask `openai` and `ollama` for JSON: `json_schema`, `json_object` or `auto`. See [structured output modes](#structured-output-modes). |
| `llm.rate_limit_rps` | See below | Maximum requests per second to `anthropic` or `openai`. Raise it on a higher account tier or for a gateway that allows more. |
| `llm.retry.max_attempts` | `5` | Attempts per call, counting the first. `1` turns retries off. |
| `llm.retry.base_delay` | `2s` | Wait before the first retry. The wait grows with each further retry. |
| `llm.retry.max_delay` | `60s` | Longest wait between two retries. |
| `llm.concurrency` | `4` | Not applied in this version. `sdd summarize --all` takes its worker count from `--concurrency`, which defaults to 4. |
| `llm.debug` | `false` | `true` writes one transcript per call into `.sdd/tmp/llm-debug/`: the purpose, provider, model, duration and tokens, both prompt parts, the response schema and the raw response. Transcripts hold graph content, so sdd ignores the key in the project file. `sdd serve` reads it at start. |
| `llm.context_tokens` | What the provider reports | The model's context window in tokens, which sizes the prose draft verifier's calls. sdd asks Anthropic, an OpenAI-compatible models listing (`context_length` or `max_model_len`) or Ollama when unset, and for `claude-cli` reads the window one short `claude -p` call reports, once per process and model; set it where the provider reports none, such as `codex`. |

sdd retries a call that failed with a temporary error, such as a rate limit or a dropped connection, and waits as long as the provider asks when it names a time. `claude-cli` and `codex` ignore the rate limit and retry keys, since the `claude` and `codex` commands retry on their own.

When a checker's response does not parse, sdd makes one more call carrying the unparseable text and asking for the JSON alone, so a model that writes its conclusion into the `severity` field does not cost you the capture. A response that parses costs one call. `llm.extract_timeout` bounds the second call separately from `llm.timeout`, so a slipping endpoint cannot spend twice the configured timeout on one check. Both responses failing is an error naming both attempts.

Default rate limits:

| Provider | Model name contains | Requests per second |
| --- | --- | --- |
| `anthropic` | `opus` | 0.5 |
| `anthropic` | `haiku` | 2 |
| `anthropic` | anything else | 1 |
| `openai` | `mini` or `nano` | 2 |
| `openai` | anything else | 1 |
| `ollama`, `claude-cli`, `codex` | any | no limit |

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

### Verify

| Key | File | Default | Meaning |
| --- | --- | --- | --- |
| `verify.mode` | User-global, project | `prose` | How carefully a capture's draft is checked against the graph before it is written. `off`: pre-flight checks the draft against the entries it references. `grounding`: the capture first reads the related entries, with no check after. `prose`: grounding, then pre-flight runs over the grounding reading and adds the checks for duplicates, outdated claims, clashes between entries and set-aside entries that bear on the draft. `deep`: grounding, then a verifier agent judges in place of pre-flight's model. The project file wins over the user-global one, and `config.local.yaml` may only raise it, in the order `off`, `grounding`, `prose`, `deep`. |
| `verify.skip` | User-global, project | `failure` | When the deep exchange may be skipped: `never`, `failure` or `weighed`. `config.local.yaml` may only tighten it toward `never`. |
| `verify.runner` | User-global, project, local | `subagent` | What runs the `deep` verifier. `subagent`: the drafting agent's harness runs a fresh-context subagent, the `sdd_delegate` subagent `sdd init` installs. `claude` and `codex`: sdd runs `claude -p` or `codex exec` headless in the project checkout, read-only, with sdd as its only MCP server, and resumes the same session each round. `driver`: sdd runs `verify.driver` each round. sdd runs every round in the background, bounded to 30 minutes: the call that launches it returns at once, and the capture's next call picks the findings up, or fails with the round's error. The setting describes the machine, so `config.local.yaml` wins. |
| `verify.driver.command`, `verify.driver.args` | User-global, local | None | The command the `driver` runner runs each round. sdd refuses it in the committed config, since it would run on every contributor's machine. |
| `verify.claude.model`, `verify.claude.effort` | User-global, project, local | `sonnet`, `medium` | The model and effort of a `deep` verifier in Claude: the `claude` runner passes them to `--model` and `--effort` (`low`, `medium`, `high`, `xhigh` or `max`), and a `subagent` dispatch names them when the MCP client is Claude Code. The setting describes the machine, so `config.local.yaml` wins. |
| `verify.codex.model`, `verify.codex.effort` | User-global, project, local | None, `medium` | The same for Codex: the `codex` runner passes them to `-m` and `model_reasoning_effort`, and a `subagent` dispatch names them when the MCP client is Codex. Unset, the model is codex's own default. |
| `verify.prefilter` | User-global, project, local | `true` | Rates the relevance of each listed entry before the reading judges it, and sets aside the ones below the calibrated threshold. Without an `llm_prefilter` block it runs the `generate` protocol on the `llm` provider and model at effort `low`; a value `llm_prefilter` sets wins. On `claude-cli` or `codex` each rated entry starts a process of its own. |

A prose verifier call fills half the context window left after the answer reserve (`llm.max_tokens`), counting 3 characters a token. A reading too large for one call is split into several calls, related entries kept together, and an entry too large on its own is sent as whole paragraphs with markers where text is left out. `sdd init` asks how carefully drafts are checked and writes `verify.mode`.

`deep` runs grounding and pre-flight's mechanical checks, then a tool-using verifier agent in place of pre-flight's model. The verifier, in a child session, reads the draft, the rules pre-flight's model would have applied and the grounding reading, checks claims against the code and its history, and exchanges findings with the drafting agent for at most 3 rounds. sdd turns the findings into edits of the draft and shows them as a diff; findings the agents settle are applied, and findings left open go to the user. A capture is written only under a deep verdict for its exact draft. A skipped exchange hands the draft to pre-flight's model before the write, writes `verification: skipped` into the entry and lowers its confidence: to `low` after a verifier failure, and at most `medium` for a skip weighed as safe. Where the harness has no subagent tool, sdd falls back to the headless runner of the first agent `supported_agents` names whose CLI it finds. With `deep` and no runner set, `sdd init` also asks what runs the verifier and writes the answer to `config.local.yaml`. Model and effort values pass to the CLI or the subagent tool as written: sdd keeps no table translating model names between harnesses, so a subagent dispatch in a client other than Claude Code or Codex names neither.

A `driver` command reads one JSON request on stdin each round, `{"round": 1, "session": "<child session>", "instance": "<child instance>", "prompt": "<round prompt>", "driver_state": "<last round's state>"}`, starts or continues its agent with the prompt, and prints `{"driver_state": "<state for the next round>"}` once the round ends. The agent reports through the child session's `next` calls, never through the driver. A nonzero exit or a round longer than 30 minutes is a verifier failure.

In a guided dialogue the writing guide runs once on the content the user approved, then the verifier; a later adjust gets a check of the changed lines only. In an autonomous dialogue both run before playback.

### Sync and sessions

| Key | Default | Meaning |
| --- | --- | --- |
| `sync.default.fetch_cooldown` | `5m` | Least time between two fetches from one remote. A value below `1m` is ignored. |
| `sync.default.push_cooldown` | `30s` | Least time between two pushes to one remote. A value below `10s` is ignored. |
| `sync.default.retries` | `2` | Retries of a git network operation that failed. |
| `sync.default.concurrency` | `2` | Git network operations that run at the same time against one host. |
| `sync.default.ssh.per_hour` | `600` | SSH operations per hour against one host. |
| `sync.default.https.per_hour` | `600` | HTTPS operations per hour against one host, with credentials. |
| `sync.default.https.per_hour_unauthenticated` | `60` | HTTPS operations per hour against one host, without credentials. |
| `sync.hosts.<host>.<limit>` | The `sync.default` value | The same limits for one remote host, such as `github.com`. |
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

sdd keeps a clone of each connected repository under `~/.local/share/sdd/repos/<repo_id>`. A read that needs it runs the clone's sync round once the fetch cooldown of the [git limits](#sync-and-sessions) has passed. The [connected repositories guide](/sdd/guides/connected-repositories/#a-connected-graph-is-what-was-pushed) describes the round.

## Language model providers

sdd calls the language model three times when it records an entry:

- The writing check reads the draft and points out unclear text.
- The pre-flight check compares the entry with the graph.
- The summary step writes the entry's one-line summary.

:::caution[The model must support structured output]
The writing check and the pre-flight check ask the model for a fixed response format, a JSON schema. By default sdd sends the schema through each provider's structured output feature; for a model that cannot take it, the [`json_object` mode](#structured-output-modes) asks for any JSON object and checks the answer against the schema itself. sdd never passes on an answer that does not match the schema, so a model that answers in neither form fails the checks. Ollama Cloud ignores the schema and does not work. The summary is plain text and works with any model.
:::

| Provider | Sends requests to | Schema sent as | API key |
| --- | --- | --- | --- |
| `anthropic` | The Anthropic Messages API, or `llm.endpoint` | `output_config.format` | `llm.api_keys.anthropic` |
| `openai` | OpenAI chat completions, or any compatible service at `llm.endpoint` | `response_format` of type `json_schema`, strict | `llm.api_keys.openai` |
| `ollama` | The Ollama server at `llm.ollama_endpoint` | The `format` field | Optional |
| `claude-cli` | The local `claude` command, run as `claude -p` | `--json-schema` | None, uses your Claude Code login |
| `codex` | The local `codex` command, run as `codex exec` | `--output-schema` | None, uses your Codex login |

A gateway or compatible service must honor the schema field in the third column for the checks to work, unless `llm.structured_output` is `json_object`.

### Structured output modes

`llm.structured_output` picks how the writing check and the pre-flight check ask for JSON:

- `json_schema` sends the schema for the provider to enforce.
- `json_object` asks for any JSON object (`response_format: {"type": "json_object"}` on `openai`, `format: json` on `ollama`), states the schema in the system prompt and validates the answer against it locally. An answer that is not one JSON object matching the schema is never passed on: the check sends it to the reformat call described under [language model](#language-model), whose answer is validated the same way.
- `auto`, the default, sends `json_schema`. When the endpoint answers 400 because the model does not support JSON schema mode (code `model_not_capable`, or a message naming `json_schema` or `json_object`), the call is retried once in `json_object` mode, and that endpoint and model stay on `json_object` for the rest of the process. Any other 400 fails the call. The retry runs inside the call's `llm.timeout` (or `llm.extract_timeout`).

Some OpenAI-compatible services serve a model in `json_object` mode only, while another service accepts `json_schema` for the same model. `claude-cli` and `codex` ignore the setting.

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

The `claude-cli` provider runs each call with `--safe-mode`, in a working directory of its own, and without `ANTHROPIC_API_KEY`, `ANTHROPIC_AUTH_TOKEN`, `ANTHROPIC_BASE_URL`, `CLAUDE_CODE_USE_BEDROCK` or `CLAUDE_CODE_USE_VERTEX` in its environment. Started inside a project tree the CLI would otherwise load that project's instruction files, skills, hooks and configured servers into a prompt that needs none of them, and an inherited key would answer on an API balance instead of the signed-in account. Authenticating by key belongs on `provider: anthropic`, which sends the key deliberately.

### Codex CLI

`codex` runs every call through `codex exec` with your Codex login, so a person who uses Codex alone needs no Claude login and no API key, only the `codex` command on your `PATH`. Like `claude-cli`, each call starts a new process. Without `llm.model`, codex runs its own default model.

```bash
sdd config set llm.provider codex
sdd config set llm.effort low
```

```yaml
llm:
  provider: codex
  effort: low
```

The `codex` provider runs each call with `--json`, a read-only sandbox, `--ephemeral`, `--ignore-user-config`, `--ignore-rules` and `project_doc_max_bytes=0`, in a working directory of its own. So no project's `AGENTS.md`, no MCP servers or profiles from `config.toml` and no execpolicy rules reach a prompt that needs none of them, and no session is kept. Your login still comes from `CODEX_HOME`.

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

The index keeps every embedded version of every entry until the cache prune removes it, so sdd versions that share it do not remove each other's work. `sdd index gc` lists the stored versions by group, `sdd index gc --drop <group>` deletes one group, and `--drop stale` deletes every version this checkout does not search.

Once a day sdd starts `sdd cache prune` in the background, which deletes what nothing used for `cache.retention` (user-global config, default `90d`): replaced index versions, unused index stores, graph snapshots of removed folders, clones of disconnected repos. It never delays a command; a failed run is reported once, and `sdd info` shows the last one.

## Hooks

A hook is a script sdd runs at an event of a running procedure, such as a prose linter on a capture draft. It reads the event as JSON on stdin and may answer with findings, which join the step's own.

| Key | Default | Meaning |
| --- | --- | --- |
| `event` | Required | The event, below. |
| `command` | Required | A file in the layer's hooks folder. Escape a space in its name with a backslash. |
| `args` | None | Arguments passed as they stand, without a shell. |
| `files` | None | Paths or globs whose contents the hook's hash covers, such as a linter's configuration. A folder covers every file below it. |
| `timeout` | `600` | Seconds before sdd stops the hook and its child processes and fails the call. |
| `findings` | `false` | The hook may answer with findings. Only an event with a findings channel accepts it. |

### Where hooks live

Every config file may declare `hooks`: the user-global file, and both `.sdd/config.yaml` and `.sdd/config.local.yaml` of the project. In a project whose graph lives in a [repository of its own](/sdd/guides/graph-repository/), the work repository's two files declare hooks as well. sdd runs the hooks of every file together.

`command` names a file in the declaring layer's hooks folder:

| Declared in | Hooks folder |
| --- | --- |
| User-global config | `~/.config/sdd/hooks/` |
| `.sdd/config.yaml` | `.sdd/hooks/`, committed with the project |
| `.sdd/config.local.yaml` | `.sdd/hooks/`, its scripts in `local/` there, which `sdd init` adds to `.sdd/.gitignore` |

sdd refuses an absolute path, `..`, a name it would have to look up on `PATH`, and anything a shell would interpret. A hook runs without a shell, with the hooks folder as its working directory, so `args` and `files` resolve from there too. Put an interpreter in the script's shebang and make the script executable. A hook in the committed file may not run a script from `local/`, which only your machine has.

### Confirming a hook

A hook from a `.sdd/` file runs only after you confirm it on your machine. Hooks from the user-global file need no confirmation.

sdd hashes each hook on every run: its event, command, arguments, timeout, findings and files, the contents of its script, of every argument naming a file, and of every file `files` matches. Any change asks again. Scripts a hook loads without listing them in `files` change unnoticed.

- `sdd hooks` lists every hook as `confirmed`, `pending`, or `changed` since you confirmed it.
- `sdd hooks trust` shows each pending hook with its event, command, arguments, the hashed files with their SHA-256 digests and the first 20 lines of its script, and asks y/N. It runs only in a terminal: you confirm each hook yourself, so without a terminal it confirms nothing and fails.
- `sdd init` offers the pending hooks when it runs in a terminal, and otherwise names how many wait.
- Over MCP, every call that works on a session or opens one first checks all of the project's hooks. Before it does anything else, it shows you the prompt for each unconfirmed hook at once through your client, and goes on only once you accept them all. A decline, or a client that cannot ask, fails the call with nothing done and names `sdd hooks trust`. A hook that changes while the call runs fails the call the same way.

Confirmations live in `~/.local/state/sdd/hooks/<repository>.json`, readable only by you, keyed by the project's `repo_id`, so clones of one repository share them. The daily cache pruning drops the confirmations of hooks no config declares any more and of checkouts gone from disk.

### Events

| Event | Fires | Accepts findings |
| --- | --- | --- |
| `capture.assemble` | The draft is complete, before any paid call | Yes |
| `capture.guide` | The writing guide is done | Yes |
| `capture.preflight` | Pre-flight is done | Yes |
| `capture.verify` | The draft verifier is done, where the project runs it | Yes |
| `capture.verify.round` | After each verifier round; the payload's `verify.round` holds its number | No |
| `entry.written` | A capture wrote its entry | No |

Every step of every procedure raises `<procedure>.<step>` when it is done, also named `<procedure>.<step>.done`, and `<procedure>.<step>.start` when the procedure arrives at it. `*` stands for any procedure or step: `*.*` matches every step being done, `*.*.start` every arrival. Only the events in the table are supported; the others follow the procedures' steps and may change with them.

A failing `.start` hook holds its step: sdd runs it again on every call, and the step's work, such as the pre-flight call, waits until it passes.

### Running a hook

Hooks on one event run at once. A hook fails the call when it exits nonzero, outlives its timeout, or writes anything to stdout but nothing or `{"findings": [...]}`. A high finding holds the step like one of the step's own.

The payload carries `event`, `time`, `project`, `branch`, `session`, `procedure`, `instance`, `step` and `phase` (`start`, `done` or `round`); `transition`, the step the procedure moves to, when a step is done; `findings`, the step's findings so far, on a step with a findings channel; `draft` during a capture, with `type`, `kind`, `layer`, `refs`, `topics`, `body`, `attachments` and `body_hash`; and `entry`, the written entry's ID, on `entry.written`. A finding has `severity` (`high`, `medium` or `low`), `category` and `observation`.

A Vale check on every capture draft:

```yaml
# .sdd/config.yaml
hooks:
  - event: capture.assemble
    command: vale.sh
    files: [.vale.ini, styles]
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
| `XDG_CACHE_HOME` | `~/.cache` | Search indexes live under `$XDG_CACHE_HOME/sdd`. |
| `XDG_DATA_HOME` | `~/.local/share` | Clones of connected repositories live under `$XDG_DATA_HOME/sdd/repos`. |
| `XDG_STATE_HOME` | `~/.local/state` | Session logs and hook confirmations live under `$XDG_STATE_HOME/sdd`. Must be an absolute path. |
| `SDD_GRAPH_CACHE` | On | `off` reads every graph from its files. Otherwise the CLI keeps each graph's entry files under `$XDG_CACHE_HOME/sdd` and reads them back only while every graph file's size, timestamps and inode match what they were when it was saved, so a command on an unchanged graph reads one file instead of every entry. |
| `SDD_SERVE_TOKEN` | None | The bearer token `sdd serve --transport http` requires on every request. Same as `--auth-token`. |
| `SDD_USAGE_LOG` | None | A file path. Every model call, pre-flight and embedding alike, appends one JSON line with its step, provider, endpoint host, model, token counts, finish reason, the provider's `billing_cost` object where the reply carries one, and whether it succeeded. Keys, prompts and answers never reach it. The record shape is `llmstats.UsageRecord` in `internal/llmstats`. |
