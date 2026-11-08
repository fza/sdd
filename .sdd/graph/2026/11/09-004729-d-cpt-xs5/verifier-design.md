# Draft verifier for sdd capture (graph-fit guide, light and deep)

Attached to the graph entry that decides the graph-fit and verifier procedure. Companion to the grounding design (its D40: the grounding reading feeds this verifier, which implements the graph-fit guide of 20260810-141555-d-cpt-20r).

## 0. Owner decisions

| # | Question | Decision | Why | Rejected | Accepted cost |
|---|---|---|---|---|---|
| V-D1 | Who builds the proposal | The verifier returns, per finding, search-replace edits over the base draft document (header fields and body); sdd applies them, builds the proposal, the diff, the hunk ids and the finding-to-hunk mapping. Overlapping or non-matching edits are refused after one repair call | A mapping the model writes can be invented or incomplete (reference A needed refusals for that; 220 of its verdicts could not map); per-finding edits cannot widen a draft unnoticed; a light model cannot reliably reproduce a whole file | The verifier copying the draft and mapping its own hunks, checked by sdd (more output tokens, silent rewording of untouched passages, miscounted hunks); edits in light and a free copy in pro (two proposal paths, formats and test sets) | The verifier cannot restructure the draft freely, only replace exact passages |
| V-D2 | Shape of round 1 | One verifier call returns the findings with their per-finding edits; sdd holds the edits back until the drafter has answered every finding (accept, or contest with a reason). With nothing contested, sdd builds the proposal at once, without a second verifier call; contested findings go back to the verifier, which answers only those | Drafters accepted 1,445 of 1,458 answered findings in reference A (0.9% contested), so a second call on every capture buys almost nothing; holding the edits back keeps the drafter judging a finding before it sees the fix | Edits shown with the findings (seeing the fix may tilt the drafter toward accepting); two steps with findings first and edits after the answers (one more verifier call on every capture, minutes in pro mode) | The verifier writes edits for findings that end up contested; one more serve step to show the proposal |
| V-D3 | Config layers for `verifier.mode` | Same rule as grounding enforcement (grounding D2): the user-global config and the committed config set it, the committed value wins over the global one, and the local config may only raise it in the order off, light, pro; a local value lowering it is ignored with a warning naming file and key | A project can make verification binding for every contributor, which the consistency goal needs; one layering rule for grounding and verification | Personal only like `dialogue.mode` (a project cannot require verification); normal layering with local winning (anyone switches it off locally) | A committed `light` needs a configured LLM on every machine, and sdd fails fast where none is set up; a committed `pro` needs subagent-capable harnesses (question 4) |
| V-D4 | When the verifier cannot run | Light failure (provider down, timeout, unreadable answer after the repair call): a step serves the error in full and offers retry. Pro not runnable (no subagent, no delivery): fall back to light where a light model is configured. Otherwise the capture waits; only the user may skip, on a non-delegable choice with the user's words required; the entry then carries `verification: skipped` and the engine forces `confidence: low` (as grounding D4). A setting `verifier.skip: user | never` (default `user`) removes the skip: with `never` the capture blocks until the verifier runs. `verifier.skip` layers like `verifier.mode` (V-D3): global and committed set it, committed wins, local may only tighten it to `never` | A check a project requires cannot vanish without the user's say, and every skip stays visible on the entry; a project that wants no unchecked entry at all can say so | The drafting agent skipping with a reason (in autonomous mode the agent switches off a required check); fail loud only as the sole behaviour (a provider outage stops every capture with no way out) | In autonomous sessions a capture with a dead provider waits for the owner; with `verifier.skip: never` an outage stops every capture until the provider answers |
| V-D5 | When the verifier runs in a dialogue | Guided mode: the draft is shaped freely in playback rounds with no verifier; once the user approves the content, the verifier runs once (full reading) on that version. No change proposed: the approval stands and the entry is written. A proposal: the user sees its diff with each finding and the drafting agent's answer, and confirms or adjusts once more; only that last adjust gets a delta check of the changed lines. Autonomous mode (no dialogue edits): the verifier runs before playback. The drafting agent answers findings and hunks in both modes | Running on every dialogue edit costs model calls (reference A: 716 s median wall time per pro session) and yields findings on text about to change; verifying at the end checks the version actually recorded, the user's own edits included; a delta reading is cheap (reference A: 33 s against 760 s for a full reading) | Verifying once before the first playback with adjusts never re-run (every dialogue edit goes unverified); before the first playback plus a recheck on request (an extra choice, edits unverified whenever nobody asks); the user answering every finding (a dialogue turn per finding, 4.34 findings per reading in reference A) | Findings arrive after the user approved the content, so a substantive finding reopens a draft the user thought finished |
| V-D6 | Writing guide timing | The writing guide moves with the verifier: in guided mode, after the user approves the content, the guide runs once, then the verifier, then the user confirms once more if either changed something; in autonomous mode both run before playback. Refines the timing of 20260810-141555-d-cpt-20r (guides between assemble and playback) for guided mode, and closes gap 20260831-113619-s-tac-fsp there | Both checkers see the final text once, in a fixed order; the guide checks the version the user approved, the user's edits included; the verifier judges text the guide already shaped | Guide before the first playback with the verifier at the end (dialogue edits never get a guide check, fsp stays open); guide in both places (a second guide call per capture, early hints on text that changes anyway) | Guide hints also arrive after the user approved the content |
| V-D7 | Verifier input without a grounding reading | `verifier.mode` other than `off` requires grounding enforcement: with `grounding.enforce` off, sdd refuses the configuration when it resolves it, naming both keys and the layer that set each, before any LLM call. Every verified capture therefore holds a reading, and the verifier never runs retrieval of its own | One input path: the verifier always judges against a reading the drafter was held to; no interim neighbourhood to build and retire | The verifier running `gather` itself when no reading exists (a second way to produce a reading); declared targets, full reads and aspirations as an interim input (weak duplicate detection, two paths) | Two opt-ins are coupled: a project wanting only the verifier must take grounding enforcement too; the verifier ships only after `gather` |
| V-D8 | Serving a large reading to the verifier subagent | The verify procedure serves the reading in pages of at most 50,000 characters each: page 1 holds the draft, the declared targets and every entry the drafter read in full; later pages hold the briefs (to judge and prefilter set-asides) in prefilter score order; every dismissal carries its reason code. The verifier's findings are refused until every page was served, as the served ledger records | Claude Code saves MCP text results over 50,000 characters to a file and caps responses at 25,000 tokens by default (`MAX_MCP_OUTPUT_TOKENS`); paging works the same in every harness, cuts nothing, and the gate shows the verifier received everything | One serve with `anthropic/maxResultSizeChars` raised (Claude Code only; other harnesses unknown); full entries inline with the rest as ids and titles (less than briefs, no proof the list was looked at) | A few extra `next` calls on large readings; a page size fixed for the strictest harness |
| V-D9 | What light and pro are | Light: sdd calls the model `llm_verifier` names (a small, fast model, for example glm-5.3-flash) itself as an op, with no tool calls possible, and serves its findings once as a one-time verdict. No accept or contest, no proposal, no rounds: the drafting agent changes the draft as it sees fit, asks the user in dialogue where needed, and carries on. Pro: a harness subagent on a strong model (Codex, Opus, Sonnet) with tool calls (own graph searches, cited code, files, URLs, web), running the full exchange of V-R3 (findings, accept or contest, proposal, at most 3 rounds, then the user). A draft citing code or the outside world is never refused in either mode; light simply cannot check such claims. The mode is the owner's deliberate choice (an `sdd init` question, owed) | Light trades accuracy for speed and safety: no tool call can run, so nothing outside the served reading is read or touched; `pkg/llm.Request` is single-turn (`pkg/llm/llm.go:41`, SystemPrompt and UserPrompt only), so a paused and resumed exchange would need the whole history resent per round, which light does without. Pro buys accuracy through tool calls and the contest rounds | Light as a tool-less harness subagent (still needs a subagent-capable harness, no faster); one level only (no fast and safe option for self-contained graphs) | Light findings on claims about code or the outside world are missing; light's verdict is advisory, nothing binds the drafter to each finding |
| V-D10 | Scope of V-D1 to V-D8 per mode (follows from V-D9) | Both modes: V-D3 (layering), V-D4 (light failure: retry or user skip; pro not runnable: fall back to light where `llm_verifier` resolves, else retry or user skip; `verifier.skip`), V-D6 (writing guide timing), V-D7 (requires `grounding.enforce`). Pro only: V-D1 (per-finding edits, sdd-built proposal), V-D2 (round 1 shape, contest), V-D8 (paged serve to the subagent). V-D5 per mode: guided pro as recorded; guided light runs once after content approval, serves its verdict, and the drafting agent's changes go to the user as one more playback with no second verdict; autonomous light runs once before playback | Light has no rounds, no proposal and no subagent, so the rows built on those cannot apply to it | none | A guided light capture whose verdict changes the draft costs one more playback |
| V-D11 | Light reading larger than the model's context | One call while the prompt fits `llm_verifier.context_chars` (new key, default 100,000 characters, about 25,000 tokens, leaving room for the answer on a 32k-token model). Above it, sdd slices and merges into one verdict: (1) the reading's entries are grouped by relation first (connected through refs within the reading, else sharing a topic), and whole groups are packed into the fewest slices that fit, so entries likely to clash share a slice; (2) a strain pass: entry pairs from different slices whose stored index vectors (`internal/index/index.go:239`) are near each other (cosine above a code constant) go together into one extra pair slice, so cross-slice clashes still get judged; (3) every slice's prompt starts with the same byte-stable prefix (rules, then the draft), with the slice's entries last, so providers with prompt caching bill the repeated draft as cached input; (4) the slice calls run in parallel; (5) findings merge by `(axis, basis ids)`, duplicates collapse into one finding naming every slice that raised it; (6) the verdict names how many slices ran. Prefilter set-asides go in as briefs in their score order, like every other brief (closes open question 6) | Almost every finding pairs the draft with one entry, so slicing loses little; relation grouping and the vector-based pair slice target the one loss left (clashes between two existing entries in different slices); the cached prefix and fewest-slices packing keep the repeated draft cheap; nothing is dropped from the reading | Cutting briefs to fit (drops exactly the entries nobody read closely, where duplicates hide); refusing light and naming pro (blocks captures where the graph is densest); score-order slicing (clashing entries land in different slices) | A clash between two entries that share no ref, no topic and no near vectors, in different slices, goes unseen; providers without prompt caching bill the draft once per slice; `context_chars` is set by hand per model |
| V-D12 | Sizing a light call (replaces the `context_chars` key and its default in V-D11) | Window: `llm_verifier.context_tokens` when set; else detected where the provider reports it (Anthropic Models API `max_input_tokens`; an OpenAI-compatible models endpoint listing `context_length` (OpenRouter) or `max_model_len` (vLLM); Ollama's effective `num_ctx`), once per process and model; else light is refused at config resolution naming the key. Output reserve: `llm_verifier.max_tokens` (thinking counts toward it), else the model's reported maximum output, capped by a code constant. Input budget: (window − output reserve) × a fill ratio, a code constant starting at 0.5, calibrated per model class by the specimen eval (`Eval_Specimens`). Tokens are estimated at 3 characters each. Ollama calls send `num_ctx` sized to prompt plus output reserve | Input and output share the window and thinking spends the output budget; small models judge worse as the prompt grows, before the window is full; Ollama truncates a prompt above `num_ctx` without an error; the owner picks the model on purpose, so one key where nothing is reported is a fair ask | `context_chars` set by hand on every provider (stale on a model change, ignores the output share); a guessed default window where nothing is reported (too small means needless slices, too large means silent truncation) | Plain OpenAI, Opper and other endpoints reporting nothing need the key; the 3-characters estimate overcounts English prose, so slices run smaller than needed there |
| V-D13 | Light input: metadata, and excerpts when a pair exceeds the budget | Metadata: every light call, sliced or not, carries the draft's header and, for every entry in that call, a fixed header that is never cut: id, type, kind, layer, status (with superseded-by or closed-by), time, topics, refs (kind, target, desc), summary, and its role in the reading (target, full read, brief, set-aside with reason code). Headers are budgeted first. Excerpts: when the rules, the draft and one pair (draft with entry, or entry with entry) exceed the input budget, an oversized body is cut into whole paragraphs (code blocks and list items whole; a paragraph larger than the budget alone is cut at sentence ends, never mid-sentence). Each paragraph is ranked by the stored vector of the index chunk that holds it (the index splitter cuts at blank lines first, `internal/textsplitter/options.go:49`, so a chunk holds whole paragraphs), by closeness to the other side of the pair, and paragraphs are added until the budget is spent. The excerpt keeps document order and the heading of every section it draws from; each gap gets a marker naming what was left out (`[… 3 paragraphs left out …]`), at the start, between kept paragraphs, and at the end. The verdict lists every entry judged on excerpts. Floor: rules, draft and one paragraph per side; a draft that alone exceeds the budget is refused for light, naming pro or a larger window | Index chunks are not usable as text: they cut at a length limit (separators blank line, line, space, then character), overlap by 320 runes (`DefaultChunkOverlap`), carry an `Entry:`/`Breadcrumb:` preamble for embedding, and hold no offsets into the body; whole paragraphs in document order with explicit gap markers keep a small model from reading a cut as the text's own end or a jump as continuity; status and refs are what outdated and inconsistent findings rest on, so no call may lack them | Index chunk text as excerpt (mid-sentence cuts, duplicated overlap, preamble noise); judging by summary only (detail lost); skipping the pair (never checked); chunk offsets added to the vendored splitter (a change to the index derivation for a verifier need) | A clash inside a paragraph not picked goes unseen; headers repeat in every slice |
| V-D14 | Build order (closes open question 7) | Slice 1: light (sdd op over `llm_verifier`). Slice 2: child sessions in full, the engine side of 20260827-232430-d-cpt-voa and the harness side of 20260829-122157-d-cpt-zpe, with pro as their first consumer beside graph research. Slice 3: pro, a `verify` task procedure in a child session | Light needs no child session; voa and zpe built whole keep one delegation path, matched to their contracts (non-interactive property, read sets, lineage, dispatch instructions, subagent definitions) | Pro building only the voa and zpe parts it needs (half-built engine machinery that must later match the full contracts, a second path if it drifts); waiting with light until voa and zpe ship (no verification for longer, though light depends on neither) | Until slice 3, graphs citing code get only light |
| V-D15 | Name of the extensive mode | `verifier.mode: off | light | deep`; "pro" in this document and in V-D1 to V-D14 means `deep` | Plain English naming the extent difference, one scale with `light` | `debate` (suggests arguing for its own sake), `review` (generic, collides with code review and the evaluate procedure), `audit` (taken by grounding D36 for the 3% re-score sample) | The name says nothing about the rounds |
| V-D16 | How a deep verifier session is launched | `verifier.launch: subagent | claude | codex`; the procedures are the same for every value. `subagent`: the drafting agent's harness runs a fresh-context subagent (visible to the user). `claude`: sdd runs `claude -p --output-format json`, keeps the returned `session_id` per verifier child session and continues each round with `claude -p --resume <session-id>`. `codex`: sdd runs `codex exec --json` and continues with `codex exec resume <SESSION_ID> <prompt>`. The headless values are invisible to the user and also serve harnesses without a subagent tool. This reopens the section 9 rejection of sdd spawning a tool-enabled agent CLI: the headless launch needs its own design for tool allowlists, sandbox and permission flags, kept apart from the isolated claude runner (`internal/llm/claude/claude.go:53-60`) | One key whose value says exactly what runs; zpe already names this side "launch configuration"; both headless CLIs continue a session across rounds natively, so the multi-round exchange needs no subagent continue mechanism there | `launch: subagent | headless` plus a `headless` CLI key (a second key dead under `subagent`); `runs: visible | hidden` (names what the user sees, not what runs, and still needs a CLI key) | sdd becomes an agent host for the headless values, with a sandbox and permission surface to own; a further headless CLI means a new value |
| V-D17 | Multi-round child sessions (closes open question 8) | Two new graph directives, each refining one existing entry. (1) Refining 20260827-232430-d-cpt-voa: a task procedure may declare interim deliveries; a step delivers values into the dispatching instance's declared field without completing the child, then waits at a gate on a field the parent fills (the drafter's answers), which the engine delivers into the child; only the final delivery completes the child; the child concludes when the dispatching instance ends (entry written or abandoned) or at its round cap, and an abandoned parent abandons it; this closes voa's two open points for task children. (2) Refining 20260829-122157-d-cpt-zpe: the first launch stays fresh-context; later rounds continue the same delegate (the harness's continue mechanism, listed in the per-harness launch configuration) or relaunch a fresh one with the same child handles, and the child's serve always carries the full exchange; sdd itself may be the launcher (`verifier.launch: claude | codex`, V-D16), keeping the CLI session id per child session and resuming it each round; the parent handle never enters a delegate prompt, whoever launches. The procedure-spec reference (20260814-100000-s-prc-psr) describes interim deliveries and parent-filled gates when they ship | A verifier exchange needs findings out, answers in and responses out; voa's single delivery ends the child after round 1, and zpe allows only one fresh launch by the harness; every other commitment of voa (fork, inheritance, non-interactive, handle-only drive, no blocking) and zpe (dispatch instructions, MCP-only path, parent handle kept out) still holds | Superseding voa and zpe (restates every unchanged commitment, and the entries building on them would need re-checking); one directive refining both (mixes the engine contract and the launch artifacts, against the split voa and zpe drew); one child per round (a fresh judge each round loses the exchange's context) | Three entries make up the delegation contract |
| V-D18 | What a headless deep verifier may touch (`launch: claude | codex`) | Working folder: the project checkout. Allowed: read, search and glob over it; web fetch and web search; the sdd MCP tools; shell only for read-only git (`git log`, `git show`, `git diff`, `git blame`). Not loaded: project instructions, skills, plugins, hooks, other MCP servers. No file writes; results reach the capture only through the child session's `next` calls. Auth: the user's own CLI login. claude: `-p --safe-mode --tools "Read,Grep,Glob,WebFetch,WebSearch,Bash" --allowedTools` with the read tools, `mcp__sdd__*` and the four git patterns, `--strict-mcp-config --mcp-config <sdd only>`; whether `--safe-mode` drops a server passed by `--mcp-config` is probed at build. codex: `exec --sandbox read-only` with `-c mcp_servers.sdd=...`; `exec` refuses every approval request. Two `sdd serve` processes share the session store safely (per-session file lock, `pkg/local/session_store.go:487`) | Deep checks claims about code and its history; an agent nobody watches gets no write or general shell power; project hooks and instructions would bend the judge's view; the same reach the subagent reference verifier had | Read-only without shell (claims about history go unchecked); the user's normal CLI setup (full write and shell power unwatched, project hooks and instructions in the judge's context) | No linters or test runs by the verifier |
| V-D19 | Custom launcher (`verifier.launch: custom_driver`) | A fourth launch value for an external orchestrator that runs the verifier its own way, for example as a long-lived interactive agent process rather than a one-shot CLI. Key `verifier.driver: {command: <path>, args: [...]}`. Per round sdd runs the command and writes one JSON request to its stdin: `{"round": n, "session": "<child session handle>", "instance": "<child instance>", "prompt": "<round prompt>", "driver_state": "<value the driver returned last round, or empty>"}`; the driver starts or continues its agent with that prompt and exits once the agent's round ended, printing `{"driver_state": "<opaque value for the next round>"}`. Findings and answers never travel through the driver: the agent delivers them through the child session's `next` calls, so they keep engine provenance. A nonzero exit or a timeout is a launch failure under V-D4. The driver command is read only from the user-global and local config layers; a committed `launch: custom_driver` is refused on a machine whose layers name no driver, naming the key | Lets an orchestrator keep its own agent roles and process model while the procedures stay unchanged; an opaque `driver_state` lets it resume whatever it runs (process, pane, session) without sdd knowing how; a command in committed config would run on every contributor's machine, so it stays machine-local | The orchestrator relaying findings on stdout (the launcher would carry the judge's words); a committed driver command (runs code the contributor never chose) | Each orchestrator writes and maintains its driver; a project committing `custom_driver` needs every contributor to configure one |
| V-D20 | Layering of `verifier.launch`, and no subagent tool | `verifier.launch` layers like a personal setting: user-global, then the committed config as project default, then the local config wins; it describes the machine (its harness, its installed CLIs), not a project requirement. With `launch: subagent` and an agent that reports no subagent tool at the dispatch step, sdd falls back to the headless launches of the agents `supported_agents` names (`internal/model/config.go:156`, for example `[claude, codex]`), in that order, trying only those whose CLI is found; the serve names the launch it fell back to, and the verdict records it. None found: refused, naming `verifier.launch` | A contributor without subagents still gets the deep verifier without a setting; `supported_agents` is the project's own statement of which agents it runs, so the fallback never starts an agent the project did not name, in the order it chose | Refusing without fallback (every such contributor sets a local key first); trying every known CLI (starts agents the project never named); `launch` layered like `mode` with committed winning (forces a launch on machines where it cannot run) | The fallback runs headless under V-D18 without a per-run confirmation; the agent's own report of "no subagent tool" is taken as given |
| V-D21 | Light folds into pre-flight (closes open question 9; replaces the separate light op of V-D9 and V-D10) | The capture's pre-flight step (`internal/baseprocedures/entries/20260703-094500-d-prc-cap.md:102-108`, after playback confirm, before write) is the light verifier. `verifier.mode` builds up: `off` runs pre-flight as today (the draft's refs); `light` runs pre-flight over the grounding reading, adding the axes duplicates, outdated, strain and dismissal, with the V-D13 headers and the V-D11 slicing; `deep` runs the light pre-flight, then the deep exchange, which receives the pre-flight findings as already raised. Pre-flight's own checks (language, durability, kind rubrics, plan coverage, mechanical) stay in every mode. `high` findings block the write with the user-only override, as today. The deep exchange starts only once pre-flight leaves no `high` finding open (fixed by the drafter, or overridden by the user, which the deep verifier then treats as settled). `llm_verifier` is removed: pre-flight runs on the `llm` model, and V-D12's `context_tokens` key moves to `llm` | Pre-flight already sits where light belongs, has light's shape, and binds; one checker per question instead of two; the deep exchange is the most expensive step, so it never spends rounds on a draft pre-flight already blocks | Light as its own step beside pre-flight (overlapping axes judged twice, an advisory verdict beside a binding one); deep with a refs-only pre-flight (no cheap first pass); keeping `llm_verifier` (a second model key for one step) | Pre-flight templates grow by the reading and four axes; deep always pays the light call; light and the other LLM ops share one model |
| V-D22 | Pre-flight after a fix, before deep | A draft revised for `high` pre-flight findings goes through playback and pre-flight again, as capture does today (`capturePreflightCurrent`); the deep exchange starts once no `high` finding is open | One small call catches regressions in what only pre-flight checks (language, durability, kind rubrics, plan coverage); deep rounds stay on what only tools can check; capture keeps one path for every mode | Skipping the re-run and letting deep confirm the fixes (unchecked regressions, deep spends rounds on the cheap pass's job, a special path for deep mode) | One more small LLM call per fix |
| V-D23 | A deliberate skip of the deep exchange (decided in part; key name and weighing rule open) | A setting lets owners allow skipping the deep exchange when it is not worth it. Unset, or set to never skip: the deep exchange always runs. Allowed: before the deep exchange, the capture weighs whether it is worth running and puts the choice to the user (user chooser, not delegable), "run" and "skip" as options, one of them marked recommended in the dialogue's language. A skip caps the entry's confidence at `medium`. Separate from V-D4's skip on a verifier failure, which sets `confidence: low` | The deep exchange is the most expensive step; small or self-contained drafts gain little from it; the user decides, and the entry records the lower assurance | none | (closed by V-D24) |
| V-D24 | Weighing the deep skip, and its key (closes V-D23's open points; amends V-D4's key) | sdd weighs by a fixed rule, not the drafting agent: it recommends **run** when the draft makes claims a tool can check (file paths or code names found in the checkout, URLs, commit hashes), is a directive, plan or contract on the strategic, conceptual or tactical layer, supersedes or closes an entry, or pre-flight left `medium` findings; otherwise **skip**. The agent may add a one-line reason, shown beside the recommendation. One key, an ordered scale: `verifier.skip: never | failure | weighed`. `never`: no skip, a verifier failure blocks until it runs. `failure` (default, V-D4's former `user`): the user may skip only when the verifier cannot run, `confidence: low`. `weighed`: additionally the weighed run-or-skip question before the deep exchange, a skip capping confidence at `medium`. Layering as V-D4: global and committed set it, committed wins, local may only tighten toward `never` | The drafter is the judged party and must not argue its way out of the check; deep's tools add value only where a claim can be checked, and the rule names those cases; one scale keeps every skip case in one place | The drafting agent weighing (the judged party decides whether it is judged); a separate `offer_skip` key beside `skip` (two skip keys, four combinations) | A fixed rule misjudges some drafts; the user still decides |
| V-D25 | Severity of deep findings (closes open question 10) | Deep findings use pre-flight's scale `high | medium | low` (`internal/llmops/preflight_templates/verdict.tmpl:8`) with its calibration, plus a boolean `owner`. A deep `high` is resolved in the rounds or goes to the user; `medium` and `low` are answered (accept or contest); an `owner: true` finding carries no edits and goes straight to the user | One scale across both checkers, so drafter and user read every finding the same way and the blocking rule maps directly; the owner flag (from the subagent reference) keeps the verifier from proposing fixes for what only the user can settle | `substantive | minor` for deep (two scales in one capture, no mapping onto blocking); the reference's five check categories without severity (nothing says what blocks) | Deep applies calibration text written for pre-flight's checks |
| V-D26 | Binding the write to the verified text (closes open question 11) | The deep verdict, or a recorded skip, is a session event over the same input fingerprint pre-flight uses (draft, staged attachments, target; `pkg/application/workflow_capture.go:144-160`). The `write` step requires a current pre-flight, and with `mode: deep` also a current deep verdict or skip (`captureDeepVerdictCurrent`); an edit after the verdict sends the capture back, V-D5's delta check covering a last adjust | The engine already binds the write to pre-flight this way; one mechanism, held in engine state, no file or hook to keep in step | A `.verified` hash file per draft as in both references (a second binding beside the engine's, on disk, enforced by a hook); no binding (an edit after the verdict is written unverified, unreported) | One more state check in capture |
| V-D27 | Verification from the CLI (closes open question 12) | Follows grounding D3. First slice: no CLI path; `sdd new` already refuses under grounding enforcement, which every verifier mode requires (V-D7). With the session-aware CLI (D3 follow-up), `sdd new --session <handle>` runs the same pre-flight and deep exchange inside that session; deep uses only sdd-started launches (`claude`, `codex`, `custom_driver`), and a configured `subagent` falls back through `supported_agents` (V-D20) | One verification path, bound to the write (V-D26), for MCP and CLI alike | A stateless `sdd verify <draft>` (verification outside capture, no binding to the written entry, a second path) | CLI-only users get verification only with the session-aware CLI |
| V-D28 | `sdd init` questions | Two questions beside init's existing four (`internal/cliapp/app.go:1666-1757`). (1) Project, written to the committed config, asked on a fresh init and whenever unset: "How strictly should drafts be checked against the graph?" with `off`, `grounding` (enforcement only), `light`, `deep`; one answer sets `grounding.enforce` and `verifier.mode` together, so no invalid pair (V-D7) can be chosen. (2) Personal, written to the local config, asked only when the resolved mode is `deep` and no layer sets `verifier.launch`: "How should the deep verifier run?" with `subagent` as the default; `claude` and `codex` offered only where `supported_agents` names them, in its order; `custom_driver` offered only where a config layer already sets `launch: custom_driver`. `verifier.skip` and `llm.context_tokens` are not asked: their defaults hold, and a refusal names the key when one is needed | The verifier mode is a deliberate owner choice, so init asks for it; one combined answer keeps grounding and verifier consistent; the launch question offers only what the project runs and what is already set up | One question per key (four questions, an invalid pair refused after the fact); no questions (an unasked default is no deliberate choice) | Changing `skip` means editing the config |

## 1. Requirements

| # | Requirement | Source |
|---|---|---|
| V-R1 | A judge outside the drafting agent's context checks each draft before the user confirms it, against the grounding reading (D40). | owner |
| V-R2 | Two variants by configuration (V-D9, V-D15, V-D21): light is pre-flight over the grounding reading on the `llm` model, no tool calls, one verdict per draft version; deep is a strong-model agent (Codex, Opus, Sonnet) with tool calls (own graph searches, cited code, files, URLs, web), running a multi-round exchange with the drafter. | owner |
| V-R3 | Procedure: (1) the verifier returns structured findings: severity, a pointer to the draft piece(s), a verdict in words; (2) the verifier pauses, the drafter accepts or contests each finding with a reason in words; (3) the verifier receives the answers, builds a proposal (the draft with changes applied), maps each finding to the diff hunks carrying its change, explains or drops contested findings, applies changes for contests it accepts and says so; (4) the drafter accepts the proposal in full (entry ready to capture) or disagrees with hunks; disagreement starts a drafter-verifier exchange of up to 3 rounds; without agreement the procedure stops and the open questions go to the owner. | owner |
| V-R4 | Harness-agnostic: works over MCP and CLI, for Claude Code, Codex and other agents; no hooks or plugins. | grounding R13 |
| V-R5 | Off by default; minimum configuration surface; the `llm` / `llm_prefilter` block shape is precedent (D8). | grounding D1, D8 |
| V-R6 | Findings are drafting input, not a hard gate; after the user's confirmation only deterministic checks run. Narrowed by V-D5 and V-D21: pre-flight (light) and the deep exchange run after content approval, and a `high` pre-flight finding blocks the write as pre-flight does today. | 20r, D13 |
| V-R7 | No truncation of agent-facing output (D29); no repeat of already-served content in MCP mode, the CLI never cuts (R14). | grounding |

## 2. Reference implementations (findings)

Full reports attached: `reference-verifier-a.md` (a verifier run by an orchestrator as a separate Claude Code process, Opus, with measurements over about 2,000 drafts) and `reference-verifier-b.md` (a verifier run as a Claude Code subagent, Sonnet, about nine drafts). Project names and project descriptions are removed; only what bears on sdd remains.

| Finding | A (orchestrator, Opus) | B (subagent, Sonnet) |
|---|---|---|
| Executor | separate `claude` process in tmux, kept alive, woken by a `Stop` hook | Claude Code subagent, continued with `SendMessage` |
| Model and tools | `opus`, high effort (medium for delta readings); Bash, Read, Edit, Write, Grep, Glob, WebSearch, WebFetch | `sonnet`, high effort; Read, Grep, Glob, limited Bash, Write to proposal and verdict only; no web |
| Findings format | free-text report lines opening `high|medium|low`; pointer free text | `check=1-5`, `owner=yes|no`, `quote`, `problem`, `evidence`, `lines` |
| Answers | `<n>: accepted` / `<n>: contested, <evidence>`; nothing parses them | same; evidence must be file:line, entry ID or quoted output |
| Proposal | verifier edits a copy; `diff -U0` hunks named by header, mapped to findings by the verifier; sdd-side refusals for unmapped or invented hunks and for widening | verifier edits a copy; pointer = proposal line numbers plus quote; reviewed as word diff |
| Adoption | whole proposal or nothing; refused while a finding stays open | whole proposal moved over the draft |
| Round bound | 3 non-clean verdicts, then refusal unless `--past-cap '<why>'` (reason not stored) | 3 rounds, then open findings with evidence go to the owner |
| Verdict binding | hash of the read bytes (`--read-hash`) after a race where the drafter edited during reading | SHA-256 of the body; capture refuses confirm without a matching verdict |
| Light variant | not built; small models only for pre-flight and the candidate filter; the verifier searches unfiltered | not built |
| Harness | tied to Claude Code, tmux, macOS `sandbox-exec` | tied to Claude Code `SendMessage` |

Measurements (A):

1. Only 93 of 1,971 first readings were clean; mean 4.34 finding lines per findings reading.
2. Contest rate 0.9% (13 of 1,458 answers).
3. 783 of 1,718 landed drafts landed as an adopted proposal; mean 5.26 hunks per proposal; 1,597 of 2,126 proposals left no finding open.
4. Non-clean readings per draft: 0: 261, 1: 1,302, 2: 374, 3: 114, 4 or more: 88.
5. Median session 1.08 USD (Claude Code estimate), 161 s API time, 716 s wall time (including waits on the drafter); full readings median 760 s, delta readings 33 s.
6. Review replays: `glm-5.3` recall 28% of 25 confirmed findings, `mistral-large-4` 4%; `glm-5.3-flash` at max effort matched `opus` on 6 of 10 validation decisions.
7. Delta readings at medium effort raised findings in 37% of later readings, full readings at high in 72%.

Failure modes and lessons:

1. Proposals widened drafts round after round (A); fixed by refusing proposal content no finding names.
2. Reading raced with drafter edits (A); fixed by binding the verdict to the read bytes.
3. 220 findings verdicts without a severity-led line could not map to hunks (A): free-text formats break mapping.
4. Drafters gone before answering: 56 readings lost (A).
5. Reusing one verifier across versions was first rejected as "not independent", then adopted (A): context grows, cost falls.
6. B's maintainer would add stable finding IDs across rounds, hunk labels tied to findings, a harness-neutral continue mechanism, and recorded counts.
7. B's `owner=yes` flag marks findings only the owner can settle; they get no fix.

## 3. Plans weighed

Two planners worked from one brief without knowing each other: plan A (brief-faithful) and plan B (first principles). Full plans: `plan-a.md`, `plan-b.md` (scratch, not attached). The plans use "pro" for what V-D15 names `deep`, and both plan light as its own op, which V-D21 folds into pre-flight. This section records the plans as written; section 4 holds the design in force.

Both depart from the brief in the same way: the verifier returns per-finding edits, and sdd builds the proposal, the diff, the hunk ids and the finding-to-hunk mapping deterministically. Reason: a mapping the model writes can be invented or incomplete (reference A's refusals exist for that), a light model cannot reliably reproduce a whole file, and an edit tied to a finding cannot widen the draft unnoticed.

| Topic | Plan A | Plan B |
|---|---|---|
| Light executor | sdd op `runVerifier`, one stateless call per round with the whole exchange | sdd op `verifyRound`, same |
| Pro executor | harness subagent drives a `verify` task procedure in a child session (voa, zpe); one child per phase, at most 5 | same, but one child session per capture across rounds |
| Round 1 | two steps: findings, then proposal after answers | findings carry their changes; the proposal call is skipped when nothing is contested |
| Store | engine-written `verifierExchange`; collected `verifierAnswers` | engine-written `verification`; collected `findingAnswers`, `hunkAnswers`, `ownerRulings` |
| Bound | constant 3 discuss rounds, then non-delegable user chooser | same |
| Escalation in autonomous mode | park the capture with the open points as the note | non-delegable chooser; capture parks until the owner answers |
| Provider failure | `verifierFailed` step: retry or skip, skip named at playback | fail loud like the writing guide |
| Pro not runnable | drafter may skip with a reason named at playback | fall back to light, or skip with a note |
| Input without a reading | declared targets, entries read in full in the capture, aspirations; gather once it ships | `verifyRound` runs gather itself once |
| Set-asides in light input | drafter verdicts plus prefilter set-asides as briefs | not addressed |
| `verifier.mode` layer | normal overlay at every layer | personal only, like `dialogue.mode` |
| Pro before voa | build the voa subset after the light slice | wait for voa and zpe |
| Pre-flight after confirm | drop its graph-facing LLM checks for verified captures (follow-up) | make it mechanical only |
| Severity | `substantive | minor` | `substantive | minor` |
| CLI | optional stateless `sdd verify` later | none |

## 4. Design

### 4.1 Modes build up (V-D21)

1. One pre-flight step in capture for every mode: after playback confirm, before write (`internal/baseprocedures/entries/20260703-094500-d-prc-cap.md:102-108`). Pre-flight's own checks (language, durability, kind rubrics, plan coverage, mechanical) run in every mode.
2. `off`: pre-flight as today, over the draft's refs, on the `llm` model.
3. `light`: pre-flight over the grounding reading, on the `llm` model, no tool calls. It adds the axes `duplicates`, `outdated`, `strain` and `dismissal`. Input: the draft's header, then per entry a fixed header that is never cut (V-D13) and its body or brief. A reading larger than the input budget is sliced and merged (V-D11, V-D12); an oversized pair is cut into whole paragraphs in drafted order with gap markers (V-D13). Requires `grounding.enforce` (V-D7).
4. `deep`: the light pre-flight, then the deep exchange. The deep verifier receives the pre-flight findings as already raised.
5. A `high` pre-flight finding blocks the write, with the user-only override (`reviseOrOverride`). A revised draft runs playback and pre-flight again (V-D22). The deep exchange starts only when no `high` finding is open; a user override is settled for the deep verifier and never raised again.

### 4.2 Timing in a dialogue (V-D5, V-D6, V-D10)

1. Guided: the draft is shaped in playback rounds with no checker. After the user approves the content, the writing guide runs once, then pre-flight, then the deep exchange in `deep` mode. A changed draft goes to the user once more; only the last adjust gets a delta check.
2. Autonomous: the same order before playback.
3. The drafting agent answers findings in both modes.

### 4.3 The deep exchange

1. Findings carry pointers, evidence, severity `high | medium | low` and `owner` (V-D25), plus per-finding search-replace edits over the base draft document (V-D1). sdd applies the edits (`internal/textpatch/textpatch.go`), refuses overlapping or non-matching edits after one repair call, and builds the proposal, the diff (`internal/textdiff/textdiff.go`, extended to structured hunks) and the finding-to-hunk mapping.
2. Round 1 returns findings with their edits; sdd holds the edits back until the drafter answered every finding. Nothing contested: the proposal is built at once. Contested findings go back to the verifier, which answers only those (V-D2).
3. An `owner: true` finding carries no edits and goes straight to the user. A deep `high` is resolved in the rounds or goes to the user.
4. At most 3 rounds, then a non-delegable user chooser serves each open point with both positions.
5. Accepting the proposal returns to `assemble`; the full gate runs again. A ref the verifier added that the drafter has not read trips `refsInspected`: the verifier's reads never credit the drafter.

### 4.4 Child session and launch (V-D16 to V-D20)

1. The deep verifier runs a `verify` task procedure in a child session. The child persists across rounds: interim deliveries into the dispatching instance, a gate on the field the parent fills (the drafter's answers), the final delivery completing it; the child ends with the dispatching instance or at its round cap (V-D17, refining 20260827-232430-d-cpt-voa).
2. Launch, `verifier.launch`, the same procedures for every value:
   1. `subagent`: the drafting agent's harness runs a fresh-context subagent; later rounds continue it, or relaunch a fresh one with the same child handles (refining 20260829-122157-d-cpt-zpe). Visible to the user.
   2. `claude`: sdd runs `claude -p --output-format json` and continues each round with `claude -p --resume <session-id>`.
   3. `codex`: sdd runs `codex exec --json` and continues with `codex exec resume <SESSION_ID> <prompt>`.
   4. `custom_driver`: sdd runs the command in `verifier.driver` per round with a JSON request on stdin (V-D19, section 5).
3. Headless reach (V-D18): working folder the project checkout; read, search and glob; web fetch and web search; the sdd MCP tools; shell only for `git log`, `git show`, `git diff`, `git blame`. No project instructions, skills, plugins, hooks or other MCP servers; no file writes; the user's own CLI login. Results reach the capture only through the child session's `next` calls.
4. No subagent tool (V-D20): with `launch: subagent`, sdd falls back to the headless launches of the agents `supported_agents` names, in its order, trying only those whose CLI is found; the serve and the verdict name the fallback. None found: refused, naming `verifier.launch`.
5. The parent handle never enters a delegate prompt, whoever launches.

### 4.5 Skip and failure (V-D4, V-D23, V-D24)

1. Verifier failure (provider down, timeout, unreadable answer after the repair call, no launch possible): a step serves the error and offers retry. The light pre-flight has already run before any deep exchange, so V-D4's fall back from deep to light is the light verdict standing alone, which counts as a skip. A skip is the user's alone, non-delegable; the entry carries `verification: skipped` and `confidence: low`.
2. Weighed skip, only with `verifier.skip: weighed`: before the deep exchange, sdd weighs by a fixed rule and puts run or skip to the user, one option marked recommended in the dialogue's language. Recommend run when the draft makes claims a tool can check (file paths or code names found in the checkout, URLs, commit hashes), is a directive, plan or contract on the strategic, conceptual or tactical layer, supersedes or closes an entry, or pre-flight left `medium` findings; else recommend skip. The agent may add a one-line reason. A skip caps confidence at `medium`.

### 4.6 Binding, store, CLI, init

1. The deep verdict, or a recorded skip, is a session event over the input fingerprint pre-flight uses (draft, staged attachments, target; `pkg/application/workflow_capture.go:144-160`). `write` requires a current pre-flight, and with `mode: deep` a current deep verdict or skip (V-D26).
2. The verification record (mode, launch, identity, base hash, findings, answers, responses, rounds, status) lives in the capture instance's engine store and the session event log. Park and resume survive mid-exchange.
3. Playback shows an engine-rendered summary (mode, launch, counts, outcome); `high` findings are named to the user.
4. CLI (V-D27): no CLI path in slice 1, since `sdd new` refuses under grounding enforcement (grounding D3). With the session-aware CLI, `sdd new --session <handle>` runs the same pre-flight and deep exchange, with sdd-started launches only.
5. `sdd init` (V-D28) asks how strictly drafts are checked (`off | grounding | light | deep`, committed, setting `grounding.enforce` and `verifier.mode` together), and, for `deep` without a launch set, how the deep verifier runs (local; `subagent` default, `claude` and `codex` per `supported_agents`, `custom_driver` only where a layer already sets it).
6. `off`: capture behaves as today.

## 5. Formats

Findings (verifier to sdd; reasoning first):

Light findings use pre-flight's finding format (`internal/llmops/preflight_templates/verdict.tmpl`, `internal/llmops/response_schemas/`), with `severity: high | medium | low` and the added axes. The verdict also lists the entries judged on excerpts and the number of slices run (V-D11, V-D13).

Deep findings (verifier to sdd; reasoning first):

```json
{"findings": [{"verdict": "words", "axis": "contradicts", "severity": "high",
  "owner": false,
  "pointers": [{"field": "body", "quote": "exact draft words"}, {"field": "refs", "id": "<entry id>"}],
  "basis": ["<entry id>"], "sources": [],
  "edits": [{"old": "...", "new": "..."}]}],
 "notes": "verifier-only working notes"}
```

1. sdd numbers findings `f1..fn`; an empty list is valid and the prompt says so.
2. `severity`: `high | medium | low` with pre-flight's calibration (V-D25). `owner: true`: only the user can settle it; `edits` stays empty.
3. `axis` (closed set): `contradicts`, `duplicates`, `outdated`, `inconsistent` (for example a missing supersede or close), `ref-fit`, `missing-ref`, `dismissal` (challenges a drafter or prefilter set-aside), `aspiration` (contradiction only, per 20r), `strain` (two existing entries clash; feeds the recheck list, grounding D5), `evidence` (deep only: a cited file or URL contradicts the draft).
4. Light: every `basis` id must be in the served reading. Deep: ids must resolve; `evidence` needs `sources`.

Drafter answers:

```yaml
answers:
  - {target: f1, verdict: accept}
  - {target: f2, verdict: contest, reason: "words"}
  - {target: h2, verdict: contest, reason: "words"}   # hunk objections; unnamed hunks accepted
```

Verifier responses (proposal round and discuss rounds): per finding `applied`, `held` (with edits and an explanation), `conceded` (the drafter's reason holds; alternative edits applied), `revised`, `dropped` (with a note).

Proposal serve (sdd to drafter): a summary line, the diff with hunks labelled by finding (`@@ h1 [f1 applied]`), notes per finding, the answer instruction, rounds left.

Custom driver (V-D19), one call per round. Request on stdin:

```json
{"round": 2, "session": "<child session handle>", "instance": "<child instance>",
 "prompt": "<round prompt>", "driver_state": "<value from the previous round, or empty>"}
```

Response on stdout once the agent's round ended:

```json
{"driver_state": "<opaque value for the next round>"}
```

Findings and answers never travel through the driver; the agent delivers them through the child session's `next` calls. A nonzero exit or a timeout is a launch failure (V-D4).

## 6. Configuration

```yaml
grounding:
  enforce: false        # required (true) for any verifier mode other than off (V-D7)
verifier:
  mode: off             # off | light | deep
  launch: subagent      # subagent | claude | codex | custom_driver (deep only)
  driver:               # custom_driver only; global or local config only
    command: /path/to/driver
    args: []
  skip: failure         # never | failure | weighed
llm:
  context_tokens: 0     # light window where the provider reports none (V-D12)
```

| Key | Layering |
|---|---|
| `grounding.enforce` | as grounding D2: global and committed set it, committed wins, local may only raise it |
| `verifier.mode` | global and committed set it, committed wins, local may only raise it in the order `off < light < deep` (V-D3); a lowering local value is ignored with a warning naming file and key |
| `verifier.launch` | personal: global, then committed as project default, then local wins (V-D20) |
| `verifier.driver` | global and local only; in the committed config it is refused, naming the key (V-D19) |
| `verifier.skip` | global and committed set it, committed wins, local may only tighten toward `never` (V-D24) |
| `llm.context_tokens` | as the `llm` block; explicit value wins over detection (V-D12) |

1. `verifier.mode` other than `off` with `grounding.enforce` off is refused when the config resolves, naming both keys and the layer that set each (V-D7).
2. Code constants: 3 rounds, one repair call per verifier call, the pre-flight fill ratio (start 0.5, calibrated by `Eval_Specimens`), 3 characters per token.
3. Deep has no model key: the harness or the headless CLI picks the model.

## 7. Changes per package and tests

Build order V-D14. Every slice: tests at the layer that owns what they assert (engine semantics in `internal/engine`, shipped-procedure behavior in `internal/proctest`), each new guard broken once and seen red.

### Slice 1: light pre-flight

| Package or file | Change | Tests |
|---|---|---|
| `internal/model/config.go` | keys `verifier.mode`, `verifier.skip`, `llm.context_tokens`; layering rules; V-D7 refusal | layering per key; mode with enforcement off refused, naming both keys and layers |
| `internal/llmops/preflight.go`, `internal/llmops/preflight_templates/`, `internal/llmops/response_schemas/` | light input (reading, headers, briefs, set-asides in score order), axes `duplicates`, `outdated`, `strain`, `dismissal`; slicing, pair slice, merge; excerpts with gap markers | prompt contract tests; slicing and merge on synthetic readings; excerpt order and markers; `Eval_Specimens` specimens per new axis (`internal/llmops/specimen_eval_test.go`) |
| `internal/index/index.go` | read stored vectors for the pair slice and paragraph ranking | vectors read by entry; nearest-chunk ranking |
| `internal/llm/factory/factory.go` | context window: key, else provider detection (Anthropic Models API, models endpoint `context_length` or `max_model_len`, Ollama `num_ctx`), else refusal; Ollama `num_ctx` sent per call | detection per provider against recorded responses; refusal names the key |
| `internal/baseprocedures/entries/20260703-094500-d-prc-cap.md`, `pkg/application/workflow_capture.go`, `pkg/application/workflow_registry.go` | pre-flight step reads the grounding reading in `light`; verdict lists excerpts and slices; weighed-skip rule data | `internal/proctest`: off unchanged, light blocks on `high`, revise re-runs pre-flight |
| `internal/cliapp/app.go` | init question on strictness (V-D28) | init writes both keys; no invalid pair offered |

### Slice 2: child sessions (voa and zpe refinements)

| Package or file | Change | Tests |
|---|---|---|
| `internal/engine/spec.go`, `internal/engine/instance.go`, `internal/engine/session.go` | task procedures with interim deliveries, gates on parent-filled fields, final delivery completing the child, child lifetime bound to the dispatching instance and a round cap | engine tests over synthetic specs: interim delivery lands without completing; gate holds until the parent fills; abandon cascades |
| `pkg/application/workflow.go` | child dispatch and delivery both ways | application unit tests |
| `pkg/local/session_store.go` | none expected (per-session file lock already serves two `sdd serve` processes) | two-process delivery test |
| `internal/bundledskills/templates/` | dispatch instructions: continue or relaunch per round; per-harness continue mechanism in the launch configuration | rendered-skill tests; `devbox run validate-skills` |
| `internal/baseprocedures/entries/20260703-190100-d-prc-exp.md` | unchanged single-delivery task procedure stays valid | `internal/proctest` explore suite still green |

### Slice 3: deep

| Package or file | Change | Tests |
|---|---|---|
| `internal/baseprocedures/entries/` | new `verify` task procedure; capture step `verify` after pre-flight, before write | `internal/proctest`: rounds, contest, owner findings, 3-round cap to user, override settled |
| `pkg/application/workflow_capture.go` | deep verdict event over the pre-flight fingerprint; `write` guard `captureDeepVerdictCurrent`; weighed skip; confidence caps | edit after verdict sends back; skip caps `medium`; failure skip sets `low` |
| `internal/textpatch/textpatch.go`, `internal/textdiff/textdiff.go` | per-finding edits, structured hunks, finding-to-hunk map | overlapping and non-matching edits refused after one repair |
| new launcher package under `internal/` | `claude`, `codex`, `custom_driver` launches with V-D18 flags, session id per child, resume per round, driver JSON contract | fake CLIs on PATH; driver contract; parent handle absent from every prompt |
| `internal/model/config.go` | keys `verifier.launch`, `verifier.driver`; driver refused in committed config | layering; refusal names the key |
| `internal/cliapp/app.go` | init launch question (V-D28) | offers only `supported_agents` CLIs; `custom_driver` only where set |
| `pkg/mcpapp` | serve texts for dispatch, rounds, fallback notice | transport tests |

## 8. Open questions

Closed:

1. Round 1 shape: V-D2.
2. `verifier.mode` layer: V-D3.
3. Escalation in autonomous sessions: V-D4 (the user skips, non-delegable) and section 4.3 item 4.
4. Provider failure and deep not runnable: V-D4, V-D20, V-D21.
5. Input when no grounding reading exists: V-D7.
6. Prefilter set-asides in the light input: V-D11.
7. Deep before voa: V-D14.
8. Child sessions per capture or per phase: V-D17.
9. Pre-flight's graph-facing LLM checks: V-D21.
10. Severity scale: V-D25.
11. Verdict binding: V-D26.
12. CLI path: V-D27.

Open:

1. Whether `claude --safe-mode` drops an MCP server passed by `--mcp-config`; a probe at build decides the V-D18 flag set.
2. Whether `codex exec` can skip the project's `AGENTS.md`; a probe at build, else the served prompt states that project instructions do not bind the verifier.

## 9. Rejected alternatives

| Alternative | Why rejected |
|---|---|
| The verifier writes the full copy and its own hunk mapping | invented or incomplete mappings, silent rewording of untouched passages, light models unreliable on long files |
| sdd spawns a tool-enabled agent CLI for deep | Reopened by V-D16 as the headless launch (`claude`, `codex`), with the reach V-D18 fixes. The original concern stands as its accepted cost: sdd becomes an agent host with a sandbox and permission surface, kept apart from the isolated claude runner (`internal/llm/claude/claude.go:53-60`) |
| The drafter relays the subagent's findings | the judged party carries the judge's words; voa set this shape aside |
| The verifier runs its own retrieval while a reading exists | two searches whose lists can disagree (D40) |
| A long-lived verifier context as a requirement | depends on the harness, breaks under compaction |
| Writing guide and verifier in parallel | their edits conflict on the same body |
| The verifier after the confirm, or as a gate | contradicts 20r and D13 |
| Escalation as abort | loses the draft |
| A `verifier.rounds` config key | the owner's rule is fixed; smallest config surface |
| Multi-turn `pkg/llm.Request` | public contract change for content one re-sent prompt carries |
| Light as a tool-less harness subagent (V-D9) | still needs a subagent-capable harness and is no faster |
| One verification level only (V-D9) | no fast and safe option for self-contained graphs |
| Light as its own step beside pre-flight (V-D21) | overlapping axes judged twice, an advisory verdict beside a binding one |
| A separate `llm_verifier` model key (V-D21) | a second model key for one step |
| Cutting briefs to fit the light window (V-D11) | drops the entries nobody read closely, where duplicates hide |
| Refusing light when the reading does not fit (V-D11) | blocks captures where the graph is densest |
| Slicing in score order (V-D11) | clashing entries land in different slices |
| `context_chars` set by hand, or a guessed default window (V-D12) | stale on a model change, ignores the output share; a guess is too small or truncates silently |
| Index chunk text as excerpt, summary-only judging, skipping an oversized pair (V-D13) | mid-sentence cuts and overlap noise; detail lost; the pair never checked |
| Chunk offsets added to the vendored splitter (V-D13) | a change to the index derivation for a verifier need |
| Building only the voa and zpe parts deep needs (V-D14) | half-built engine machinery, a second path if it drifts |
| Waiting with light until voa and zpe ship (V-D14) | no verification for longer, though light depends on neither |
| `debate`, `review`, `audit` as mode names (V-D15) | arguing for its own sake; generic, collides with code review; taken by grounding D36 |
| `launch: subagent | headless` with a CLI key, or `runs: visible | hidden` (V-D16) | a second key dead under `subagent`; names what the user sees, not what runs |
| Superseding voa and zpe, one combined refinement, one child per round (V-D17) | restates unchanged commitments; mixes engine contract and launch artifacts; a fresh judge per round loses the exchange |
| Headless without any shell, or with the user's normal CLI setup (V-D18) | claims about history go unchecked; full write and shell power unwatched, project hooks in the judge's context |
| The driver relaying findings on stdout, a committed driver command (V-D19) | the launcher carries the judge's words; runs code a contributor never chose |
| Refusing without fallback, trying every known CLI, committed `launch` winning (V-D20) | a setting before first use; starts agents the project never named; forces a launch where it cannot run |
| Skipping the pre-flight re-run before deep (V-D22) | unchecked regressions, deep spends rounds on the cheap pass's job, a special path |
| The drafting agent weighing the skip, a separate `offer_skip` key (V-D24) | the judged party decides whether it is judged; two skip keys, four combinations |
| `substantive | minor` for deep, check categories without severity (V-D25) | two scales in one capture; nothing says what blocks |
| A `.verified` hash file, no binding (V-D26) | a second binding beside the engine's; an edit after the verdict written unverified |
| A stateless `sdd verify <draft>` (V-D27) | verification outside capture with no binding to the written entry |
| One init question per key, no init questions (V-D28) | an invalid pair refused after the fact; an unasked default is no deliberate choice |
