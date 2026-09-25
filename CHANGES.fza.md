# Local changes

Changes carried on top of upstream `networkteam/sdd`. The baseline is upstream `v0.17.0` (merge base `b569dcf6`). Nothing listed here is in upstream `main`.

Section headings are local build stamps, matching the version string the binary reports (`sdd --version`).

## 0.17.0+fza5

### Added

**A graph in a repository of its own.** `graph_dir` already accepted an absolute path, so the entry files could live beside the project, but every git operation ran in the process working directory: a capture from the project repository staged a path outside it and failed with `fatal: … is outside repository`. Git operations now follow the paths they act on. `git.CLI` and `git.RemovalCommitter` carry a `Dir`, prefixed as `-C` on every ambient operation, and `git.RepoRootFor(path)` resolves the repository holding a path the process is not in.

The composition root picks the repository per target. Entry, summary, rewrite and WIP-marker commits, plus `sdd sync --pull` and the background sync check, run in the repository holding the graph. `sdd init` commits skills and metadata, `sdd repo` writes connected-repo config, and `sdd wip start` branches, all in the project's own checkout. `repos.Manager` clone and pull are untouched, since those methods address their target through arguments.

This keeps the project's history free of `sdd: …` commits, which is why a separate graph repository exists at all. A graph inside the project's own repository remains the default and is unchanged: the resolved repository is then the project's, and an absolute commit pathspec behaves exactly as the working directory did.

`sdd serve` refuses to start when the graph sits in another repository, rather than losing commits quietly: the engine's write path acquires a worktree of the served checkout and commits the graph inside it (`meta.ResolveGraphDir(checkout, cfg)` in `newLocalMutationTargets`), so an absolute `graph_dir` would be written to disk and committed nowhere. The CLI capture path is the supported route for this arrangement.

**Graph identity follows the graph.** `sdd init` derives `repo_id` and `default_branch` from the repository holding the graph rather than from the checkout it runs in, because both are facts about the graph: other graphs reference entries under that identity, and the graph is published on that repository's branch. Deriving them from the project's remote announced a graph under the identity of a repository that does not contain it, so a cross-repo reference resolved to the wrong clone. The in-repo default is unchanged, since there the two repositories are one.

`git.RepoRootFor` resolves through a path that does not exist yet, so a graph directory named in config before it is created still names its repository.

Accepted cost: the derived values are committed in the project's config as well as in the graph repository's own config, and the two drift silently when the graph repository is renamed or its branch changes. Reading graph facts from the graph repository's config instead would remove the duplication at the price of a second committed config to resolve on every invocation.

Also required for cross-repo reachability, and unchanged: the graph repository carries its own committed `.sdd/config.yaml` with a `graph_dir` relative to itself, because `repos.GraphDir` resolves a cached repo's graph inside the clone (`internal/repos/cache.go:60`).

Files: `internal/git/git.go`, `internal/git/sync.go`, `cmd/sdd/main.go`, `cmd/sdd/sync.go`, `cmd/sdd/serve.go`, `README.md`.

Tests: `internal/git/git_test.go` (commit, removal commit and ambient operations target the configured repository; `RepoRootFor`), `cmd/sdd/sidecar_test.go` (an end-to-end capture from the project repository lands in the graph repository and leaves the project's history untouched; the in-repo default lands its capture in the project's own history; `sdd serve` refuses the separate-repository arrangement).

## 0.17.0+fza3

Pre-flight reliability across every endpoint, plus the provider surface it needs.

### Delivery shape

Four self-contained commits, each written with upstream naming and conventions, tests included, nothing fork-specific, so any one of them cherry-picks onto `origin/main` without rework. Targeting upstream first is rejected: it leaves a broken pre-flight in place until a review lands and invites reopening settled decisions. Building fork-shaped is rejected too, because the divergence falls on `pkg/llm`, `LLMConfig` and `internal/llmops`, the surfaces upstream changes most. Accepted cost: a unit upstream does not want was still shaped for a reviewer who never arrives.

### gollm pinned to the fork carrying the OpenAI-compatible base URL

`replace github.com/teilomillet/gollm` points at `github.com/fza/gollm v0.0.0-20260924204021-a90258ddef9f`, one commit past `networkteam/gollm`'s `generate-with-usage` branch. The bump moves the pin three commits forward from `f6f84ac` and brings `config.OpenAIEndpoint`, the `SetOpenAIEndpoint` option, `OpenAIProvider.SetEndpoint` applied from config in `SetDefaultOptions`, the shared `providers/endpoint.go` helper that `vllm.go` now uses, and a `llm/validate.go` fix so a missing endpoint field no longer skips the OpenAI key check. Provider-aware retry and per-call usage reporting were already in the pin.

The endpoint feature needed no patch: it exists in the org fork with its own tests, so `llm.endpoint` needs only a `SetOpenAIEndpoint` call on the sdd side.

One patch does sit on top, as `fix/mistral-system-prompt`. `MistralProvider.PrepareRequest` copied every option into the request body verbatim, `system_prompt` among them, and the Mistral API rejects unknown body fields, so every call carrying a system prompt failed with `422 extra_forbidden`. The system prompt now travels as a system-role message on both the plain and the schema request path, matching what the OpenAI provider already did. Tests cover the per-call value, the provider default, and the absence of a system prompt.

The mirror to a personally owned fork is deliberate: the pin cannot be moved out from under this line. Pinning `networkteam/gollm` directly was rejected because the pin would name a feature-branch head in a shared repository, and waiting for that branch to merge into its `main` would block the endpoint work on a review scheduled elsewhere. Accepted cost: two forks to track when upstream moves, and contributing the same tree back to `networkteam` later means moving the pin twice.

### Pre-flight and writing-guide verdict extraction

Both JSON checks ask a single LLM call to reason and to emit strict machine-readable output at once. Weaker models put a prose conclusion such as `no finding` into the `severity` field, and `parsePreflightResult` rejects the whole response on the first unparseable finding, so every other finding is lost and the capture aborts. The only way through is `--skip-preflight`, which annotates a good entry as validated by nobody. Observed on `mistral-large-2512` and `claude-sonnet-5`, rare on `claude-sonnet-4-6`.

Six parts:

**A lazy extraction call, shared by both JSON checks.** On a parse failure the runner is called once more with a short prompt carrying the unparseable output verbatim plus the severity scale, asking for the JSON object alone. Roughly 130 words, no task rubric, no entry, no graph context. Unconditional on every provider, with no config key. When the second response also fails to parse, the error names both attempts.

**Native schema output where the wire key is known.** `internal/llm/factory` holds a mux that builds one gollm client per purpose and dispatches on `req.Purpose`, composed as `Bounded(rateLimited(mux))` so the configured rate limit stays one bucket. The pre-flight and writing-guide clients carry the findings schema as `response_format` for openai and mistral and as `format` for ollama, set through `client.SetOption`, which both providers merge into the request body. The summarize client carries none, because it returns prose. Anthropic and claude-cli send no schema.

**An endpoint setting for the chat axis.** `model.LLMConfig` carries `endpoint`, matching `EmbeddingConfig.Endpoint` in name and meaning, and `factory.buildProvider` accepts gollm's native `mistral` provider, with matching entries in `isRemote` and `providerDefaultRPS` so Mistral traffic is rate limited like any other remote provider. Reaching Mistral as `provider: openai` with an endpoint is rejected: stats rows would label the traffic `openai` and a Mistral key would live under `api_keys.openai`. Accepted cost: three gates in the factory must stay in step for every provider added this way, and a missed `isRemote` entry silently removes rate limiting. The pinned fork supplies the configurable base URL for the OpenAI-compatible provider. Together these reach `api.mistral.ai` and any self-hosted OpenAI-compatible server directly, with no external proxy in between.

**Two purposes in `pkg/llm`.** `preflight-extract` and `writing-guide-extract`, which the mux routes to the parent purpose's client. They separate the two calls in `.sdd/stats/llm.jsonl`, which makes the per-endpoint slip rate readable.

**Schemas reflected from tagged structs.** One named struct per check carries `jsonschema` tags, is reflected into the schema sent as `response_format`, and is the unmarshal target, so schema and parser cannot drift by construction. This follows the pattern already used for MCP tool surfaces, and promotes `invopop/jsonschema` from an indirect to a direct dependency. A hand-written schema literal guarded by a drift test is rejected: it declares the same shape twice and makes the test load-bearing. The reflector already emits the closed objects, required fields and enums a strict validator wants; only its `$schema` declaration is dropped. Accepted cost: the exact wire shape is one step removed from what the struct states.

**A `severity_scale` partial in `shared_templates`.** Included by `verdict.tmpl` and by the extractor, so the scale is stated once.

Every response shape that lost a capture is pinned as a fixture in `observedMalformations`: a conclusion in the `severity` field, `none` as a severity, an empty severity, an empty category, prose with no JSON, and one bad finding among good ones. Each asserts the capture survives on exactly one fallback call, so a parser or rubric change that stops rescuing one fails a test instead of a capture. A transport failure on the first call spends no second call, because the fallback reformats an answer rather than retrying a request.

The live eval prints a verdict-extraction rate per identity, counting checker calls against extraction calls. That figure is the per-endpoint reliability measure: an identity that never slips costs one call per check, one that slips often costs two and risks failing both.

The parsers stay exactly as they are. `parseSeverity`, the empty-field checks and the abort-on-first-bad-finding loop are unchanged, so a checker that has stopped working still fails loudly.

The extraction call runs on the same identity as the first call. It fires only after a parse failure, so the doubled cost is bounded and rare, and one identity keeps the stats rows comparable. A configurable `extract_model` is rejected as a second key for the same rare call, and a per-purpose model map, though nearly free once the mux exists, is scope beyond this failure.

The extraction call is bounded separately. `llm.extract_timeout` and a `--preflight-extract-timeout` flag carry it, defaulting to 60 seconds, applied per purpose where the mux already dispatches. `Bounded` starts a fresh deadline on every `Run`, so without a second bound a slipping endpoint spends twice the configured timeout on one check, which is 12 minutes at a `timeout: 6m` configuration.

Rejected for the extraction bound: leaving each call on the full configured timeout, which doubles the worst case; deriving the bound as a fraction of the configured timeout, which invents a number nobody set and can time out an extraction whose phase 1 would have survived; and one shared budget per check, which leaves seconds for the fallback exactly when a slow phase 1 makes it necessary and pushes the deadline from the runner into llmops, against the rule that an instance bounds its own calls.

Rejected alternatives:

- A config key gating the extraction call. With the call firing only on a parse failure, a healthy endpoint never pays for it, so the key would add config surface and a second path to keep calibrated for no gain.
- Parser tolerance in any shape: accepting a declared `none` severity and dropping those findings, salvaging the findings that parse while reporting the rest as a checker malfunction, or dropping findings whose prose withdraws them. The withdrawal guard was proposed in `20260610-000139-s-prc-xr9` and replaced by the deterministic matrix in `20260610-233200-d-tac-tph`, and every tolerant shape can hide a checker that has stopped working.
- Native schema output as the only mechanism. The ollama transport and claude-cli have no schema channel, and gollm's anthropic schema path replaces the system message with the schema, which would destroy the byte-stable cacheable preamble.
- A fork patch carrying per-call request options, which would allow one client instead of a mux. It adds a divergence to maintain and blocks shipping on a fork release plus a `go.mod` bump, while the mux realizes the per-purpose routing that `20260830-234501-d-cpt-q6n` anticipated and `20260901-093840-s-tac-ahc` asked for.
- Two other extractor shapes. Replaying the original prompts doubles input tokens on the failure path and lets the model reach a verdict different from the one being extracted. An extractor with no severity scale has nothing to map an ambiguous prose verdict onto.
- Per-provider endpoint fields, and a new `openai-compatible` provider name over gollm's vllm provider. `EmbeddingConfig` already settled this vocabulary with a single `endpoint` field, and the vllm route hand-rolls the authorization header and bypasses per-provider request fixes such as `max_completion_tokens`.
- JSON escaping rules in the extraction prompt, dropped to keep it near 130 words. The schema makes malformed JSON impossible on openai, mistral and ollama, so the exposure is limited to anthropic and claude-cli.

Accepted costs: endpoints that slip pay two calls per check, so latency and input tokens roughly double on the failure path; three gollm clients live per process; `networkteam/gollm` carries one more divergence for the endpoint override; the closed `Purpose` vocabulary in `pkg/llm` grows by two values; an endpoint that fails both phases still blocks the capture.

Acceptance criteria:

- [x] A first response carrying `severity: "no finding"` produces a successful capture whose findings come from the extraction call, asserted with an `llm.RunnerFunc` double and no network.
- [x] A first response that parses makes exactly one call, asserted by call count.
- [x] Both responses unparseable produces an error naming both attempts, the entry file stays on disk, and no `preflight:` annotation is written.
- [x] Summarize never receives a `response_format` or `format` option. Pre-flight and writing-guide always carry one on openai, mistral and ollama.
- [x] The configured rate limit is observed across purposes, not once per client.
- [x] `llm.endpoint` with `provider: mistral` reaches the configured base URL, and `sdd config` reports its provenance.
- [x] `.sdd/stats/llm.jsonl` distinguishes an extraction call from a first call.
- [x] No finding that parses is dropped or downgraded anywhere in the parsers.
- [x] The observed failure payloads are pinned as eval fixtures before any rubric wording changes, and the live eval reports how often the extraction call fired per identity.

Out of scope: pre-warmed agent processes, covered under isolated claude-cli spawning below, which takes the isolation without the warmth; graph-resident calibration, which stays untouched.

### Pre-flight bypass provenance

`Entry.Preflight` is a closed set of two values, declared as `model.PreflightSkipped` and `model.PreflightDryRunVerified`:

- `skipped` — the author bypassed the check, written by `--skip-preflight`.
- `dry-run-verified` — findings were settled by a prior `--dry-run` pass, written by `--preflight-verified`, which stays silent on stderr.

Absent means a checker ran and raised nothing blocking. The two values separate a review dodged from findings already settled, because `skipped` misstates a capture a dry-run did validate. The field's doc comment names them; no code writes the `error` value it used to promise.

`sdd show` displays the value as an omitempty field in its YAML envelope, so provenance is visible where an entry is read. The 58 entries carrying a bare `skipped` keep their meaning.

The entry line shared by `sdd view` and `sdd search` is left alone. Its slot order is documented as fixed at `internal/presenters/presenters.go:22` and skills parse it, so a new always-on slot would change a parsed format and put the marker into every listing naming one of those 58 entries. A `sdd view` filter is rejected as well: it only helps someone already looking. Bulk lookup is `grep -rl '^preflight:' .sdd/graph`.

No lint check counts bypassed entries. Advisories never fail `sdd lint`, and entries are immutable, so a per-entry advisory would add 58 permanent informational lines that train readers to skip the section, while an aggregate count names no entry and answers nothing the grep does not. Accepted cost: a rising bypass rate stays invisible unless someone looks.

Rejected alternatives:

- A free-text reason written verbatim from the flag. Nothing can aggregate or check unbounded prose in frontmatter, the flag stops being a bare boolean for every script and skill that passes it, and it demands a sentence at the moment the author is trying to get through.
- Leaving `--preflight-verified` traceless. An entry nobody checked and an entry a dry-run cleared then read identically.

A third value for a failed checker is rejected. A pre-flight infrastructure error aborts the capture before the entry file is written (`internal/handlers/handler_new_entry.go:163`), so nothing exists to annotate, and every way to write such a value costs more than the distinction is worth: a `--preflight-optional` flag adds a third pre-flight flag and a third exclusivity pair; a value on `--skip-preflight` records an author assertion that sdd never verified; letting pre-flight errors stop blocking is the silent-fallback shape AGENTS.md forbids. Accepted cost: an entry captured because the checker was broken reads identically to one captured to dodge a review.

### Isolated claude-cli spawning

`internal/llm/claude` spawns `claude -p` with neither `cmd.Dir` nor `cmd.Env` set, so every call inherits the invocation's working directory and its whole environment. Inside a project tree that loads the project's instruction files, skills, hooks and configured servers: the same two-token prompt measures 57,716 tokens there against 21,466 with the agent's own isolation flag set. An inherited `ANTHROPIC_API_KEY`, `ANTHROPIC_AUTH_TOKEN` or `ANTHROPIC_BASE_URL` also makes the agent answer on that credential instead of the signed-in account, which moves the bill from the subscription to the API.

The runner spawns `claude -p --safe-mode` in a temporary working directory it creates and removes per call, with the Anthropic credential variables removed from the child environment. No process outlives the invocation, so the CLI stays short-lived and sdd takes on no supervision.

Rejected alternatives:

- A warm process pool inside `sdd serve`, which already outlives invocations. Only agents driving sdd over MCP would benefit, `sdd new` from a terminal would still spawn cold, and the server would hold spare processes for projects nobody is touching.
- A dedicated daemon subcommand holding the pool behind a unix socket. It buys warmth for every caller at the price of owning process supervision, a socket protocol, retention and concurrency settings, and a second daemon beside `sdd serve`.
- An OpenAI-compatible endpoint on `sdd serve` backed by a pool, reached through `llm.endpoint`. The dialogue server would grow an unrelated proxy surface with its own auth path, and pointing `llm.endpoint` at sdd itself makes a configuration loop possible.

Accepted cost: process startup stays on every call, measured at roughly 0.5 seconds (2.57-3.30s cold against 2.05-2.36s warm). Warmth stays the job of an external proxy, which `llm.endpoint` reaches.

Acceptance criteria:

- [x] The claude-cli runner passes `--safe-mode`.
- [x] The child process runs in a temporary directory created per call and removed afterwards, never the invocation's working directory.
- [x] `ANTHROPIC_API_KEY`, `ANTHROPIC_AUTH_TOKEN`, `ANTHROPIC_BASE_URL`, `CLAUDE_CODE_USE_BEDROCK` and `CLAUDE_CODE_USE_VERTEX` are absent from the child environment, asserted by a test over the built command.
- [x] Reported context for a claude-cli pre-flight call in `.sdd/stats/llm.jsonl` sits in the isolated range: a full pre-flight measures 3 input tokens with 13,382 cache-read and 13,177 cache-create, so roughly 26.6k of context against the 57,716 a project-loaded call carries for a two-token prompt.

The scrub is unconditional. The `claude-cli` provider answers on the signed-in subscription, so an inherited environment key that silently reroutes the bill is the defect, not a configuration. API-key authentication has a better path already: `provider: anthropic` with the same key under `llm.api_keys`, which spawns no process and gets prompt caching. A config switch for the scrub is rejected, because its off state reinstates the billing reroute it exists to prevent. Accepted cost: a setup whose only claude-cli authentication is `ANTHROPIC_API_KEY` fails with an authentication error until it moves to `provider: anthropic`.

## 0.17.0+fza2

### Added

**`--local-config <path>`, a global flag.** Points the machine-local config layer at a file outside the checkout for the whole invocation, in place of `.sdd/config.local.yaml`. A sandbox, a CI runner or a second identity can supply its own layer without writing into the repository.

The passed file keeps the same slot in the config overlay (user-global base, then the committed `.sdd/config.yaml`, then the machine-local layer, then CLI flags), parses as the same `PerRepoConfig` schema, and reports as source `local` in `sdd config` provenance output.

```bash
sdd --local-config ~/ci-local.yaml config get participant
sdd --local-config ~/ci-local.yaml config set --local participant Ada
sdd --local-config ~/ci-local.yaml init
```

Reads and writes move together. `sdd config set --local`, `sdd config unset --local`, and the participant that `sdd init` records all target the passed file, so one invocation resolves and mutates the same layer.

Two alternatives were rejected:

- Redirecting reads while leaving writes on `.sdd/config.local.yaml`. A value written with `--local` would then be invisible to the invocation that passed the flag.
- Redirecting reads and failing `--local` writes while the flag is set. This adds a special-case rule and leaves `sdd init` unable to record a participant.

Accepted cost: the flag carries write authority, and a mistyped path is not an error. It resolves as an absent layer on reads and is created with mode 0600 on first write.

Edge behavior: outside a `.sdd/` directory the passed file still overlays the user-global config, so the flag names a file rather than a location inside a checkout. With neither a `.sdd/` directory nor the flag there is no machine-local layer at all, which keeps an empty directory from reading `config.local.yaml` relative to the current working directory.

`meta.LocalConfigPath` is the single resolution point. It is threaded through `meta.ReadConfig`, `meta.ResolveConfig` and `meta.ReadConfigLayers` on the read side, through `query.EffectiveConfigQuery` and `query.UnknownConfigKeysQuery` for the provenance and unknown-key surfaces, and through `handlers.Options.LocalConfigPath` and `command.InitCmd.LocalConfigPath` on the write side. The CLI parses the root flag once into an absolute path, because config resolution runs in helpers that hold no parsed command.

Files: `internal/meta/meta.go`, `internal/query/config.go`, `internal/finders/config.go`, `internal/handlers/handler.go`, `internal/handlers/configfile.go`, `internal/handlers/handler_init.go`, `internal/command/init.go`, `cmd/sdd/main.go`, `cmd/sdd/config.go`, `cmd/sdd/repo.go`, `README.md`.

Tests: `internal/meta/localconfig_test.go`, `cmd/sdd/localconfig_test.go`, and `TestConfigSet_LocalOverrideWritesOverrideFile` in `internal/handlers/handler_config_test.go`.

## 0.17.0+fza1

### Added

**`sdd new --preflight-verified`.** Skips the pre-flight LLM call and records `preflight: dry-run-verified`, with no stderr warning. It is for the real capture of an entry whose findings a `--dry-run` pass already settled.

Pre-flight is an LLM call and non-deterministic, so a second run over identical text can raise findings the dry-run loop already cleared, sending the capture through another revision round for no gain. `--skip-preflight` records the bypass in the entry, which misstates a capture that was in fact validated.

```bash
sdd new decision tactical "..." --dry-run      # settle findings
sdd new decision tactical "..." --preflight-verified
```

Mutually exclusive with `--skip-preflight`. Passing both is an error, since the two record different things: a review dodged against findings already settled. The flag is only correct when the dry-run covered the same text; a revised entry needs a fresh dry-run.

Files: `cmd/sdd/main.go`, `internal/command/new_entry.go`, `internal/handlers/handler_new_entry.go`, `internal/bundledskills/templates/sdd/SKILL.md.tmpl`, `internal/bundledskills/templates/sdd/references/cli-reference.md.tmpl`, and the installed skill renders under `.claude/skills/` and `.agents/skills/`.

Tests: `internal/handlers/handler_new_entry_test.go`.

## Fixes

None. Every local change is additive.
