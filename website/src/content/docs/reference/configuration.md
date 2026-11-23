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
| `dialogue.mode` | User-global or local | Set per person. sdd ignores it in the project file. |
| `llm_prefilter.*` | User-global | Any file works, as for `llm.*`. |
| `verify.*` | Project or user-global | Each key names its files under [verify](#verify). The local file may only tighten `verify.mode`, `verify.kinds` and `verify.skip`, and wins for the keys that describe the machine. |
| `graph_dir`, `default_branch`, `repo_id`, `language`, `skill_scope`, `supported_agents`, `dependencies` | Project | These describe the repository. sdd ignores them in the user-global file. |
| `events`, `recheck` | Project | The team's choice. sdd ignores them in the local file and in the user-global file. |
| `repos` | User-global | sdd ignores it in the project files. |
| `contribution` | Local | sdd refuses it in `.sdd/config.yaml` and ignores it in the user-global file. |

:::caution[Keep API keys out of the project file]
`.sdd/config.yaml` is committed and shared. sdd reads `api_keys` from any file and does not stop you, so put keys in the user-global or the local file. `sdd config set` writes only to those two.
:::

sdd skips a key it does not know, including a key in a file where it does not belong. Every command then warns `ignoring unknown config key` and names the key and the file, and `sdd config` lists these keys at the end. A file written by a newer sdd version therefore still works with an older one.

sdd also reads past a known key whose value it does not apply: a per-person key in the project file, such as `dialogue.mode`, or a value outside a setting's fixed set, such as `verify.mode light`. Every command then warns `ignoring config setting` with the key, the file and the reason, the value of the layer below applies, or the default, and `sdd config` lists these settings at the end.

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
- `set` refuses a value sdd would ignore or refuse when it reads the file: one outside a setting's fixed set, such as `dialogue.mode x`, or a `verify` value the layers do not allow together, such as a local `verify.mode` below the shared one. The refusal names the allowed values, and the file stays unchanged.
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

### Participant

| Key | Default | Meaning |
| --- | --- | --- |
| `participant` | None | Your name, recorded as the author of the entries you capture. `sdd init` asks for it, offers your git `user.name`, and writes it to `.sdd/config.local.yaml` unless another file already sets it. Every command warns while it is missing. |

### Contribution mode

For a fork whose features go upstream as pull requests: keeps each feature's entries on that feature's branch.

| Key | Default | Meaning |
| --- | --- | --- |
| `contribution` | None | An upstream ref, such as `origin/main`. Set, every graph write in this checkout is refused unless the checked-out branch is cut from the ref and is neither `default_branch` nor the ref's own branch. Covers `sdd new`, `sdd rewrite`, `sdd summarize`, `sdd wip start`, `sdd wip done` and captures through `sdd serve`. A ref that does not resolve refuses every write, so fetch it first. Set it with `sdd config set --local contribution origin/main` in the feature's worktree. `sdd info` and the session framing show `Mode: contribution (origin/main)` while it is on. |

### Dialogue

| Key | Default | Meaning |
| --- | --- | --- |
| `dialogue.mode` | `guided` | Who takes the choices an engine session would put to you. `guided`: you confirm each step. `auto`: your agent takes each choice a procedure marks delegable and records a one-line reason with it, for a headless job too. Where a check blocks a capture, the agent may only take the one answer the step allows, revising the draft or retrying the check, and only once; it never overrides, settles or skips a check, and sdd refuses words relayed as yours at those steps. A second block at the same step [parks the capture](/sdd/guides/working-with-coding-agents/#parked-captures). sdd reads the key from the user-global file and the local file `.sdd/config.local.yaml` only; in `.sdd/config.yaml` it is ignored with a warning naming the file and the key. `sdd init` asks for it, or takes `--dialogue-mode`, and writes it to the local file. Any other value is ignored with a warning and the session runs guided; a local value does so even when the user-global file sets `auto`. A session opens in the mode the config sets. Continued under an `sdd serve` that read another mode at its start, it follows that mode from its next call. |

### Language model

| Key | Default | Meaning |
| --- | --- | --- |
| `llm.provider` | `claude-cli` | `anthropic`, `openai`, `ollama` or `claude-cli`. See [language model providers](#language-model-providers). |
| `llm.model` | `claude-sonnet-4-6` | Model name as the provider spells it. sdd uses the default with any provider, so set a model whenever you change the provider. |
| `llm.effort` | `medium` for `claude-cli` and `anthropic`; `low` for `openai` and `ollama` | Reasoning effort, passed as written: to `claude-cli` as `--effort`, to `anthropic` as `output_config.effort`, to `openai` as `reasoning_effort` and to `ollama` as `think`. sdd sends it only to models whose API takes it: on `anthropic` Claude Opus 4.5 and later Opus models, Sonnet 4.6 and later, Haiku 5.5 and later, and the Fable and Mythos models; on `openai` the o-series and GPT-5 and later, but not `-chat` models or `o1-mini` and `o1-preview`; on `ollama` the `gpt-oss` models, which take thinking levels. Other models run at their own default. A value for the same field in `llm.params` wins. |
| `llm.api_keys.<provider>` | None | API key for a provider. Required for `anthropic` and `openai`. Optional for `ollama`, which receives it as a bearer token. |
| `llm.endpoint` | The vendor's public API | Base URL of a gateway or compatible service, for `openai` and `anthropic` only. Accepts the host, a URL ending in `/v1`, or the full URL ending in `/chat/completions` for `openai` or `/messages` for `anthropic`. |
| `llm.ollama_endpoint` | `http://localhost:11434` | Ollama server for the `ollama` provider. |
| `llm.timeout` | `5m` for `claude-cli`, `2m` for every other provider | Time limit for one model call. `sdd new --preflight-timeout` and `sdd summarize --timeout` override it for one run. A pre-flight call past it leaves the draft unjudged, and the capture puts the timeout to you: send the call again, revise the draft, skip the check where `verify.skip` allows it, or drop the capture. The error names the setting, its value and `sdd config set llm.timeout <duration>`. |
| `llm.extract_timeout` | `1m` | Time limit for the reformat call that follows a check response that does not parse. `sdd new --preflight-extract-timeout <duration>` sets it for one command. |
| `llm.params.<name>` | None | Provider settings that sdd adds unchanged to every request body, such as `reasoning_effort` set to `low`. `claude-cli` ignores them. |
| `llm.preflight.params.<name>` | None | Provider settings for the pre-flight calls alone, laid over `llm.params` key by key, such as `reasoning_effort` set to `medium` while other calls keep `high`. Unset, pre-flight takes `llm.params` as they stand. `claude-cli` ignores them. |
| `llm.rate_limit_rps` | See below | Maximum requests per second to `anthropic` or `openai`. Raise it on a higher account tier or for a gateway that allows more. |
| `llm.retry.max_attempts` | `5` | Attempts per call, counting the first. `1` turns retries off. |
| `llm.retry.base_delay` | `2s` | Wait before the first retry. The wait grows with each further retry. |
| `llm.retry.max_delay` | `60s` | Longest wait between two retries. |
| `llm.claude_cli.credentials` | `login` | What `claude-cli` authenticates with. `login` uses your Claude Code login: `claude -p` gets no `ANTHROPIC_*` or `CLAUDE_*` variable but the login's own and a few TLS and path ones, and no user, project or local settings file. `env` passes the environment and your settings, so `claude -p` can run on an Anthropic API key or another provider such as Bedrock or Vertex. See [Claude Code CLI](#claude-code-cli). |
| `llm.concurrency` | `4` | Not applied in this version. `sdd summarize --all` takes its worker count from `--concurrency`, which defaults to 4. |
| `llm.context_tokens` | None | The model's context window in tokens, which sizes the calls of the [prose verifier](#verify) only. Unset, a call holds about 100,000 characters, which fits every supported model. Set, a call fills half the window, counting 3 characters a token. |

sdd retries a call that failed with a temporary error, such as a rate limit or a dropped connection, and waits as long as the provider asks when it names a time. `claude-cli` ignores the rate limit and retry keys, since the `claude` command retries on its own.

When a checker's response does not parse, sdd makes one more call carrying the unparseable text and asking for the JSON alone, so a model that writes its conclusion into the `severity` field does not cost you the capture. A response that parses costs one call. `llm.extract_timeout` bounds the second call separately from `llm.timeout`, so a slipping endpoint cannot spend twice the configured timeout on one check. Both responses failing is an error naming both attempts.

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
| `embedding.keep_alive` | `30m` | How long Ollama keeps the model loaded after sdd's last request, as a Go duration. A negative one, such as `-1s`, keeps it loaded; `0` unloads it at once. Ollama's own default unloads it after 5 idle minutes, so a search after a pause waits for a reload. `openai` ignores it. |
| `embedding.batch_size` | `100` for `openai`, `64` for `ollama` | Texts sent in one request. |
| `embedding.dimensions` | The model's own size | Shorter vectors, for `openai` models that support it, such as `text-embedding-3-small`. `ollama` ignores it. |
| `embedding.rate_limit_rps` | `5` for `text-embedding-3` models, `3` for other `openai` models | Maximum requests per second to `openai`. `ollama` has no limit. |
| `embedding.query_template` | None | Template for search queries. See [query and document templates](#query-and-document-templates). |
| `embedding.document_template` | None | Template for entries when sdd indexes them. |

### Verify

The `verify` block sets how carefully a capture's draft is checked against the graph before it is written. The whole block with its defaults:

```yaml
verify:
  mode: prose            # off | grounding | prose | deep
  skip: failure          # weighed | failure | never
  depth:
    down: 2
    up: 1
  brief_all_below: 100
  prefilter_from: 30
  prefilter: true
  runner: subagent       # subagent | claude | driver
  driver:                # never in .sdd/config.yaml
    command: ""
    args: []
  claude:
    model: sonnet
    effort: medium       # low | medium | high | xhigh | max
  round_timeout: 10m
  idle_timeout: 10m
  kinds: {}              # for example directive: deep
```

| Key | File | Default | Meaning |
| --- | --- | --- | --- |
| `verify.mode` | User-global, project, local (raise only) | `prose` | How carefully a capture's draft is checked. `off`: pre-flight checks the draft against the entries it references. `grounding`: the capture first reads the related entries through [`gather`](#grounding), with no check after. `prose`: grounding, then pre-flight runs over the whole reading and adds the checks for duplicates, outdated claims, clashes between entries and set-aside entries that bear on the draft. `deep`: grounding, then a [verifier agent](#deep-verifier) judges in place of pre-flight's model. `deep` needs Claude Code: under `deep`, or with a kind set to `deep` in `verify.kinds`, a session from Codex fails to start or resume and names the setting, as does a Codex capture into a connected repository whose own config checks it `deep`. The project file wins over the user-global one, and the local file may only raise it, in the order `off`, `grounding`, `prose`, `deep`. `sdd init` asks for it in a terminal while no file sets it, or takes `--verify-mode`, and writes it to `.sdd/config.yaml`. |
| `verify.kinds.<kind>` | User-global, project, local | None | The mode for drafts of one entry kind, for example `verify.kinds.done: grounding`. A named kind gets exactly its mode, stricter or looser than `verify.mode`; an unnamed kind gets `verify.mode`. While no kind is named, `done`, `actor`, `role`, `focus`, `annotation`, `insight`, `question`, `activity` and `procedure` are checked at most at `prose`. A kind at `off` owes no grounding. Kinds merge one by one across the layers, and the local file may only raise each kind's mode. |
| `verify.skip` | User-global, project, local (tighten only) | `failure` | When a check may be skipped: the deep exchange, or a pre-flight answer that was cut short or ran past `llm.timeout`. `weighed`: also after you weighed the skip; sdd recommends a run for a draft naming a checkout path, a URL or a commit, for a directive, plan or contract above the operational layer, for a supersede or close, and while pre-flight has medium findings open. `failure`: only after a verifier failure, or for an unjudged pre-flight answer. `never`: the write waits until the check judged the draft. Only you skip, and never while a high finding is open. A skip writes `verification: skipped` into the entry and lowers its confidence: to `low` after a failure or an unjudged answer, and at most `medium` for a weighed skip. The local file may only tighten it toward `never`. |
| `verify.depth.down`, `verify.depth.up` | User-global, project, local | `2`, `1` | How far down and up a candidate's chain of refs the reading takes in candidates. |
| `verify.brief_all_below` | User-global, project, local | `100` | Below this many live entries, the reading lists every live entry's brief and runs no search. |
| `verify.prefilter_from` | User-global, project, local | `30` | Below this many candidates, the reading calls no [relevance prefilter](#relevance-prefilter). |
| `verify.prefilter` | User-global, project, local | `true` | Rates the relevance of each candidate before the reading lists it, and sets aside the ones below the calibrated threshold. The judge runs on the [`llm_prefilter`](#relevance-prefilter) model. On `claude-cli` each rated entry starts a process of its own. The deep verifier's reading uses the same judge and threshold. |
| `verify.runner` | User-global, project, local | `subagent` | What runs the deep verifier. See [runners and rounds](#runners-and-rounds). Under `deep`, `sdd init` asks for it while no file sets it, or takes `--verify-runner`, and writes it to the local file; it offers `claude` only where `supported_agents` names it and `driver` only where a file sets `verify.driver.command`. The setting describes the machine, so the local file wins. |
| `verify.driver.command`, `verify.driver.args` | User-global, local | None | The command the `driver` runner runs each round. sdd refuses it in `.sdd/config.yaml`, since it would run on every contributor's machine. |
| `verify.claude.model`, `verify.claude.effort` | User-global, project, local | `sonnet`, `medium` | The model and effort of the deep verifier: the `claude` runner passes them to `--model` and `--effort`, and a `subagent` dispatch names them when the MCP client is Claude Code. The values pass as written. The local file wins. |
| `verify.round_timeout` | User-global, project, local | `10m` | How long one round of a verifier sdd launched may run, counted from its launch or from the end of the round before it, whichever is later. Past it the round fails, and the capture puts the failure to you. The local file wins. |
| `verify.idle_timeout` | User-global, project, local | `10m` | How long a capture waiting on its verifier lets the verifier's session go without a call. Past it sdd ends the verifier's session, and the capture puts the failure to you, naming the bound. The local file wins. |

A local `verify.mode`, `verify.kinds` value or `verify.skip` that would loosen what the shared files set, or a `verify.driver` in `.sdd/config.yaml`, is ignored on read with an `ignoring config setting` warning naming the key and the file, and the shared value applies; `sdd config set` refuses to write such a value. A `verify.mode`, `verify.skip`, `verify.runner`, `verify.claude.effort` or `verify.kinds` value outside its set, or a `verify.round_timeout` or `verify.idle_timeout` that is no positive duration, is ignored with an `ignoring config setting` warning, and the value of the layer below applies, or the default. `sdd config` shows the defaults.

#### Grounding

Under `grounding`, `prose` and `deep`, a capture reads the graph before it drafts, through `gather`: the `gather` tool of the `sdd` server, or the `sdd gather` command.

```bash
sdd gather [entry-id...]
  --draft <text> | --draft-file <path>     # the draft the reading searches from
  --kind <kind> --topic <topic>...         # the draft's kind and topics
  --query/-q <phrase> --term/-t <term>...  # explicit searches
  --subject <entry-id>                     # an existing entry stands in for the draft
  --embedding-provider, --embedding-model
```

On a graph below `verify.brief_all_below` live entries, `gather` lists every live entry's brief. On a larger one, a model call writes one search phrase and 2 to 6 keyword terms from the draft's kind, topics and text, unless `--query` or `--term` give them; `gather` then runs the semantic search, one keyword search per term, matched as whole words regardless of case, each listing up to 20 live entries that can become candidates, and lists every live entry of each draft topic. The reading covers the graph the capture writes to; connected repositories are not searched. The header names each read with its hit count, the live entries counted and the prefilter's status: `off`, `ok`, `partial` or `failed`. The reading follows in three parts: entries relevant enough to serve in full, briefs to judge, and a set-aside list by reason and count, such as entries not live or judged not relevant before. Only live entries become candidates, each once; actors and roles never do. Entries named by ID come in full, at most 12. Only a `gather` inside an engine capture grounds that capture; `sdd gather` prints the reading and credits none.

A capture under enforcement is refused until a `gather` inside it ran, and the refusal names the call that clears it. Every entry the reading served in full must end in the draft's refs, supersedes or closes, or be set aside in its `verdicts` with a code: `topic-only`, `other-scope`, `term-clash`, `covered`, `meta` or `other`. Actor and role drafts are exempt. Entries that landed in the graph since the draft was assembled are listed before the write: you set them aside, revise the draft, or drop the capture. A draft can also report a `strain` between two entries the reading listed, `contradicts`, `duplicates` or `outdates`, which opens a [recheck](#recheck) item; while it is open, a draft citing one side of it is refused, and a draft citing neither or both passes.

`sdd new` under enforcement is refused outside an engine capture, and the error names the two ways out: the engine capture over the MCP server, or `--skip-grounding`. `--skip-grounding` writes the entry without a reading and records the bypass in it as `grounding: skipped` with `confidence: low`, which `sdd show` and `sdd export` show. A dry run and actor and role entries pass.

#### Prose verifier

Under `prose`, pre-flight judges the draft against the whole reading, with the same severity scale and the same override. Each finding names the entries it rests on. A high finding stops the write until you revise or override; the other findings come once, and the agent proceeds or revises. Each reading entry comes with a header that is never cut, then its body where the agent read it in full, else its brief. A reading too large for one call is split into the fewest calls, entries linked by refs kept together, else entries sharing a topic; every call carries the same rules and draft. An entry too large on its own is sent as whole paragraphs, code blocks and list items ranked by their overlap with the draft, in document order with headings kept and a marker where text is left out. A call holds about 100,000 characters, or half of `llm.context_tokens` when that is set. A draft whose rules and referenced entries alone exceed one call fails, naming `llm.context_tokens` and `verify.mode: deep`.

The drafting agent picks the entry kind, so a draft whose kind is checked looser than another kind also gets a kind check: pre-flight tests whether the body answers its kind's question or that of a kind checked more strictly, and a misfit is a high finding that holds the write until the draft is relabelled or you override it.

#### Deep verifier

`deep` runs grounding and pre-flight's mechanical checks, then a tool-using verifier agent in place of pre-flight's model. The verifier, in a child session, reads the draft, the rules pre-flight's model would have applied and the grounding reading, checks claims against the code and its history, and exchanges findings with the drafting agent for at most 3 rounds. Before a finding reaches the drafting agent, sdd checks what it cites: a passage it quotes must be in the draft, a cited file in the checkout and holding any quote given from it, a commit in the repository and an entry in the graph; a URL is checked for its form only, never fetched. Refused findings go back to the verifier for one repair; refused again, they end the exchange as a verifier failure. The drafting agent accepts or contests each finding with a reason, and the verifier holds, concedes, revises or drops each contested one.

The reading lists each entry by its header and its brief, or its body where the drafting agent read it in full; a closed entry whose closer the reading also lists comes as its ID, kind, closer and brief alone. An entry the drafting agent was shown and left without a ref or verdict is marked implicitly dismissed, in the `prose` reading too, and a near-duplicate of the draft among these is a high finding. Once the prefilter is calibrated, an entry it scored below its set-aside threshold comes as one line, its ID and brief, unless it is a declared target, read in full, given a verdict, one ref away from a target, or of the draft's kind on its narrowest topic; 3% of these lines, drawn blind, come as ordinary entries instead. The verifier scores only the pairs the reading holds no score for, and publish stores every score, so no pair is scored twice. A finding resting on such an entry, opened by the verifier, counts as a miss of the prefilter; an opening without one is counted apart, with no effect on the threshold.

sdd turns the findings into edits of the draft and shows them as a diff, each hunk labelled by its finding; findings the agents settle are applied, and findings left open go to you. Accepting the diff has sdd write it into the draft. An edit of yours after a verdict that changes only text the exchange's edits wrote or its findings quote counts as settled: neither the verifier nor the writing guide judges it again. Any other draft changed after a verdict goes to the same verifier as a revision, the changed passages and the entries read since, which it judges against the reading it holds, again for at most 3 rounds. A verifier sdd runs headless takes a revision the same way: its next round resumes the session its last round reported. A fresh verifier reads everything again only once the earlier one ended, after 3 revisions, or when a headless round reported no session to resume, and its dispatch says which. Findings the verifier left without a change are played back to you before the write.

A capture is written only under a deep verdict for its exact draft. A skipped exchange hands the draft to pre-flight's model before the write; where that answer is cut short or runs past `llm.timeout`, you may skip it too, and the entry is written at `low`.

#### Runners and rounds

| Runner | What runs the verifier |
| --- | --- |
| `subagent` | The drafting agent's harness runs a fresh-context subagent, the `sdd_delegate` subagent `sdd init` installs. Where the harness has no subagent tool, sdd falls back to `claude` where `supported_agents` names it and its CLI is on `PATH`, and otherwise refuses, naming `verify.runner`. |
| `claude` | sdd runs `claude -p` headless in the project checkout, read-only: files, the web, `git log`, `git show`, `git diff` and `git blame`, and sdd as its only MCP server. Each later round resumes the same session. |
| `driver` | sdd runs `verify.driver.command` with `verify.driver.args` each round. |

sdd runs each `claude` and `driver` round in a detached `sdd` process of its own, bounded by `verify.round_timeout`. The call that launches it returns at once, the round keeps running when the `sdd serve` that launched it exits, and the capture's next call, through any server, picks the findings up. A round starts once the round before it recorded its outcome. A round is a verifier failure when it fails, ends before its verifier delivered, ends its process without recording an outcome, runs past its timeout, or has no running process a minute after its launch. The capture puts the failure to you, naming the cause and the round's log file. Each round writes its output to `$XDG_STATE_HOME/sdd/rounds/<repository>/<verifier session>-round-<n>.log`, by default under `~/.local/state`. Abandoning the capture or ending the verifier's session stops a running round with the agent CLI it started.

A `driver` command reads one JSON request on stdin each round, `{"round": 1, "session": "<child session>", "instance": "<child instance>", "prompt": "<round prompt>", "driver_state": "<last round's state>"}`, starts or continues its agent with the prompt, and prints `{"driver_state": "<state for the next round>"}` as its last line once the round ends. The agent reports through the child session's `next` calls, never through the driver. A nonzero exit or a missing `driver_state` line fails the round. The driver runs in the round's process group: when the round's process ended without an outcome while the driver still runs, sdd stops the driver with that group.

#### Check timing

In a guided dialogue the agent plays the draft back without a check; once you confirm, the writing guide runs once on the content you approved, then the verifier. A finding that needs a revision needs your confirm again, and a later change gets a writing check of the changed lines only. With `dialogue.mode: auto` both run before playback.

#### Relevance prefilter

The prefilter judge rates how much each candidate bears on the draft, from `verify.prefilter_from` candidates on. It runs on a chat model the `llm_prefilter` block sets, with the keys of [`llm`](#language-model) that apply to a judge: `provider`, `model`, `endpoint`, `ollama_endpoint`, `api_keys`, `timeout`, `params`, `concurrency`, `rate_limit_rps` and `retry`:

```yaml
llm_prefilter:
  provider: openai
  model: gpt-5-mini
  timeout: 45s
  concurrency: 16
```

| Key | Default | Meaning |
| --- | --- | --- |
| `llm_prefilter.provider`, `llm_prefilter.model`, `llm_prefilter.endpoint`, `llm_prefilter.ollama_endpoint`, `llm_prefilter.api_keys.<provider>` | The `llm` value | The judge's model and connection. Each value the block sets wins over the `llm` one. |
| `llm_prefilter.timeout` | `45s` | Time limit for one judge call. |
| `llm_prefilter.concurrency` | `16` | Judge calls in flight; each call rates one candidate. |

The judge's identity is `generate:<model>`, and calibration keys its labels on it. A whole pass ends after 45 seconds; candidates whose call failed or came late stay unscored and go to judge. A candidate too long for one call is never cut: it goes to judge too.

Two dedicated judges rate a whole batch of candidates in one call instead of one chat call per candidate. `llm_prefilter.provider` picks them: `systemone`, Jev over the System One protocol, or `cohere`, Cohere rerank. The block then takes nothing from `llm`; it needs its own key under `llm_prefilter.api_keys.<provider>`:

```yaml
llm_prefilter:
  provider: systemone
  api_keys:
    systemone: <your-opper-key>
```

| Provider | Default `endpoint` | Default `model` | Direction | One call |
| --- | --- | --- | --- | --- |
| `systemone` | `https://api.opper.ai/v3/compat/v1/systemone` | `jev-1.14.0` | Both: one score answers the pair either way | Up to 281 candidates and 30,476 tokens of draft and candidates; each question comes on top |
| `cohere` | `https://api.opper.ai/v3/rerank` | `aws/cohere/rerank-v3.5` | Draft to candidate only | Up to 1,000 candidates, each within 4,096 tokens beside the draft and the instruction that leads the query |

`endpoint` and `model` point the judge at another host speaking the same protocol; `endpoint` must be an absolute http or https URL. `timeout` and `rate_limit_rps` apply as for a chat judge; `params`, `concurrency`, `ollama_endpoint` and `retry` belong to chat providers, and a dedicated judge refuses them. Only `llm_prefilter.provider` names a dedicated judge: `llm.provider` set to one is refused. The judge's identity is `<provider>:<model>`, calibrated on its own: a new judge stays uncalibrated until `sdd grounding rescore` scores the labelled pairs. A candidate that does not fit beside the draft in Cohere's window goes to judge uncut. A judge that answers 429 is called once more after the delay its `Retry-After` asks for, in seconds or as a date, plus up to a quarter more at random so calls refused together come back apart, where that wait fits in `timeout`. `llm.provider` set to a dedicated judge is refused by `sdd config set` too. Whole entry texts go to the judge's vendor. A missing key fails the prefilter's build, `sdd serve` included, naming the key; `sdd config set` refuses a provider outside the list.

Each judge gets thresholds of its own from the labels in the relevance ledger. The set-aside threshold is the highest score that keeps the 95% Wilson lower bound of recall at 0.98. Once the live labels alone hold that bound, the lower of two cuts sets aside, the one over every label or the one over the live labels: a replayed reference is easier to find than an entry a live reading had to find. The relevant threshold is the lowest score, never below the set-aside threshold, at which the Wilson lower bound of the positive share of the live labels reaches one half; replayed labels are positives only and take no part in it, and while the live labels reach that share at no score, nothing is served in full. A score below the set-aside threshold is set aside whatever the relevant threshold says. `sdd grounding status` names the labels each threshold rests on once replayed labels take part. Until the recall bound holds, the prefilter only sorts the briefs to judge and sets nothing aside. A blind audit returns 3% of the set-aside entries, rounded up, as ordinary briefs to judge, with no score shown on any line. After each publish, the drift rule looks at the latest 200 audit labels, once there are at least 50: where more than 2% of them, by weight, were referenced, it halves the set-aside threshold, at most once per judge in 24 hours, and never raises it.

| Command | What it does |
| --- | --- |
| `sdd grounding status [--exclude-replay]` | Shows the judge's labels, recall bound, thresholds, audit and drift state, the implicit dismissals and their flip rate, and how many hidden entries the deep verifier opened. `--exclude-replay` calibrates over live labels alone. With `verify.prefilter: false` it reports that no judge runs. |
| `sdd grounding rescore` | Has the current judge score every labelled pair it has not scored, after a judge change. |
| `sdd grounding seed --from <file\|->` | Appends a replayed reference label for each pair a JSON Lines file names as `{"a": "<entry ID>", "b": "<entry ID>"}`, skipping pairs already labelled. A bad line refuses the whole file. |

#### Shown-entry states

Every entry a capture's reading showed, served in full or printed as a brief to judge, ends the capture in one of four states: referenced (in the draft's refs, supersedes or closes), opened (served in full), dismissed explicitly (in `verdicts`), or dismissed implicitly, which sdd records at publish for every shown entry left without another state. An entry printed only as a set-aside ID or count gets no state. Each state is a calibration label weighted by how much it says: referenced 1, opened 0.5, an explicit dismissal 1, an implicit one 0.25. References are positive labels; dismissals coded `topic-only`, `other-scope` or `term-clash`, opened or not, and implicit ones are negative; `other` is counted only, and `covered`, `meta` and an opened entry without a code leave calibration. Calibration, the recall bound and the audit's drift share count labels by weight, so four implicit dismissals weigh as one explicit answer, and an audit sample left unanswered counts for the drift rule as an implicit dismissal. When a later entry references both an entry and a candidate that entry's capture dismissed implicitly, sdd flips that dismissal to a positive label at the later entry's publish.

#### Relevance ledger

The verdicts and scores behind that calibration form the relevance ledger, which stays on your machine: it lives in `~/.local/state/sdd/relevance/<repository>/`, or under `$XDG_STATE_HOME/sdd/relevance/` when that variable is set, keyed like the search index, and never in the repository. Each machine and clone builds its own. `gather` serves an entry in full where a past capture referenced it, and sets it aside where a past capture gave it any other state, so each pair is judged once. A corrupt ledger fails `gather`.

| Command | What it does |
| --- | --- |
| `sdd relevance export` | Writes the ledger as JSON Lines: reader verdicts, scores, then drift steps. Replayed labels carry `"source": "replay"`. |
| `sdd relevance import <file\|->` | Appends the records of a JSON Lines file, as `export` writes it. |
| `sdd relevance compact [--drop-replay]` | Merges the ledger into one segment. `--drop-replay` leaves the replayed labels out, taking a seed back; a pair a live capture also labels stays. |

### Recheck

| Key | File | Default | Meaning |
| --- | --- | --- | --- |
| `recheck.responsible` | Project | None | The participants a catch-up shows the open recheck items to. Empty shows them to everyone. |

The recheck list holds the strains captures reported between two entries, and hand edits: non-merge commits that change an entry file without the `sdd:` subject prefix. The first scan sets a baseline and ignores earlier history; sdd scans after every capture write. An item heals once a later entry cites every entry of it, or once one of its entries is superseded or closed.

| Command | What it does |
| --- | --- |
| `sdd recheck scan` | Records every hand edit committed since the baseline. |
| `sdd recheck list` | Shows the open items with a healing suggestion, or `No open recheck items.` |

### Events

sdd keeps project events, such as the recheck items, in an event store under `.sdd/events/`, one folder per channel.

Every write adds a file `<time>-<random>.jsonl` and never changes one, so files from two clones merge in git without conflicts. sdd merges a channel's small files into a larger one as they pile up. Each line holds one event as a JSON object: `id`, `type`, `time`, `writer`, `subject` (`uri`, `entries` or both) and the payload, as `payload` when it is JSON, else base64 in `payload_base64`. The folder names the channel. A file whose last line has no newline, or with a line that is no complete event, fails every read with an error naming the file, and no merge drops it. A field sdd does not know is ignored, and a merge keeps its line as written, so the lines a newer sdd writes in a `git` channel still read in an older one.

| Key | File | Default | Meaning |
| --- | --- | --- | --- |
| `events.channels.<name>.share` | Project | `local` | `local` keeps the channel on this machine, with a `.gitignore` in its folder. `git` commits it with the graph in the next sdd commit. |

| Command | What it does |
| --- | --- |
| `sdd events compact` | Merges each channel's files into one, holding every event once. Healed recheck items are dropped. |
| `sdd events export [--channel <name>]...` | Writes the events as JSON Lines ordered by time, every channel unless `--channel` names some. |
| `sdd events import <file\|->` | Validates every line first, then appends the events the store does not hold yet. It refuses a field it does not know, since it would drop it. |

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

Under [grounding](#grounding), a reading of a graph past `verify.brief_all_below` live entries also makes one call that writes its searches, and the prefilter judge runs on the `llm_prefilter` model.

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

Ollama cuts a prompt longer than its context size without an error, so sdd sets `num_ctx` on every call, sized from that call's prompt at 3 characters a token plus 8192 tokens for the answer and rounded up to a power of two, at least 16384: Ollama reloads the model whenever `num_ctx` changes, so calls of similar size share one value. `llm.context_tokens` does not change it.

Ollama Cloud does not work, because it ignores the response schema.

### Claude Code CLI

`claude-cli` is the fallback when no provider is set. It runs every call through `claude -p` with your Claude Code login, so it needs no API key, only the `claude` command on your `PATH`. Each call starts a new Claude Code process and answers only when its turn completes, so a call gets 5 minutes by default where other providers get 2. A long entry can still run past that: raise `llm.timeout` or set up another provider.

```bash
sdd config set llm.provider claude-cli
sdd config set llm.model claude-sonnet-4-6
```

```yaml
llm:
  provider: claude-cli
  model: claude-sonnet-4-6
```

sdd runs `claude -p` isolated:

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

`claude-cli` needs Claude Code 2.1.169 or later, the first version with `--safe-mode`. An older `claude` stops with `unknown option`, and sdd shows that message.

If you ran `claude-cli` on an exported `ANTHROPIC_API_KEY` without a Claude Code login, calls now fail with `Not logged in`. Sign in to Claude Code with `/login`, or set `llm.claude_cli.credentials: env`. With one of the two file-borne routes above, the calls run on it instead of failing. Any other `ANTHROPIC_*` or `CLAUDE_*` variable a setup relied on needs `llm.claude_cli.credentials: env` too.

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

## Environment variables

| Variable | Default | Effect |
| --- | --- | --- |
| `XDG_CONFIG_HOME` | `~/.config` | The user-global file is `$XDG_CONFIG_HOME/sdd/config.yaml`. |
| `XDG_CACHE_HOME` | `~/.cache` | Search indexes and clones of connected repositories live under `$XDG_CACHE_HOME/sdd`. |
| `XDG_STATE_HOME` | `~/.local/state` | Session logs, the relevance ledger and the logs of verifier rounds live under `$XDG_STATE_HOME/sdd`. Must be an absolute path. |
| `SDD_SERVE_TOKEN` | None | The bearer token `sdd serve --transport http` requires on every request. Same as `--auth-token`. |
