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
| Local | `.sdd/config.local.yaml` | You, in this checkout only. `sdd init` adds it to `.sdd/.gitignore` and leaves the `.gitignore` at the repository root as it is. |

A later row overrides an earlier one: local wins over project, project wins over user-global, and user-global wins over the built-in default. A file sets only the keys it names and inherits the rest. Maps merge entry by entry, so `llm.api_keys.openai` in the local file keeps `llm.api_keys.anthropic` from the user-global file. A list replaces the whole list below it. Command-line flags such as `--model` or `--embedding-provider` override every file for that one command.

The global `--local-config <path>` flag points the local layer at another file for the whole command: reads resolve it, and `--local` writes land in it. A sandbox, a CI runner or a second identity can so supply its own local layer without touching the checkout. The file must exist: a missing file fails the command, and `sdd config set --local` creates it. A file inside a git repository must be ignored there, so no commit can carry it.

```bash
sdd --local-config ~/ci-local.yaml config get participant
```

### Which keys go where

| Keys | Belong in | Notes |
| --- | --- | --- |
| `participant`, `llm.*`, `embedding.*` | User-global | Any file works. Use the local file to override a value for one project. A value in the project file applies to everyone on the project and overrides their user-global value. |
| `sessions.retention`, `cache.retention`, `index.replaced_retention` | User-global | The [daily sweep](#the-daily-sweep) reads them from the user-global file only. |
| `sync.default.*`, `sync.hosts.*` | User-global | The git limits. sdd reads them from the user-global file only. |
| `secrets.rules_update` | User-global | Every scan on the machine shares one rule file. sdd reads it from the user-global file only. |
| `dialogue.mode` | User-global or local | Set per person. sdd ignores it in the project file. |
| `titles.backfill` | Any file | The project file switches it on for everyone on the project. The local file overrides that for one checkout. |
| `graph_dir`, `default_branch`, `repo_id`, `language`, `skill_scope`, `supported_agents`, `dependencies` | Project | These describe the repository. sdd ignores them in the user-global file. |
| `repos` | User-global | sdd ignores it in the project files. |
| `hooks` | Any file | The lists of all files run together. A hook from a project file runs only once you [allow it](#allowing-a-repositorys-hooks). |

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
- `set` and `unset` run on a file whose only fault is a value sdd refuses to load, such as a `dialogue.mode` outside `guided`, `auto` and `assisted`, so you repair it through them. A file that does not parse still fails them.
- No command writes `.sdd/config.yaml` directly. `sdd init` creates it, `sdd repo add` and `sdd repo remove` change `dependencies`, and you edit the rest by hand and commit it.

:::note[A running server follows config changes]
Your agent starts the sdd server, `sdd serve`. Before each tool call it checks the config files. After a change it answers with the new configuration and names the settings that changed; a file that fails to load refuses every call until it is fixed. A call already running finishes under the configuration it started with. This includes the `llm` settings: the next call goes to the language model they name. sdd commands in a terminal read the configuration on every run.
:::

## Keys

Durations use the units `s`, `m` and `h`, such as `90s`, `5m` or `1h30m`.

### Project

`sdd init` writes these keys to `.sdd/config.yaml`.

| Key | Default | Meaning |
| --- | --- | --- |
| `graph_dir` | `.sdd/graph` | Directory of the entry files, relative to the repository root, or an absolute path. It may lead [out of the repository](/sdd/guides/graph-repository/#a-graph-folder-outside-the-repository). Ignored in a file that sets `graph_repo`. |
| `default_branch` | The branch checked out during `sdd init` | A session that names no branch works on the branch checked out in the project directory, and on this branch when none is checked out. `sdd serve` does not start without it. |
| `repo_id` | Derived from the `origin` remote | The project's identity, such as `github.com/acme/platform`. Other graphs reference an entry here as `<repo_id>:<entry-id>`. Empty for a repository without a remote. Do not change it by hand. |
| `language` | English | Language of the entry text, as a locale code such as `en`, `de` or `de-DE`. Translated SDD vocabulary ships for `de`. |
| `skill_scope` | Asked by `sdd init` | `project` installs the skill into the repository, such as `.claude/skills/`. `user` installs it into your home directory, such as `~/.claude/skills/`. |
| `supported_agents` | `[claude]` | The agents `sdd init` installs the skill for: `claude`, `codex` or both. |
| `dependencies` | None | The `repo_id`s of other graphs this graph references. See [connected repositories](#connected-repositories). |
| `graph_repo` | None | Set in a code repository only: the `repo_id` of the repository holding its graph, which sdd reads and writes through a clone of its own. See [a graph in its own repository](/sdd/guides/graph-repository/). |

### Participant

| Key | Default | Meaning |
| --- | --- | --- |
| `participant` | None | Your name, recorded as the author of the entries you capture. `sdd init` asks for it, offers your git `user.name`, and writes it to `.sdd/config.local.yaml` unless another file already sets it. Every command warns while it is missing. |

### Dialogue

| Key | Default | Meaning |
| --- | --- | --- |
| `dialogue.mode` | `guided` | Who takes the choices an engine session would put to you. `guided`: you confirm each step. No entry asks you for a reason. A reason you give, or one the agent proposed in the draft you confirm, is written on the entry with `reason-by` naming you, and a directive's intent with `intent-by` naming you. `auto`: your agent takes them, for a headless job too. Where a check blocks a capture, the agent revises the draft or retries the check once, and never skips, settles or overrides it; sdd refuses those answers, and words relayed as yours, at that step. A second block at the same step [parks the capture](/sdd/guides/working-with-coding-agents/#parked-captures). Every entry the agent writes in `auto` is marked `decided-by: agent`, with you as `run-by`, the actor of the MCP client that started the capture as participant, and the agent's own `reason` on a decision where it inferred one from the dialogue. A client that sends no name cannot capture in `auto`, and a capture from a client no actor names is held until the agent captures that actor ([decisions an agent takes alone](/sdd/guides/working-with-coding-agents/#decisions-an-agent-takes-alone)). A draft made in `auto` and then confirmed or written in `guided` is written as yours, its reason and intent with it. An agent alone writes signals, and plans, activities and pending directives on the tactical and operational layers; [any other draft parks for you](/sdd/guides/working-with-coding-agents/#decisions-an-agent-takes-alone). `assisted`: your agent takes them as in `auto`, and each entry's checked draft is played back to you once; your approval writes it as yours, marked `approved-by` you, with `reason-by` and `intent-by` naming you: the agent infers a directive's intent, and may infer a reason, as in `auto`, the playback shows them, and your approval adopts them as yours, with no separate question; to change them, you adjust the draft. Several sealed drafts can be approved in one answer, a draft you never answer stays parked at playback, and every `assisted` session opens by naming the drafts other sessions left waiting for you ([entries you approve](/sdd/guides/working-with-coding-agents/#entries-you-approve)). sdd reads it from the user-global file and the machine-local file, `.sdd/config.local.yaml` or the `--local-config` file, only; in `.sdd/config.yaml` it is ignored with a warning. `sdd init` asks for it, or takes `--dialogue-mode`, and writes it to the local file. Any other value in a file sdd reads it from fails every command but `sdd config set` and `unset` with an error naming the file, the value and the valid values. |

### Titles

| Key | Default | Meaning |
| --- | --- | --- |
| `titles.backfill` | `false` | `true` lets sdd fill the [missing titles](/sdd/reference/graph-schema/) of entries on its own: when a session starts, on a session's calls at most every 15 minutes, with each sync round of `sdd serve`, and after `sdd sync` and `sdd sync --pull`. Each run titles the 10 newest untitled entries with one model call per entry and commits them as `sdd: summarize N entries`. `false` makes none of these call a model or commit for titles. A capture still titles its entry, and `sdd summarize` still fills titles on request. |

### Language model

| Key | Default | Meaning |
| --- | --- | --- |
| `llm.provider` | `claude-cli` | `anthropic`, `openai`, `ollama`, `claude-cli` or `codex`. See [language model providers](#language-model-providers). |
| `llm.model` | `claude-sonnet-4-6`, none for `codex` | Model name as the provider spells it. sdd uses the default with any provider but `codex`, so set a model whenever you change to another provider. Unset under `codex`, codex runs its own default model. |
| `llm.effort` | `medium` for `claude-cli`, `codex` and `anthropic`; `low` for `openai` and `ollama` | Reasoning effort, passed as written: to `claude-cli` as `--effort`, to `codex` as `model_reasoning_effort`, to `anthropic` as `output_config.effort`, to `openai` as `reasoning_effort` and to `ollama` as `think`. sdd sends it only to models whose API takes it: on `anthropic` Claude Opus 4.5 and later Opus models, Sonnet 4.6 and later, Haiku 5.5 and later, and the Fable and Mythos models; on `openai` the o-series and GPT-5 and later, but not `-chat` models or `o1-mini` and `o1-preview`; on `ollama` the `gpt-oss` models, which take thinking levels. Other models run at their own default. A value for the same field in `llm.params` wins. |
| `llm.api_keys.<provider>` | None | API key for a provider. Required for `anthropic` and `openai`. Optional for `ollama`, which receives it as a bearer token. |
| `llm.endpoint` | The vendor's public API | Base URL of a gateway or compatible service, for `openai` and `anthropic` only. Accepts the host, a URL ending in `/v1`, or the full URL ending in `/chat/completions` for `openai` or `/messages` for `anthropic`. |
| `llm.ollama_endpoint` | `http://localhost:11434` | Ollama server for the `ollama` provider. |
| `llm.timeout` | `2m` | Time limit for one model call. A pre-flight call past it leaves the draft unjudged, and the capture puts the timeout to you: send the call again, revise the draft, skip the check where `verify.skip` allows it, or drop the capture. |
| `llm.extract_timeout` | `1m` | Time limit for the reformat call that follows a check response that does not parse. |
| `llm.max_tokens` | `4096` | Output cap of one call to `anthropic`, `openai` or `ollama`. A reasoning model spends its thinking against the cap, so it needs a higher one. `0` sends no cap, except on `anthropic`, whose API requires one and gets 32000. A pre-flight answer cut at the cap leaves the draft unjudged, and the capture puts the cut to you instead of sending the same request again. |
| `llm.params.<name>` | None | Provider settings that sdd adds unchanged to every request body, such as `reasoning_effort` set to `low`. `claude-cli` and `codex` ignore them. |
| `llm.preflight.params.<name>` | None | Provider settings for the pre-flight calls alone, the prose verifier's and their reformat call included, laid over `llm.params` key by key, such as `reasoning_effort` set to `medium` while other calls keep `high`. Unset, pre-flight takes `llm.params` as they stand. |
| `llm.structured_output` | `auto` | How the checks ask `openai` and `ollama` for JSON: `json_schema`, `json_object` or `auto`. See [structured output modes](#structured-output-modes). |
| `llm.rate_limit_rps` | See below | Maximum requests per second to `anthropic` or `openai`. Raise it on a higher account tier or for a gateway that allows more. |
| `llm.retry.max_attempts` | `5` | Attempts per call, counting the first. `1` turns retries off. |
| `llm.retry.base_delay` | `2s` | Wait before the first retry. The wait grows with each further retry. |
| `llm.retry.max_delay` | `60s` | Longest wait between two retries. |
| `llm.claude_cli.credentials` | `login` | What `claude-cli` authenticates with. `login` uses your Claude Code login: `claude -p` gets no `ANTHROPIC_*` or `CLAUDE_*` variable but the login's own and a few TLS and path ones, and no user, project or local settings file. `env` passes the environment and your settings, so `claude -p` can run on an Anthropic API key or another provider such as Bedrock or Vertex. A `chat` judge on `claude-cli` takes the value unless `llm_prefilter` sets its own. See [Claude Code CLI](#claude-code-cli). |
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

### Ranking judge

The `llm_prefilter` block configures the one judge that rates how relevant each listed entry is to a draft, for gather's prefilter and the `deep` verifier's reading alike. It takes the keys of the `llm` block, plus `llm_prefilter.protocol`.

| Key | Default | Meaning |
| --- | --- | --- |
| `llm_prefilter.protocol` | `chat` | `chat`, `cohere` or `systemone`. `chat` has a chat model score up to 100 entries a call, by kind, title and summary, 8 calls at once. In a gather with a [far floor](#the-far-floor) three of a call's entries are its far ones. A call whose answer breaks, lacks a score or runs past `llm_prefilter.timeout` is sent again in halves, down to one entry a call beside the far ones, and sdd remembers the half per model in `~/.local/state/sdd/relevance/chat-batch-sizes.json`, so the next run starts there. A request whose calls all answer, one of them at the remembered size, doubles that size, up to 100, so a failure that passed does not keep the calls small; a size that breaks again is halved again. `cohere` and `systemone` are rerankers reached with `llm_prefilter.api_keys.<provider>`, Opper unless `llm_prefilter.provider` names another. sdd reads `generate` and `listwise`, which an older version wrote, as `chat`, and `cohere-rerank` as `cohere`. |
| `llm_prefilter.timeout` | `45s` | Time limit for one scoring call. |

Without an `llm_prefilter` block the judge depends on the client. A session whose MCP client is a Claude host ranks with Haiku 5.5 (`claude-haiku-5-5`) through the claude CLI at effort `low`, on the `llm.claude_cli.credentials` of the `llm` block; Haiku 5.5 cannot switch its thinking off, and `low` is its lowest effort. Any other client ranks with `chat` on the `llm` provider and model at effort `low`. A command without a client, such as `sdd grounding status` or `sdd grounding update`, takes the Claude host's judge when it finds `claude` on `PATH`, else `chat` on the `llm` provider. Any `llm_prefilter` value replaces both: the configured judge ranks, whatever the client, each value it sets winning over the `llm` block's. A judge is named by its protocol and model, such as `chat:claude-haiku-5-5`; scores and labels stored under a former name, such as `listwise:claude-haiku-5-5`, count for the name it became.

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
| `verify.mode` | User-global, project | `prose` | How carefully a capture's draft is checked against the graph before it is written. `off`: pre-flight checks the draft against the entries it references. `grounding`: the capture first reads the related entries, with no check after. `prose`: grounding, then pre-flight runs over the grounding reading and adds the checks for duplicates, outdated claims, clashes between entries and set-aside entries that bear on the draft. `deep`: grounding, then a verifier agent judges in place of pre-flight's model. `deep` needs Claude Code: under `deep`, or with a kind set to `deep` in `verify.kinds`, a session from Codex fails to start or resume and names the setting, as does a Codex capture into a connected repository whose own config checks it `deep`. The project file wins over the user-global one, and `config.local.yaml` may only raise it, in the order `off`, `grounding`, `prose`, `deep`; a read ignores a lower local value with a warning. |
| `verify.kinds.<kind>` | User-global, project, local | None | The mode for drafts of one entry kind, for example `verify.kinds.done: prose`. A named kind gets exactly its mode, stricter or looser than `verify.mode`; an unnamed kind gets `verify.mode`. While no kind is named, `done`, `actor`, `role`, `focus`, `annotation`, `insight`, `question`, `activity` and `procedure` are checked at most at `prose`. Kinds merge one by one across the layers, and `config.local.yaml` may only raise each kind's mode; a read ignores a lower local value with a warning. |
| `verify.skip` | User-global, project | `failure` | When the deep exchange, or a pre-flight answer cut at `llm.max_tokens` or past `llm.timeout`, may be skipped: `never`, `failure` or `weighed`. In any `verify.mode`, `failure` and `weighed` let you skip a cut answer or a timeout, which writes `verification: skipped` and confidence `low`; `never` holds the write until pre-flight's model judges the draft. `config.local.yaml` may only tighten it toward `never`; a read ignores a looser local value with a warning. |
| `verify.runner` | User-global, project, local | `subagent` | What runs the `deep` verifier. `subagent`: the drafting agent's harness runs a fresh-context subagent, the `sdd_delegate` subagent `sdd init` installs. `claude`: sdd runs `claude -p` headless in the project checkout, read-only, with the web and sdd as its only MCP server, whose tools run without asking for approval, and resumes the same session each round. `driver`: sdd runs `verify.driver` each round. sdd runs every round in a detached `sdd` process of its own, bounded by `verify.round_timeout`: the call that launches it returns at once, the round keeps running when the `sdd serve` that launched it exits, the round's process records the outcome with the verifier's session, and the capture's next call, through any server, picks the findings up. A round starts once the round before it recorded its outcome. The drafting agent gets no handle or prompt of a verifier sdd launched: the verifier works on its own while the drafting agent waits, each check served as one line that says to call next again, at most every 20 seconds, and never to wait with sleep or a timer. A round that fails, or ends before its verifier delivered, is a verifier failure: the waiting capture puts it to the user, naming the round's error or the agent's last message. So is a round that recorded no outcome while its process ended, or past its timeout, and a round whose process never started: sdd records a round's launch before it starts the round's process, and a round whose process is not recorded as running a minute after its launch fails. The capture names that the round's process ended without recording an outcome, that the round ran past `verify.round_timeout`, or that the round's process never started, together with the round's log file. Each round's process writes its output and its log to `$XDG_STATE_HOME/sdd/rounds/<repository>/<verifier session>-round-<n>.log` (default `~/.local/state/sdd/rounds/...`), keyed like the relevance ledger; the daily cache pruning removes round logs unchanged for `cache.retention`. Abandoning the capture or ending the verifier's session stops a running round's process, with the agent CLI it started. When a round's process ended without an outcome while the agent CLI it started still runs, failing the round or ending the verifier's session stops that CLI with every process left in the round's process group. The value `codex` no longer exists: a config that sets it, or a `verify.codex` block, fails to load, naming the file. The setting describes the machine, so `config.local.yaml` wins. |
| `verify.round_timeout` | User-global, project, local | `10m` | How long one round of a verifier sdd launched may run, as a duration such as `10m` or `90s`, counted from its launch or from the end of the round before it, whichever is later. Past it the round's process stops the runner and records the round as failed, so the waiting capture puts the failure to the user. A round with no outcome a minute past its timeout fails the same way: the capture's next call stops its process and names the timeout and the round's log file. The setting describes the machine, so `config.local.yaml` wins. |
| `verify.idle_timeout` | User-global, project, local | `10m` | How long a capture waiting on its verifier lets the verifier's session go without a call, as a duration such as `10m`. Past it sdd ends the verifier's session, and the capture puts the failure to you, naming the bound. A drafting agent whose subagent ended without delivering reports so at the wait, with the same result. The setting describes the machine, so `config.local.yaml` wins. |
| `verify.driver.command`, `verify.driver.args` | User-global, local | None | The command the `driver` runner runs each round. sdd ignores it in the committed config with a warning, and `sdd config set` refuses it there, since it would run on every contributor's machine. |
| `verify.claude.model`, `verify.claude.effort` | User-global, project, local | `sonnet`, `medium` | The model and effort of a `deep` verifier in Claude: the `claude` runner passes them to `--model` and `--effort` (`low`, `medium`, `high`, `xhigh` or `max`), and a `subagent` dispatch names them when the MCP client is Claude Code. The setting describes the machine, so `config.local.yaml` wins. |
| `verify.prefilter` | User-global, project, local | `true` | Rates the relevance of each listed entry before the reading judges it, and sets aside the ones below the calibrated threshold and the ones below the [far floor](#the-far-floor). The [ranking judge](#ranking-judge) rates them, and the `deep` verifier's reading ranks with the same judge and threshold: see [Ranking the deep reading](#ranking-the-deep-reading). |
| `verify.auto_calibrate` | User-global, project, local | `true` | Lets `sdd serve` calibrate the ranking judge in the background: it scores the labelled pairs the judge has not scored until the judge holds the top recall stage. This spends model calls, or search units on a reranker. `false` leaves calibration to `sdd grounding update`. See [Background calibration](#background-calibration). |
| `verify.ranking_timeout` | User-global, project, local | `45s` | How long the `deep` verifier's reading waits for its ranking judge, as a duration such as `45s`, counted from the start of the ranking pass. Past it the entries the judge left unscored rank by similarity to the draft. See [Ranking the deep reading](#ranking-the-deep-reading). |

A prose verifier call fills half the context window left after the answer reserve (`llm.max_tokens`), counting 3 characters a token. A reading too large for one call is split into several calls, related entries kept together, and an entry too large on its own is sent as whole paragraphs with markers where text is left out. `sdd init` asks how carefully drafts are checked and writes `verify.mode`.

The drafting agent picks the entry kind, so a draft whose kind is checked looser than `verify.mode` or than any kind named stricter in `verify.kinds` also gets a kind check: pre-flight tests whether the body answers its kind's question or that of a kind checked more strictly, and a misfit blocks the write until the draft is relabelled or you override it.

A `verify.mode`, `verify.skip`, `verify.runner`, `verify.claude.effort` or `verify.kinds` value outside its set, such as one a newer sdd version wrote, or a `verify.round_timeout`, `verify.idle_timeout` or `verify.ranking_timeout` that is no positive duration, is ignored: every command warns `ignoring config setting` with the key and the file, and the value of the layer below applies, or the default.

`deep` runs grounding and pre-flight's mechanical checks, then a tool-using verifier agent in place of pre-flight's model. The verifier, in a child session, reads the draft, the rules pre-flight's model would have applied without its answer format and severity scale, and the grounding reading, checks claims against the code and its history, and exchanges findings with the drafting agent for at most 3 rounds. Before a finding reaches the drafting agent, sdd checks what it cites: a passage it quotes must be in the draft, a cited file in the checkout and holding any quote given from it, a commit in the repository and an entry in the graph; a URL is checked for its form only, never fetched. Refused findings go back to the verifier for one repair; refused again, they end the exchange as a verifier failure. The reading serves in full, however many there are, the declared targets, the entries the drafting agent read in full, and every entry closing or superseding a target or closed or superseded by one; an entry given a verdict comes by its header and brief. The ranking judge ([Ranking the deep reading](#ranking-the-deep-reading)) ranks the rest of the reading against the draft, done entries last: the top 5 are served in full, where the judge scored them or every ranked entry fits the 5, and every other entry is listed one line each in one closing block, which the verifier opens with `show` where an entry bears on the draft. An entry one ref from a target and the next 100 ranked come by ID, title and first summary sentence, the rest by ID and title. At most 50 entries one ref from a target and 300 by title are listed, by rank within three groups: the implicitly dismissed, those below the prefilter's threshold, the others. A last line counts the rest, which the verifier reaches with `search` or `show`. This keeps the reading within a verifier's context: served whole, the reading of a large graph ran to nearly a million characters, while on a graph of about 1,800 entries a block served in full averages about 3,000 characters, a summary line about 330 and a title line about 60. A closed entry whose closer the reading also serves comes as its ID, kind, closer and brief alone. An entry the drafting agent was shown and left without a ref or verdict counts as implicitly dismissed, in the `prose` reading too, and a near-duplicate of the draft among these is a high finding. Once the prefilter is calibrated, its threshold replaces the count of 100: past the top 5, a ranked entry scoring at or above its set-aside threshold comes with its summary sentence, and one below it by title, in the group below the threshold; 3% of those below, drawn blind, rank among the others instead. The judge scores only the pairs the reading holds no score for, and only where the rank decides: once calibrated, or when more than 5 entries are ranked. Publish stores every score, so no pair is scored twice. A finding resting on such an entry, opened by the verifier, counts as a miss of the prefilter; an opening without one is counted apart, with no effect on the threshold. sdd turns the findings into edits of the draft and shows them as a diff; findings the agents settle are applied, and findings left open go to the user. Accepting the diff has sdd write it into the draft. An edit of yours after a verdict that only reverts edits of the diff to the text they replaced, or restores them, each where the diff applied it, counts as settled: neither the verifier nor the writing guide judges it again. Any other draft changed after a verdict goes to the same verifier as a revision, the changed passages and the entries read since, which it judges against the reading it holds, again for at most 3 rounds. A verifier sdd runs headless takes a revision the same way: its next round resumes the session its last round reported. A fresh verifier reads everything again only once the earlier one ended, after 3 revisions, or when a headless round reported no session to resume, and its dispatch says which. A capture is written only under a deep verdict for its exact draft. A skipped exchange hands the draft to pre-flight's model before the write, writes `verification: skipped` into the entry and lowers its confidence: to `low` after a verifier failure, and at most `medium` for a skip weighed as safe. Where pre-flight's model then answers past `llm.max_tokens` or `llm.timeout`, you may skip it too, and the entry is written at `low`. Where the harness has no subagent tool, sdd falls back to the headless `claude` runner where `supported_agents` names `claude` and sdd finds its CLI, and launches every later round and revision of that verifier the same way. With `deep` and no runner set, `sdd init` also asks what runs the verifier and writes the answer to `config.local.yaml`. Model and effort values pass to the CLI or the subagent tool as written: sdd keeps no table translating model names between harnesses, so a subagent dispatch in a client other than Claude Code names none.

`sdd grounding status` shows the ranking judge's calibration:

```
labels:  2,412 pairs scored by people's choices
         14 referenced · 1,280 dismissed implicitly (11 later flipped to bearing) · 0 set aside with a code
audit:   3% of the entries the threshold hides are shown to the verifier anyway, to catch a threshold that hides too much
         0 of the last 0 audited entries turned out referenced (window 200, needs 50 before it judges)
drift:   none · lowers the threshold when audited entries keep turning out relevant (more than 2% of the window)
floor:   on · sets aside what the judge rates no higher than entries far from the draft in the same call
         0 of the last 0 audited entries it set aside turned out referenced (window 200, off above 2% of at least 50)
judge:   chat:claude-haiku-5-5 (default on a Claude host; `llm_prefilter` would replace it)
         2,412 pairs unscored · no recall stage yet (bound 0.00) · stage 0.90 needs ~35 more referenced pairs · no threshold yet
auto:    on (verify.auto_calibrate): `sdd serve` scores unscored labelled pairs in the background until the top stage holds
         uncalibrated: run `sdd grounding update`
```

`labels` counts the labelled pairs whatever judge scored them; `audit`, `drift`, `floor` and `judge` read the judge a command without a client ranks with (see [Ranking judge](#ranking-judge)). `floor` says whether the [far floor](#the-far-floor) sets aside for that judge, and how its audited set-asides were answered. `auto` says whether a server calibrates that judge in the background, and while one does, since when, how many pairs it scored, the stage the judge holds and whether it waits for the server to idle. `--verbose` shows that judge's full calibration: the label weights, the labels by kind and source, the recall stage and the next one, the thresholds, ranking recall, the far floor, drift steps, how many hidden entries the verifier opened, and how the pairs served in full were answered, per reason code. Labels come from live captures or are replayed from the graph's references, as `sdd grounding update` seeds them. Calibration counts both sources; `sdd grounding status --exclude-replay` shows it over the live labels alone.

A judge's thresholds rest on recall stages: the Wilson lower bound (95%) of its weighted recall over the referenced labels at 0.90, 0.95 or 0.98. A judge that misses no reference holds them from 35, 73 and 189 referenced pairs it scored. The highest stage the judge holds sets its set-aside threshold, at the highest score that keeps the recall bound at that stage; below 0.90 the judge has no threshold, so only the [far floor](#the-far-floor) sets aside and nothing is served in full. Once the live labels alone hold a stage, the lower of two thresholds sets aside, the one over every label or the one over the live labels: a replayed reference is easier to find than an entry a live draft needs, so both sources together would prove more than live captures show. The serve-in-full threshold is the lowest score, never below the set-aside threshold, from which the Wilson lower bound (95%) of the referenced share of the live labels reaches 0.5. Replayed labels are references only, with no dismissal beside them, so they do not count for it. While the live labels reach that bound at no score, the judge serves nothing in full and those entries stay lines to judge. Where replayed labels take part in the calibration, `sdd grounding status` names the labels each threshold rests on, `(all labels)` or `(live labels)`. A lower stage rests on fewer labels, so its threshold is less sure. The audit and the drift rule act at every stage. `sdd grounding status` names the stage the judge holds and the referenced pairs the next one needs.

While the judge holds no stage, a capture's reading ranks by its scores. Of the entries the judge scored that the [far floor](#the-far-floor) did not set aside, the top 100 are listed to judge as briefs, the next 300 by ID and title alone, and a last line counts the rest; an entry the judge left unscored is listed to judge. A judge whose ranking recall is weak doubles the 100 to 200. The 100 and the 300 match the deep reading's tiers, so the drafting agent and the verifier read the same cut of one ranking. An entry listed by title is listed but not shown: it gets a state only when the draft references it, a verdict dismisses it, or `gather` with its ID opens it.

A live capture adds about two referenced labels, so a new judge would hold no stage for a long time. `sdd grounding update` seeds it from the graph itself: it labels each entry with every entry it refs, closes or supersedes, leaving out actors, roles and the entries the binary ships, as replayed references, skipping pairs already labelled, then has the ranking judge score every labelled pair it has not scored, one request per referencing entry. `--force` scores every labelled pair again, the new scores replacing the judge's old ones. A reranker bills each request as a search unit, so seeding a graph costs about one search unit per entry that references another. Its labels count as replayed ones: `--exclude-replay` leaves them out, and `--drop-replay` takes them back. A reference makes an easier positive than an entry a capture's reading had to find, so a calibration resting on seeded labels may set aside more than one resting on live ones, until the live labels hold a stage of their own and their lower threshold takes over. While the judge holds no stage, `sdd grounding status` says so and names the command, and `sdd serve` logs it once at start.

#### The far floor

Every scoring call of a gather also rates three entries far from the draft. An entry is far when it is live, is no candidate of the gather, none the draft answers and none the binary ships, shares no topic and no topic area (the first component of a topic path) with the draft, and lies more than two reference hops (refs, supersedes or closes, in either direction) from every entry the draft references. sdd picks the three by a hash of the draft and the entry ID, so every gather of one draft picks alike, and puts them among the call's lines in the form of every other line, at positions drawn the same way. A candidate the judge rates no higher than the median of the three in its own call is set aside, in a group of its own in the gather's result, with and without a calibrated threshold. The floor rests on no label, so it sets aside from the first gather on a machine and for any judge.

Two kinds of candidate never fall below the floor, whatever their score: one within two reference hops of an entry the draft references, and one that two or more of the gather's reads found. A gather has no floor when fewer than three entries are far from the draft, when the draft has neither a topic nor a reference, or when a call of the judge holds no more than three entries. The far entries take three of the entries a call holds, and a call sent again in halves carries them in every half. Their scores are not kept.

A floor verdict stays with the capture: a later gather of the same draft scores no candidate again that it holds a score for, and keeps each one where its first verdict put it. A held verdict gives way only to what the floor never overrides: a candidate the draft has since referenced a neighbour of, or a second read has found, returns to the lines to judge, and once the floor is off for the judge its held verdicts set nothing aside. The audit covers the floor's set-asides with the threshold's, 3% of both shown blind as lines to judge. A referenced sample of a floor set-aside counts against the floor and not against the threshold. Once the latest 200 audited floor set-asides, at least 50, hold a weighted share of referenced ones above 2%, the floor is off for that judge and stays off. `sdd grounding status` shows the floor's state and those counts.

#### Background calibration

While `verify.auto_calibrate` is on, `sdd serve` calibrates the ranking judge of a command without a client in the background, from a minute after it starts: on first use, after a judge change, or once captures add labelled pairs the judge has not scored. On a ledger without labels it first labels the graph's own references, as `sdd grounding update` does. It then scores the labelled pairs the judge has not scored, the referencing entries with the most referenced pairs first. Each request holds one entry's referenced pairs and a sample of its other labelled pairs, up to 60, since a chat judge scores an entry against the others in its call. It scores at once until the judge holds the next stage, then stops; each further request waits until no tool call has reached the server for a minute, stage by stage, up to the top stage. It never blocks a capture. Only one server per repository on a machine scores at a time. The log names the start, each stage reached and each failure; a referencing entry whose request failed is left until the next server. This spends model calls, or search units on a reranker: up to one request per referencing entry until the top stage holds.

When the server quits, on Ctrl-C, SIGTERM or the MCP client disconnecting, every scoring request in flight stops at once: sdd kills the `claude -p` process group of a Claude CLI judge and cancels an HTTP request of any other provider. Each request's scores are stored as soon as it returns, so the next server resumes with the pairs still unscored and pays for none twice. `sdd grounding update` stores its scores the same way, and Ctrl-C stops it the same way.

Every entry a capture's reading showed, served in full or printed as a brief to judge, ends the capture in one of four states: referenced (in the draft's refs, supersedes or closes), opened (served in full), dismissed explicitly (in `verdicts`, or through `sdd dismiss`), or dismissed implicitly, which sdd records at publish for every shown entry left without another state. An entry printed only as a set-aside ID, a count or a line listed by title gets no state. Each state is a calibration label weighted by how much it says: referenced 1, opened 0.5, an explicit dismissal 1, an implicit one 0.25. References are positive labels; dismissals coded `topic-only`, `other-scope` or `term-clash`, opened or not, and implicit ones are negative; `other` is counted only, and `covered`, `meta` and an opened entry without a code leave calibration. Calibration, the recall bound and the audit's drift share count labels by weight, so four implicit dismissals weigh as one explicit answer, and an audit sample left unanswered counts for the drift rule, and for the far floor, as an implicit dismissal. When a later entry references both an entry and a candidate that entry's capture dismissed implicitly, by refs of any kind (`refines` among them), supersedes or closes, sdd flips that dismissal to a positive label at the later entry's publish. `sdd grounding status` shows the flip rate: the implicit dismissals flipped, of all recorded.

The verdicts and scores behind that calibration form the relevance ledger, which stays on your machine: it lives in `~/.local/state/sdd/relevance/<repository>/`, or under `$XDG_STATE_HOME/sdd/relevance/` when that variable is set, keyed like the search index, and never in the repository. Each machine and clone builds its own. The daily cache pruning removes the ledger of a project unused for `cache.retention`. The settings `relevance.commit` and `relevance.content` no longer exist: sdd reports them as ignored and `sdd config set` refuses them. Segments a repository still holds under `.sdd/relevance/` are ignored too: every command warns and names the folder to move them into, which merges them, and you commit their removal from the repository.

A `driver` command reads one JSON request on stdin each round, `{"round": 1, "session": "<child session>", "instance": "<child instance>", "prompt": "<round prompt>", "driver_state": "<last round's state>"}`, starts or continues its agent with the prompt, and prints `{"driver_state": "<state for the next round>"}` once the round ends. The agent reports through the child session's `next` calls, never through the driver. A nonzero exit, a round longer than `verify.round_timeout`, a round that ends before its agent delivered, a round whose process ended without recording an outcome, or a round whose process never started is a verifier failure. The driver runs in the round's process group: when the round's process ended without an outcome while the driver still runs, sdd stops the driver with that group.

In a guided dialogue the writing guide runs once on the content the user approved, then the verifier; a later adjust gets a check of the changed lines only. When the verifier leaves findings it changed nothing for, the draft is played back once more with them before the write. With `dialogue.mode: auto` both run before playback, and with `assisted` every check, pre-flight included, runs before the playback you approve.

#### Ranking the deep reading

The `deep` verifier's reading ranks the entries it does not serve by their relevance to the draft, with the [ranking judge](#ranking-judge) gather's prefilter used for the same capture, while `verify.prefilter` is on. Scores the gather already holds for the draft are reused, so no pair is scored twice. The [far floor](#the-far-floor) is gather's alone: the deep reading's ranking calls carry no far entries, and an entry the floor set aside ranks in the deep reading by its score.

Ranking starts as soon as the capture's draft and reading stand together, when `assemble` passes or `gather` runs after the draft, and runs while the playback and pre-flight do. The reading waits for it at most `verify.ranking_timeout` from its start; a call whose answer breaks is sent again in halves within it. Entries the judge left unscored, by then or because there is no judge, rank by their similarity to the draft in the local search index, the most similar first, then in listed order; without an embedder they keep listed order. Similarity alone serves no entry in full. The reading's header names its ranking source: the judge, similarity, listed order, or mixed, with the count of each.

`sdd grounding update` scores the labelled pairs with the ranking judge of a command without a client, the one `sdd grounding status` names; no other judge sends a request. A referencing entry whose requests fail, past `llm_prefilter.timeout` for one, does not stop the run: its pairs stay unscored and the other entries are scored. Once the judge is done, the command lists each failed entry with its judge and cause and exits non-zero; running it again scores the pairs the failures left. On a terminal a progress bar named for the judge counts the pairs done, failed ones included, and how many failed.

A judge's ranking recall is the weighted share of its referenced labels it scores at 0.5 or above. Measured from 20 referenced labels on, a recall below 0.9 makes the judge weak: `sdd grounding status --verbose` and `sdd serve` warn, and the reading it ranks doubles its tiers, the top 10 served in full and the next 200 listed with their summary sentence.

### Sync and sessions

| Key | Default | Meaning |
| --- | --- | --- |
| `sync.default.fetch_cooldown` | `5m` | Least time between two fetches from one remote. A value below `1m` is ignored. sdd lengthens it when the remotes on one host would otherwise exceed the hourly limit. A clone where none exists yet is not held by it, unless that clone failed itself: it then waits for the retry or the cooldown like a fetch. |
| `sync.default.push_cooldown` | `30s` | Least time between two pushes to one remote. A value below `10s` is ignored. |
| `sync.default.retries` | `2` | Retries of a git network operation that failed. |
| `sync.default.concurrency` | `2` | Git network operations that run at the same time against one host. |
| `sync.default.lease_pause` | `10m` | How long a clone's [lease](#leases) holds after the last call that renewed it. A value below `1m` is ignored. |
| `sync.default.read_wait` | `15s` | How long a call waits for the sync round its lease start runs. A value below `1s` is ignored. |
| `sync.default.ssh.per_hour` | `600` | SSH operations per hour against one host. |
| `sync.default.https.per_hour` | `600` | HTTPS operations per hour against one host, with credentials. |
| `sync.default.https.per_hour_unauthenticated` | `60` | HTTPS operations per hour against one host, without credentials. |
| `sync.default.ssh.per_minute` | A tenth of `ssh.per_hour` | SSH operations per minute against one host. Once reached, every operation waits for the next minute. |
| `sync.default.https.per_minute` | A tenth of `https.per_hour` | The same for HTTPS with credentials. |
| `sync.default.https.per_minute_unauthenticated` | A tenth of `https.per_hour_unauthenticated` | The same for HTTPS without credentials. |
| `sync.hosts.<host>.<limit>` | The `sync.default` value | The same limits for one remote host, such as `github.com`. |
| `sessions.retention` | `14d` | The [daily sweep](#the-daily-sweep) deletes a session that ended longer ago than this, with its staged attachments. Accepts days, such as `14d`, or a duration, such as `336h`. Read from the user-global file only. |
| `cache.retention` | `90d` | The daily sweep deletes cache, state and data that nothing used for longer than this. Same forms. Read from the user-global file only. |
| `index.replaced_retention` | `14d` | The daily sweep deletes replaced search index data this long after its replacement or last use, whichever is later. Same forms. Read from the user-global file only. |

`sdd config set` refuses a value below its minimum; read from a file, such a value is ignored with a warning.

#### Leases

The clones sdd keeps under `~/.local/share/sdd/repos/`, those of connected repositories and the project's own `graph_repo` clone, sync automatically only while a lease holds. A graph inside the code repository has no lease and never syncs automatically. Every `sdd serve` on the machine shares one lease per repository. A lease starts or renews:

- for the home project's graph, on `start_session`, `start_procedure` and `resume_session`;
- for a project, on every call of a move working in it: `start_procedure`, `next`, `park`, `abandon` and `gather`;
- for a repository a read names: `search` or `view` with `repos` or `all_repos`, `search`, `show`, `view`, `read_attachment` or `info` with `project`, a capture ref `<repo-id>:<entry-id>`, a capture written into a dependency;
- for the home project's graph, on a read naming no repository.

`registry` reads no repository and renews nothing. A lease ends `lease_pause` after its last renewal.

While a lease holds, the timer of `sdd serve` fetches the clone once per effective fetch cooldown, described under [git credits](#git-credits). Without a lease it fetches nothing. Commits waiting in a clone are pushed whether a lease holds or not.

When a call starts a lease and the clone's effective fetch cooldown has passed, one sync round runs at once and the call waits for it at most `read_wait`. Past that, the call reads the clone as it stands and its sync notice says the refresh still runs and when the clone last fetched; the round finishes in the background. A lease starting inside the cooldown reads the clone as it stands and spends nothing. The CLI commands that read connected repositories, `sdd show`, `sdd view`, `sdd search`, `sdd export` and `sdd new` with a ref into one, start or renew the leases of the repositories they read the same way and print the same warning. A command ends only once its rounds have finished.

#### Git credits

sdd counts every git network operation it starts in a bucket per host: SSH, HTTPS with credentials, or HTTPS without. Automatic fetches may use 90 percent of each bucket's hourly limit; the rest is kept for pushes and for syncs you command. The clones sdd keeps under `~/.local/share/sdd/repos/` share the fetches: the fetch cooldown of each repository is at least one hour times the number of those clones in its bucket, divided by 90 percent of the bucket's hourly limit. Repositories you work in yourself and removed clones do not count. With the defaults this lengthens the cooldown from 46 clones on, or from 5 over HTTPS without login. An HTTPS repository whose login sdd does not know yet counts in both HTTPS buckets and takes the longer cooldown.

Once a clone may fetch again, each span of one cooldown that passes without a fetch earns its host and protocol one credit. The credit expires one hour after its span began. A credit lets one operation run inside its cooldown:

- The push of commits sdd just wrote, such as a capture into a connected repository right after its last push.
- A sync you command with `sdd repo sync` or the `sync_repos` tool, its fetch or its push.

Other reads and the rounds `sdd serve` runs on its own never spend a credit. Even with one, fetches of one repository stay at least 1 minute apart and its pushes at least 10 seconds apart, the hourly and per-minute limits and `concurrency` still apply, and the cooldown after a failed operation holds. Without a credit the operation waits for its cooldown.

### Credential scan

| Key | Default | Meaning |
| --- | --- | --- |
| `secrets.rules_update` | `daily` | `daily`: the [daily sweep](#the-daily-sweep) fetches the rule file of gitleaks' latest release when it is newer than the one in use. `off`: sdd asks GitHub nothing and [scans for credentials](/sdd/working-modes/#capture) with the rule file built into sdd only, also when it fetched a newer one before. Read from the user-global file only. |

`sdd config set` refuses any other value. In the user-global file, another value fails every command but `sdd config set` and `unset`, and every call to a running `sdd serve`, with an error naming the file, the key, the value and the valid values. In a project or local file the key is ignored with a warning, whatever its value.

### Connected repositories

| Key | File | Default | Meaning |
| --- | --- | --- | --- |
| `dependencies` | Project | None | The `repo_id`s of the graphs this graph references. Reads, moves, `sdd export` and `sdd lint` reach these and every repository down their own `dependencies`, the whole chain. `sdd repo add` adds one and commits the change. `sdd repo remove` removes one. |
| `repos` | User-global | None | How this machine reaches each connected repository, as a list of `repo_id` and `clone_url` pairs. `sdd repo add` writes it. |

```yaml
# ~/.config/sdd/config.yaml
repos:
  - repo_id: github.com/acme/platform
    clone_url: git@github.com:acme/platform.git
```

sdd keeps a clone of each connected repository under `~/.local/share/sdd/repos/<repo_id>`. A read that needs it starts or renews the clone's [lease](#leases), which runs the clone's sync round once its effective fetch cooldown has passed. The [connected repositories guide](/sdd/guides/connected-repositories/#a-connected-graph-is-what-was-pushed) describes the round.

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
- `json_object` asks for any JSON object (`response_format: {"type": "json_object"}` on `openai`, `format: json` on `ollama`), states the schema in the system prompt and validates the answer against it locally, after dropping properties the schema does not declare, since no check reads them. An answer that is not one JSON object matching the schema is never passed on: the check sends it to the reformat call described under [language model](#language-model), whose answer is validated the same way.
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

sdd runs `claude -p` isolated, the context window probe included:

- `--tools ""` and `--strict-mcp-config`: no built-in tools and no MCP servers.
- `--safe-mode`: instruction files, skills, plugins, hooks and your other customizations stay out of the prompt.
- `--no-session-persistence`: no transcript of the draft stays under `~/.claude`.
- An empty temporary working directory, removed after the call, so no project file is in reach either.
- In every mode, none of the variables a Claude Code session hands the processes it starts, such as `CLAUDECODE`, `CLAUDE_CODE_SESSION_ID` or `CLAUDE_CODE_ENTRYPOINT`. When an agent started sdd, the call would otherwise count itself part of that agent's session.
- With `llm.claude_cli.credentials: login`, the default, no `ANTHROPIC_*` or `CLAUDE_*` variable reaches `claude -p`, matched without regard to case, except `CLAUDE_CONFIG_DIR`, `ANTHROPIC_CONFIG_DIR`, `CLAUDE_SECURESTORAGE_CONFIG_DIR`, `CLAUDE_CODE_OAUTH_TOKEN`, `CLAUDE_CODE_OAUTH_TOKEN_FILE_DESCRIPTOR`, `CLAUDE_CODE_CERT_STORE`, `CLAUDE_CODE_CLIENT_CERT`, `CLAUDE_CODE_CLIENT_KEY`, `CLAUDE_CODE_CLIENT_KEY_PASSPHRASE`, `CLAUDE_CODE_DISABLE_LOOPED_CROSS_CERT_DROP`, `CLAUDE_CODE_DISABLE_SELF_NAMED_CERT_DROP`, `CLAUDE_CODE_GIT_BASH_PATH` and `CLAUDE_CODE_TMPDIR`. Every other variable passes. No user, project or local settings file loads (`--setting-sources ""`), since a settings file's `env` block and `apiKeyHelper` apply even in safe mode; managed policy settings still apply.

Every variable that moves a call off the login, such as a key, an endpoint, a provider switch, `ANTHROPIC_PROFILE` or a host-managed credential, sits in Claude's own name space. Provider credentials outside it, such as `AWS_*`, take effect only through a `CLAUDE_CODE_USE_*` switch inside it. To use an API key or another variable of that name space, set `llm.claude_cli.credentials: env`, or use `provider: anthropic`. With `env`, `claude -p` loads your settings and can also run on another provider such as Bedrock or Vertex. A proxy or CA set only in your settings' `env` block is left out in `login` mode: export it, or use `env`.

Two file-borne routes stay open in `login` mode:

- The `env` map of Claude Code's global config, `$CLAUDE_CONFIG_DIR/.claude.json`, else `~/.claude.json`, or a legacy `.config.json` in Claude Code's config directory. Claude Code applies it whatever `--setting-sources` says, so a key, endpoint or provider switch there moves the call off your login.
- An Anthropic Console profile. Claude Code looks for it in `ANTHROPIC_CONFIG_DIR`, else `$XDG_CONFIG_HOME/anthropic`, else `~/.config/anthropic`, under the name in `<dir>/active_config`, else `default`. A profile of type `oidc_federation` takes precedence over your stored claude.ai login and may carry its own `base_url`, so with one the calls run on that profile, unless `CLAUDE_CODE_OAUTH_TOKEN` or its file descriptor is set. A `user_oauth` profile answers only when no claude.ai login is stored.

Without either, and unless your managed policy settings' `env` sets one, `login` mode bills your Claude Code login.

`claude-cli` needs Claude Code 2.1.169 or later, the first version with `--safe-mode`. An older `claude` stops with `unknown option`, and sdd shows that message. A failed call says what `claude -p` reported, on its output or its error stream.

If you ran `claude-cli` on an exported `ANTHROPIC_API_KEY` without a Claude Code login, calls now fail with `Not logged in`. Sign in to Claude Code with `/login`, or set `llm.claude_cli.credentials: env`. With one of the two file-borne routes above, the calls run on it instead of failing. Any other `ANTHROPIC_*` or `CLAUDE_*` variable a setup relied on needs `llm.claude_cli.credentials: env` too.

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

The `codex` provider runs each call with `--json`, a read-only sandbox, `--ephemeral`, `--ignore-user-config`, `--ignore-rules` and `project_doc_max_bytes=0`, in a working directory of its own. So no project's `AGENTS.md`, no MCP servers or profiles from `config.toml` and no execpolicy rules reach a prompt that needs none of them, and no session is kept. `OPENAI_API_KEY`, `CODEX_API_KEY` and `OPENAI_BASE_URL` are left out of its environment, so an exported key or endpoint never answers in place of your login. Your login still comes from `CODEX_HOME`.

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

The index keeps every embedded version of every entry until the [daily sweep](#the-daily-sweep) removes it, so sdd versions that share it do not remove each other's work. `sdd index gc` lists the stored versions by group, `sdd index gc --drop <group>` deletes one group, and `--drop stale` deletes every version this checkout does not search.

## The daily sweep

Once a day sdd starts `sdd cache prune` in the background, detached from the command that started it; a running `sdd serve` checks every hour whether a run is due. The run keeps the cache (`~/.cache/sdd`), state (`~/.local/state/sdd`) and data (`~/.local/share/sdd`) folders free of what nothing needs. It never delays a command or waits for a lock another process holds: it skips what is in use and takes it on the next run. A failed run is reported once, as a warning on the next command, and `sdd info` shows the last run. `sdd cache prune` runs it by hand.

| What | When it goes |
| --- | --- |
| A session that ended (concluded or abandoned), with its staged attachments | `sessions.retention` after it ended, also one older sdd versions logged in the `.sdd/sessions` of a checkout sdd recorded. An open session stays, however long it is idle. |
| A staged attachment | Once the entry it is attached to is published, unless another capture in progress names it; the rest of a session's when the session ends. One a pending capture still needs stays until that capture is retried or cancelled. |
| A project's relevance ledger and its empty session folder | After `cache.retention` without the project being used. Open sessions stay. |
| A project's staged attachments no session owns | After `cache.retention` without the project being used. Those of a session open in a checkout's `.sdd/sessions`, where older sdd versions logged sessions, stay: sdd records each checkout it opens the project from and reads the sessions there. So do all the project's staged attachments while no checkout is recorded, or while a recorded one is missing from disk, as on an unmounted volume. |
| A recorded checkout gone from disk, such as a removed worktree | Its record line once it has been missing for `cache.retention`; until then it may be an unmounted volume. Then also the staged attachments, untouched for `cache.retention`, of sessions no log names any more, while the project is in use too. |
| An older version of an entry in the search index | `index.replaced_retention` after a newer version replaced it, unless a checkout that searched the index within `cache.retention` still reads it. The version the current checkout searches always stays. |
| Index data in an older format | `index.replaced_retention` after its last use. |
| The index of an embedding setup you switched away from | `index.replaced_retention` after its last use, once the project's index under another setup was used later. Switching back afterwards embeds the graph again. |
| A whole index nothing uses | After `cache.retention`. |
| Graph snapshots, the git gate's record of a repository, embedding calibrations | After `cache.retention` without use. |
| A clone under `~/.local/share/sdd/repos/` | Once `sdd repo remove` dropped its repository under the same user-global config file and the `repos` list in it no longer names it, unless it holds unpushed commits or entries a force push set aside, or sdd fetched or pushed it within `cache.retention`. A clone no `sdd repo remove` dropped stays, `graph_repo` clones included; delete it by hand. So does a clone holding another clone, or the place of a listed repo, inside it. Listed clones stay and get `git gc --auto`. |
| The log of a verifier round | After `cache.retention`. |
| What a killed process left: git trace files, unfinished clones, unfinished index writes, emptied index folders | One day after, once no lock holds it. |

A session log sdd cannot read stays, with its staged attachments: it may belong to a newer sdd. The run reports how many it kept.

Another sdd version on the machine may use the same cache and state folders. The run works only on what this sdd has a record of using, and leaves the rest untouched, however old:

- An index that holds no use stamp. sdd stamps an index each time it searches or fills it.
- The sessions and staged attachments of a project this sdd never opened: one it holds no use stamp, checkout record or relevance ledger of.
- Every clone and folder under `~/.cache/sdd` that a version keeping its clones there placed. This sdd keeps its clones under `~/.local/share/sdd/repos/` only.

The checkout record of a project is `~/.local/state/sdd/checkouts/<repository>.list`. Each line holds a checkout's path, when sdd last saw it on disk and, once it is gone, since when, separated by tabs, the times in UTC. A line holding a path alone, as earlier versions wrote it, still reads, and sdd rewrites it on the next write.

While `secrets.rules_update` is `daily`, the background run also asks GitHub for gitleaks' latest release and fetches its rule file into `~/.cache/sdd/secret-rules/` when it is newer than the one in use ([credential scan](/sdd/working-modes/#capture)). Each request gives up after 20 seconds. A failed or rejected fetch keeps the rule file in use and does not stop the run's other work, but fails the run: the next command reports it once, and `sdd info` shows it with its reason until a later check succeeds. `sdd cache prune` by hand fetches nothing.

The run removes no clone and fails when a clone would go while the user-global config file is missing or lists no repos, or while some of `XDG_CONFIG_HOME`, `XDG_CACHE_HOME`, `XDG_STATE_HOME` and `XDG_DATA_HOME` are set and others are not. While some of `XDG_CACHE_HOME`, `XDG_STATE_HOME` and `XDG_DATA_HOME` are set and others are not, the run fails and removes no session, staged attachment or relevance ledger. Set all four or none.

`sdd index gc` stays for deliberate cleanup beyond these rules.

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

Every config file may declare `hooks`: the user-global file, and both `.sdd/config.yaml` and `.sdd/config.local.yaml` of the project. In a project whose graph lives in a [repository of its own](/sdd/guides/graph-repository/), the code repository's two files declare hooks as well. sdd runs the hooks of every file together.

`command` names a file in the declaring layer's hooks folder:

| Declared in | Hooks folder |
| --- | --- |
| User-global config | `~/.config/sdd/hooks/` |
| `.sdd/config.yaml` | `.sdd/hooks/`, committed with the project |
| `.sdd/config.local.yaml` | `.sdd/hooks/`, its scripts in `local/` there, which `sdd init` adds to `.sdd/.gitignore` |

sdd refuses an absolute path, `..`, a name it would have to look up on `PATH`, and anything a shell would interpret. A hook runs without a shell, with the hooks folder as its working directory, so `args` resolve from there too. Put an interpreter in the script's shebang and make the script executable. sdd judges `local/` by the folder a command reaches, links followed and in any spelling: a hook in the committed file may not run a script there, which only your machine has, and a hook in `config.local.yaml` runs only a script there, never one a pull may change.

### Allowing a repository's hooks

Hooks in a repository's `.sdd/` files, the committed and the machine-local one alike, run only once you allow that repository's hooks on your machine. Hooks from the user-global file need no allow.

An allow records a hash over the repository's hook declarations, each with its layer, event, command, arguments, timeout and findings, and over every file git tracks in its `.sdd/hooks/`, by content and execute bit, a link by its text. The whole folder counts because a hook runs more than the script it names: files it sources or imports, a file a flag names, an interpreter beside it. A change to any of them, whether you made it or a pull brought it, needs a new allow. A file git does not track, such as `local/` or a cache a hook writes beside itself, changes nothing; code outside `.sdd/hooks/` is not covered. sdd follows no link to hash it, so a link out of the repository is covered by its text alone.

- `sdd hooks` lists every hook as `allowed`, `pending` (never allowed on this machine), or `changed` since you allowed it.
- `sdd hooks allow` shows each repository waiting with its hooks, the files the allow covers with their SHA-256 digests and the first 20 lines of each script, and asks y/N. It runs only in a terminal: you allow each repository yourself, so without a terminal it allows nothing and fails. It records only what it showed you: a change in between refuses the allow.
- `sdd init` offers the waiting repositories when it runs in a terminal, and otherwise names how many hooks wait.
- Over MCP, every call that works on a session or opens one first checks all of the project's hooks. Before it does anything else, it shows you the prompt for each repository waiting at once through your client, and goes on only once you accept them all. A decline, or a client that cannot ask, fails the call with nothing done and names `sdd hooks allow`. A hook that changes while the call runs fails the call the same way.

sdd fails closed. A repository git cannot list, such as a folder without `.git` or one git refuses, has hooks that neither run nor can be allowed, and the call names why. A hooks folder whose link leads out of the repository is refused the same way, and so is one holding files outside `local/` while git tracks none of them, as in a project unpacked inside a repository that does not track it: track the files, or give the project a repository of its own.

The allow record is `~/.local/state/sdd/hooks/allowed.json`, readable only by you. It is keyed by the checkout's real path, which a repository cannot choose, so each checkout is allowed on its own. An unreadable record allows nothing, and the next `sdd hooks allow` rewrites it. The [daily sweep](#the-daily-sweep) drops the allows of checkouts gone from disk.

A hook sees only `PATH`, `HOME`, `LANG`, `LC_ALL`, `LC_CTYPE`, `TMPDIR`, `USER` and `LOGNAME` of sdd's environment, so API keys set there do not reach it. It still runs with your rights and can read your files, sdd's config included.

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

Hooks on one event run at once. A hook fails the call when it exits nonzero, outlives its timeout, writes more than 1 MiB to stdout, or writes anything to stdout but nothing or `{"findings": [...]}`. Its stderr joins the error, cut at 4 KiB. A high finding holds the step like one of the step's own.

The payload carries `event`, `time`, `project`, `branch`, `session`, `procedure`, `instance`, `step` and `phase` (`start`, `done` or `round`); `transition`, the step the procedure moves to, when a step is done; `findings`, the step's findings so far, on a step with a findings channel; `draft` during a capture, with `type`, `kind`, `layer`, `refs`, `topics`, `body`, `attachments`, `body_hash`, `supersedes`, `closes` and `approves` (entry ID lists) and `skip_grounding` (whether the draft bypasses the grounding gate); and `entry`, the written entry's ID, on `entry.written`. A finding has `severity` (`high`, `medium` or `low`), `category` and `observation`.

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

## sdd serve flags

| Flag | Default | Effect |
| --- | --- | --- |
| `--output-schema` | Off | For debugging and for programs that read the replies as data, not through a model: lists each tool's reply schema. Agents don't need it. |

## Environment variables

| Variable | Default | Effect |
| --- | --- | --- |
| `XDG_CONFIG_HOME` | `~/.config` | The user-global file is `$XDG_CONFIG_HOME/sdd/config.yaml`. |
| `XDG_CACHE_HOME` | `~/.cache` | Search indexes live under `$XDG_CACHE_HOME/sdd`. |
| `XDG_DATA_HOME` | `~/.local/share` | Clones of connected repositories live under `$XDG_DATA_HOME/sdd/repos`. |
| `XDG_STATE_HOME` | `~/.local/state` | Session logs, the hook allow record, the relevance ledger and the logs of verifier rounds live under `$XDG_STATE_HOME/sdd`. Must be an absolute path. |
| `SDD_GRAPH_CACHE` | On | `off` reads every graph from its files. Otherwise the CLI keeps each graph's entry files under `$XDG_CACHE_HOME/sdd` and reads them back only while every graph file's size, timestamps and inode match what they were when it was saved, so a command on an unchanged graph reads one file instead of every entry. |
| `SDD_SERVE_TOKEN` | None | The bearer token `sdd serve --transport http` requires on every request. Same as `--auth-token`. |
| `SDD_USAGE_LOG` | None | A file path. Every model call, pre-flight and embedding alike, appends one JSON line with its step, provider, endpoint host, model, token counts, finish reason, the provider's `billing_cost` object where the reply carries one, and whether it succeeded. Keys, prompts and answers never reach it. A path sdd cannot write fails the command. The record shape is `llmstats.UsageRecord` in `internal/llmstats`. |
