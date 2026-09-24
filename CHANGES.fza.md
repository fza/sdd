# Local changes

Changes carried on top of upstream `networkteam/sdd`. The baseline is upstream `v0.17.0` (merge base `b569dcf6`). Nothing listed here is in upstream `main`.

Section headings are local build stamps, matching the version string the binary reports (`sdd --version`).

## Planned

Design settled, not yet implemented.

## Implementation order

1. `llm.endpoint` with the `SetOpenAIEndpoint` call, the per-purpose mux, and the two schemas. Schema-enforced verdicts on an OpenAI-compatible provider may end the parse failures without the extraction call existing.
2. The lazy extraction call with `extract_timeout` and the two new purposes, as the net for anthropic, claude-cli and any transport with no schema channel.
3. Isolated claude-cli spawning.
4. The two-value bypass annotation with its show envelope field.

### gollm pinned to the fork carrying the OpenAI-compatible base URL

`replace github.com/teilomillet/gollm` points at `github.com/fza/gollm v0.0.0-20260919121334-aa9e2d1faebc`, mirrored from `networkteam/gollm`'s `generate-with-usage` branch. The bump moves the pin three commits forward from `f6f84ac` and brings `config.OpenAIEndpoint`, the `SetOpenAIEndpoint` option, `OpenAIProvider.SetEndpoint` applied from config in `SetDefaultOptions`, the shared `providers/endpoint.go` helper that `vllm.go` now uses, and a `llm/validate.go` fix so a missing endpoint field no longer skips the OpenAI key check. Provider-aware retry and per-call usage reporting were already in the pin.

No patch was written. The endpoint feature exists upstream in the org fork with its own tests, so `llm.endpoint` needs only a `SetOpenAIEndpoint` call on the sdd side.

The mirror to a personally owned fork is deliberate: the pin cannot be moved out from under this line. Pinning `networkteam/gollm` directly was rejected because the pin would name a feature-branch head in a shared repository, and waiting for that branch to merge into its `main` would block the endpoint work on a review scheduled elsewhere. Accepted cost: two forks to track when upstream moves, and contributing the same tree back to `networkteam` later means moving the pin twice.

### Pre-flight and writing-guide verdict extraction

Both JSON checks ask a single LLM call to reason and to emit strict machine-readable output at once. Weaker models put a prose conclusion such as `no finding` into the `severity` field, and `parsePreflightResult` rejects the whole response on the first unparseable finding, so every other finding is lost and the capture aborts. The only way through is `--skip-preflight`, which annotates a good entry as validated by nobody. Observed on `mistral-large-2512` and `claude-sonnet-5`, rare on `claude-sonnet-4-6`.

Five parts:

**A lazy extraction call, shared by both JSON checks.** On a parse failure the runner is called once more with a short prompt carrying the unparseable output verbatim plus the severity scale, asking for the JSON object alone. Roughly 130 words, no task rubric, no entry, no graph context. Unconditional on every provider, with no config key. When the second response also fails to parse, the error names both attempts.

**Native schema output where the wire key is known.** `internal/llm/factory` gains a mux that builds one gollm client per purpose and dispatches on `req.Purpose`, composed as `Bounded(rateLimited(mux))` so the configured rate limit stays one bucket. The pre-flight and writing-guide clients carry the findings schema as `response_format` for openai and mistral and as `format` for ollama, set through `client.SetOption`, which both providers merge into the request body. The summarize client carries none, because it returns prose. Anthropic and claude-cli send no schema.

**An endpoint setting for the chat axis.** `model.LLMConfig` gains `endpoint`, matching `EmbeddingConfig.Endpoint` in name and meaning, and `factory.buildProvider` accepts gollm's native `mistral` provider, with matching entries in `isRemote` and `providerDefaultRPS` so Mistral traffic is rate limited like any other remote provider. Reaching Mistral as `provider: openai` with an endpoint is rejected: stats rows would label the traffic `openai` and a Mistral key would live under `api_keys.openai`. Accepted cost: three gates in the factory must stay in step for every provider added this way, and a missed `isRemote` entry silently removes rate limiting. A patch in `networkteam/gollm` adds a configurable base URL for the OpenAI-compatible provider family. Together these reach `api.mistral.ai` and any self-hosted OpenAI-compatible server directly, with no external proxy in between.

**Two purposes in `pkg/llm`.** `preflight-extract` and `writing-guide-extract`, which the mux routes to the parent purpose's client. They separate the two calls in `.sdd/stats/llm.jsonl`, which makes the per-endpoint slip rate readable.

**Schemas reflected from tagged structs.** One named struct per check carries `jsonschema` tags, is reflected into the schema sent as `response_format`, and is the unmarshal target, so schema and parser cannot drift by construction. This follows the pattern already used for MCP tool surfaces, and promotes `invopop/jsonschema` from an indirect to a direct dependency. A hand-written schema literal guarded by a drift test is rejected: it declares the same shape twice and makes the test load-bearing. Accepted cost: the reflected output needs post-processing for OpenAI strict mode, so the exact wire shape is one step removed from what the struct states.

**A `severity_scale` partial in `shared_templates`.** Included by `verdict.tmpl` and by the extractor, so the scale is stated once.

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

- [ ] A first response carrying `severity: "no finding"` produces a successful capture whose findings come from the extraction call, asserted with an `llm.RunnerFunc` double and no network.
- [ ] A first response that parses makes exactly one call, asserted by call count.
- [ ] Both responses unparseable produces an error naming both attempts, the entry file stays on disk, and no `preflight:` annotation is written.
- [ ] Summarize never receives a `response_format` or `format` option. Pre-flight and writing-guide always carry one on openai, mistral and ollama.
- [ ] The configured rate limit is observed across purposes, not once per client.
- [ ] `llm.endpoint` with `provider: mistral` reaches the configured base URL, and `sdd config` reports its provenance.
- [ ] `.sdd/stats/llm.jsonl` distinguishes an extraction call from a first call.
- [ ] No finding that parses is dropped or downgraded anywhere in the parsers.
- [ ] The observed failure payloads are pinned as eval fixtures before any rubric wording changes, and the live eval reports how often the extraction call fired per identity.

Out of scope: pre-warmed agent processes with a neutral working directory and a scrubbed environment, taken on by its own plan below; the annotation surface of `--skip-preflight` and `--preflight-verified`, decided separately; graph-resident calibration, which stays untouched.

### Pre-flight bypass provenance

`Entry.Preflight` takes one value today, `skipped`, written by `--skip-preflight` and carried on 58 entries in this graph. No presenter, lint check, view or MCP surface reads it, so a bypass is invisible unless someone opens the file. `--preflight-verified` records nothing at all, and the doc comment on the field promises an `error` value that no code writes.

The field becomes a closed set of two values, and a read surface shows it:

- `skipped` — the author bypassed the check, written by `--skip-preflight`.
- `dry-run-verified` — findings were settled by a prior `--dry-run` pass, written by `--preflight-verified`.

The doc comment on the field is corrected to match. No code writes the `error` value it currently promises.

`sdd show` displays the value as an omitempty field in its YAML envelope. The 58 entries carrying a bare `skipped` stay valid, since the value keeps its meaning.

The entry line shared by `sdd view` and `sdd search` is left alone. Its slot order is documented as fixed at `internal/presenters/presenters.go:22` and skills parse it, so a new always-on slot would change a parsed format and put the marker into every listing naming one of the 58 entries. A `sdd view` filter for bypassed entries is also rejected for now: it only helps someone already looking, and bulk discovery belongs to a lint check.

`--preflight-verified` recording `dry-run-verified` replaces its original no-trace behavior. The reason that behavior avoided an annotation was that `skipped` misstates a capture which was in fact validated. A distinct value states it accurately instead, so the annotation is no longer a misstatement.

Rejected alternatives:

- A free-text reason written verbatim from the flag. Nothing can aggregate or check unbounded prose in frontmatter, the flag stops being a bare boolean for every script and skill that passes it, and it demands a sentence at the moment the author is trying to get through.
- A read surface over the existing single value. The three cases stay indistinguishable and `--preflight-verified` stays traceless.
- Leaving the field alone. The marks stay unreadable without grepping the files.

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

- [ ] The claude-cli runner passes `--safe-mode`.
- [ ] The child process runs in a temporary directory created per call and removed afterwards, never the invocation's working directory.
- [ ] `ANTHROPIC_API_KEY`, `ANTHROPIC_AUTH_TOKEN`, `ANTHROPIC_BASE_URL`, `CLAUDE_CODE_USE_BEDROCK` and `CLAUDE_CODE_USE_VERTEX` are absent from the child environment, asserted by a test over the built command.
- [ ] Reported input tokens for a claude-cli pre-flight call in `.sdd/stats/llm.jsonl` drop against the pre-change figure.

No lint check counts bypassed entries. `sdd lint` reports no issues on this graph today, advisories never fail the command, and entries are immutable, so a per-entry advisory would add 58 permanent informational lines that train readers to skip the section. An aggregate count is rejected too, since it names no entry and `grep -rl '^preflight:' .sdd/graph` already answers the same question. Accepted cost: a rising bypass rate stays invisible unless someone looks.

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

**`sdd new --preflight-verified`.** Skips the pre-flight LLM call and leaves no trace: no `preflight:` frontmatter annotation and no stderr warning. It is for the real capture of an entry whose findings a `--dry-run` pass already settled.

Pre-flight is an LLM call and non-deterministic, so a second run over identical text can raise findings the dry-run loop already cleared, sending the capture through another revision round for no gain. `--skip-preflight` records the bypass in the entry, which misstates a capture that was in fact validated.

```bash
sdd new decision tactical "..." --dry-run      # settle findings
sdd new decision tactical "..." --preflight-verified
```

Mutually exclusive with `--skip-preflight`. Passing both is an error, since one records the bypass and the other does not. The flag is only correct when the dry-run covered the same text; a revised entry needs a fresh dry-run.

Files: `cmd/sdd/main.go`, `internal/command/new_entry.go`, `internal/handlers/handler_new_entry.go`, `internal/bundledskills/templates/sdd/SKILL.md.tmpl`, `internal/bundledskills/templates/sdd/references/cli-reference.md.tmpl`, and the installed skill renders under `.claude/skills/` and `.agents/skills/`.

Tests: `internal/handlers/handler_new_entry_test.go`.

## Fixes

None. Every local change is additive.
