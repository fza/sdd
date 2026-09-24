# Local changes

Changes carried on top of upstream `networkteam/sdd`. The baseline is upstream `v0.17.0` (merge base `b569dcf6`). Nothing listed here is in upstream `main`.

Section headings are local build stamps, matching the version string the binary reports (`sdd --version`).

## Planned

Design settled, not yet implemented.

### Pre-flight and writing-guide verdict extraction

Both JSON checks ask a single LLM call to reason and to emit strict machine-readable output at once. Weaker models put a prose conclusion such as `no finding` into the `severity` field, and `parsePreflightResult` rejects the whole response on the first unparseable finding, so every other finding is lost and the capture aborts. The only way through is `--skip-preflight`, which annotates a good entry as validated by nobody. Observed on `mistral-large-2512` and `claude-sonnet-5`, rare on `claude-sonnet-4-6`.

Five parts:

**A lazy extraction call, shared by both JSON checks.** On a parse failure the runner is called once more with a short prompt carrying the unparseable output verbatim plus the severity scale, asking for the JSON object alone. Roughly 130 words, no task rubric, no entry, no graph context. Unconditional on every provider, with no config key. When the second response also fails to parse, the error names both attempts.

**Native schema output where the wire key is known.** `internal/llm/factory` gains a mux that builds one gollm client per purpose and dispatches on `req.Purpose`, composed as `Bounded(rateLimited(mux))` so the configured rate limit stays one bucket. The pre-flight and writing-guide clients carry the findings schema as `response_format` for openai and mistral and as `format` for ollama, set through `client.SetOption`, which both providers merge into the request body. The summarize client carries none, because it returns prose. Anthropic and claude-cli send no schema.

**An endpoint setting for the chat axis.** `model.LLMConfig` gains `endpoint`, matching `EmbeddingConfig.Endpoint` in name and meaning, and `factory.buildProvider` accepts gollm's native `mistral` provider. A patch in `networkteam/gollm` adds a configurable base URL for the OpenAI-compatible provider family. Together these reach `api.mistral.ai` and any self-hosted OpenAI-compatible server directly, with no external proxy in between.

**Two purposes in `pkg/llm`.** `preflight-extract` and `writing-guide-extract`, which the mux routes to the parent purpose's client. They separate the two calls in `.sdd/stats/llm.jsonl`, which makes the per-endpoint slip rate readable.

**A `severity_scale` partial in `shared_templates`.** Included by `verdict.tmpl` and by the extractor, so the scale is stated once.

The parsers stay exactly as they are. `parseSeverity`, the empty-field checks and the abort-on-first-bad-finding loop are unchanged, so a checker that has stopped working still fails loudly.

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

### Agent processes inside sdd

Own plan, not yet drafted. `internal/llm/claude` spawns `claude -p` per call inside the project tree, so every call pays process startup and the project's own instruction files, skills, hooks and servers. An external Ollama-compatible proxy currently covers this by keeping a started process ready and starting it somewhere neutral. What of that belongs in sdd, and in what shape, is undecided.

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
