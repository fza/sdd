# Design: engine-enforced grounding for sdd captures

Status: consolidated design. Verdicts are proposed, open for QA, except the requirements in section 1 and the owner decisions in section 0.

Sources merged: two plans written independently from one brief ("plan N" and "plan S"), the reference implementation (an agent-side grounding flow with a prefilter cascade, run against a production graph), the reference gate (agent-side hook enforcement, to be retired), two verified library searches, and the sdd code and graph as of 2026-11-08.

Conventions:

- "Estimate" marks every figure not measured. Figures from the reference implementation and the reference gate come from their own documents and are not re-measured here.
- `file:line` refers to this repository. Every `file:line` in section 4 was re-read against the code.
- "Reading" means the set of grounding reads that credit one capture. "Candidate" means an entry a credited read listed. "Answer" means read at depth (bears) or set aside with a reason (does not bear).

## 0. Owner decisions

| # | Question | Decision | Why | Rejected | Accepted cost |
|---|---|---|---|---|---|
| D1 | Enforcement default (open question 3) | `grounding.enforce` defaults to `false` in every project; a project opts in with `true` | No existing or new project changes behaviour on update, which upstream acceptance needs | `true` for newly initialised projects only (two classes of projects behave differently; upstream refuses an unrequested gate); `true` everywhere (every existing graph starts refusing captures on update) | Each project that wants grounding sets the key itself |
| D2 | Who may switch enforcement (open question 1) | `grounding.enforce` resolves in layers: the user-global base may set `true` or `false`; the committed `.sdd/config.yaml` may set `true` or `false` and overrides the global base; `.sdd/config.local.yaml` may only set `true`, and a local `false` is refused with an error naming the committed file | A team that turns grounding on can rely on it for every capture, while one machine can still opt in where the project has not | Committed file only (no per-machine opt-in); normal layering (any machine can drop the gate with no trace in the repository, so the guarantee is only advisory) | The first asymmetric key in config resolution: new resolution logic, a refusal text, and tests for each layer combination |
| D3 | `sdd new` under enforcement (open question 2) | First slice: `sdd new` refuses and names the engine capture as the path. Follow-up slice: a session-aware CLI (`--session` on `gather`, `search`, `show`, `view`, `new`; CLI reads log into that session; `new` checks its reading against the draft) | No hole in the gate from the first slice on; CLI-only users regain a grounded path later | Recorded bypass alone as the answer (an agent can always take it) | In enforced projects, scripts and humans lose the plain CLI write until the follow-up slice ships |
| D4 | Recorded grounding bypass | `--skip-grounding` on `sdd new` and the same option on the MCP capture. The entry carries `grounding: skipped` in its metadata, and the engine always sets `confidence: low` on it. Every reader (gather, catch-up, prefilter, calibration) treats such an entry as low confidence whatever its `confidence` value; the forced `confidence: low` is only a safety net | An explicit escape hatch that stays visible in the graph, and ungrounded entries never pass as well grounded | A silent bypass; no bypass at all (blocks urgent captures) | A new metadata field, its rendering in `show`, `view` and catch-up, and low-confidence handling in every consumer |
| D5 | Recheck list (healing) | A committed binary recheck list beside the verdict ledger. It holds: (1) entries whose committed metadata or body changed by hand, which sdd detects from the git log; (2) inconsistencies an agent detects between live entries (contradict, overlap, and the like). `sdd catchup` tells the responsible participants about unresolved items and suggests a healing run. A healing run goes through every item and proposes changes, which become new entries through the normal capture procedure. Healed items leave the list automatically. Config can name the participants responsible for recheck runs; catch-up shows the recheck information and the healing suggestion only to them | Inconsistencies and hand edits become tracked, owned work instead of findings lost in one session | Healing only as seeded captures at the end of one capture (no standing list, no owner); groom's manual strained-pair sweep alone | A second binary format with its merge handling, git-log scanning, a catch-up section, a healing procedure, and a config key for responsible participants |
| D6 | Parallel healing | Two people healing the same item produce more inconsistency filings, so people are told to coordinate. Later, discussed first: a P2P feature where several MCP servers for one project on the network coordinate; the same idea on hosted sdd, so no two people run a healing in parallel | Coordination prevents duplicate healing work | Locking in the first slices (no shared runtime to lock in yet) | Until the P2P or hosted feature exists, coordination is a human duty |
| D7 | When the prefilter runs | The prefilter runs only when the number of pairs to judge reaches a configurable threshold. Below it, the agent gets the briefs of the entries the searches found (minus those already judged not bearing) and judges them itself. The prefilter can be disabled even when its LLM is configured | Small readings do not pay prefilter latency and cost, and the agent judges small sets well | Always prefilter when configured; prefilter switched on only by configuring a model | A threshold key and a separate on/off switch |
| D8 | Prefilter LLM config | The config gains an `llm_prefilter` block with much the same shape as `llm`, fully independent of it (own providers, models, keys, limits) | The prefilter's cheap model and provider never couple to the generation setup | A `bearing` operation selection inside `llm` (couples both); a `classify` capability under the planned provider config | A second LLM config block to document, resolve and validate |
| D9 | How "not bearing" is recorded (open question 21) | A `verdicts` report field on the capture (`verdicts: [{ids, why}]`), reported at `assemble` and at the drift step, declared `confirmExempt` so verdicts added after the confirm keep it. The CLI gets `sdd dismiss --session S <id>... --because "..."`, which records into the same session's verdicts | Verdicts sit with the draft they justify and are checked in the same report; CLI users keep a way to answer | A separate MCP `dismiss` tool (one more tool; dismissals apart from the draft) | A new engine attribute `confirmExempt`; `sdd dismiss` depends on the session-aware CLI (D3 follow-up) |
| D10 | Reason on a not-bearing verdict (open question 19) | A fixed reason code, no free text: `verdicts: [{ids, code}]`. Codes: `topic-only`, `other-scope`, `term-clash`, `covered`, `meta`, `other` (`superseded` removed by D12). The code sits in its own byte of the ledger record, with headroom for codes added later | Codes carry meaning a program can read (grouping at playback, reuse in later gathers, calibration analysis) and cost almost nothing in the binary ledger; free text adds size and puts private prose into a committed file | Free text kept only by session logs (nobody reads it again); free text in the ledger (size, privacy); codes plus optional note; `other` requiring a note | `other` gives no hint which code the set lacks; adding a code is a format-compatible change that readers must tolerate |
| D11 | Reason codes in calibration | Negative labels for threshold training come only from `topic-only`, `other-scope`, `term-clash`. `covered` is excluded (the candidate bears; an entry already read says the same). `superseded` and `meta` are excluded and handled by rules before the prefilter (a superseded entry is replaced by its live head; actor, role and procedure records never reach the prefilter). `other` is counted only. `sdd grounding status` shows precision per code (passed in full, then set aside with code X) and names the dominant code as a prompt revision hint; recall stays measured by blind audit draws | Thresholds train only on real relevance judgments, structural cases leave the pair volume, and prompt tuning gets a named direction | Training on every dismissal (`covered` would teach the prefilter to hide relevant entries; structural codes skew thresholds); label rules without diagnostics (prompt tuning gets only raw miss counts) | Code-aware calibration logic, its tests, and a diagnostics section in `sdd grounding status` |
| D12 | Superseded entries | Grounding candidates are strictly live: `gather` and the gate use default search (superseded excluded, `internal/query/search.go:53-57`) and live listings. The `superseded` reason code is removed; its slot stays free. Plain `view` listings also default to the live population (today `:active` is opt-in, `internal/viewlayout/reference.go:40`), with an explicit modifier for history. `show` chains keep history as context that owes no answer. Before implementation, upstream changes are folded in and checked for overlapping work on supersession display (open upstream gap 20261001-151002-s-tac-9lq: `show` names the supersede trail without resolving the live head) | Dead entries never owe answers or add pairs, and agents stop meeting them in listings | Live candidates with plain `view` unchanged (agents keep meeting dead entries in listings); keeping the code for chain entries (answers owed for history that never bears) | A behaviour change for every user of `view`, which needs upstream agreement and a migration note |
| D13 | Entries that landed after the readings began (open question 22) | Every new entry counts, checked twice. At `assemble`, every entry new since the reading began joins the candidates and takes the D7 path (briefs below the threshold, prefilter above). At `write`, after the confirm, only entries new since the assemble check are listed as briefs, and the agent answers each; no LLM runs there, per 20260810-141555-d-cpt-20r (only deterministic checks after the confirm) | Strict and matching the required flow, while the prefilter still carries the bulk before playback and the post-confirm step stays mechanical | Every new entry once after the confirm (the whole backlog lands where no prefilter may run); draft-scoped new entries only (misses an entry that bears without sharing a topic, term or vector rank) | On a busy graph the agent answers more entries |
| D14 | Storage of verdicts and the recheck list | Verdicts stay a binary ledger (`.sdd/bearing/`, committed behind its setting). Recheck items become events in the project event store of 20261107-115150-d-cpt-0mu: type `sdd.recheck`, subject the entry ids, retention until healed. This replaces the committed binary recheck file of D5; D5's contents, catch-up behaviour, healing run and responsible participants stand | The recheck list is a work queue whose items leave when healed, which matches events; a hosted event store gives the shared view that coordinated healing (D6) needs | Own binary recheck file behind a port (a second format beside an upstream store built for this kind of data); verdicts in the event store too (JSON events break the size requirement, and expiry drops calibration labels) | The recheck slice depends on the event store, which is not built and whose query surface is open (D15) |
| D15 | Who builds the event store, in which format (open question 31) | The event store of 20261107-115150-d-cpt-0mu is built in this work, on its own `pr/` branch before the recheck slice: its interface, envelope (type, time, writer, subject, payload), reserved `sdd.` namespace and append and query tools, with a query surface of type, subject and time. The local default is a binary append log sharing the ledger's codec, not the JSON files 0mu names. A refining entry states why binary replaces JSON. 0mu's only stated reason for JSON is that WIP markers need a default store at all; nothing in it or its refs argues JSON over binary | One binary codec serves the ledger and the events; the store stays small at large graphs; recheck and healing do not wait on upstream | JSON per 0mu (a second, text-based storage beside the binary ledger, growing faster); waiting for upstream (recheck and healing wait an unknown time) | The fork departs from 0mu's stated default and shapes its open query surface; upstream may reshape both, and the fork then adapts |
| D16 | Diagnostic tools for the binaries | The sdd CLI reads every binary file this design adds (verdict ledger, event log) and converts each to and from JSON | Binary files stay inspectable and repairable without custom tooling | No tooling (binary files become opaque) | Export and import commands with round-trip tests per format |
| D17 | File layout of the verdict ledger and the event stream (open questions 23, 28) | Both are directories of immutable segment files named `<time>-<random>.seg`: one directory for verdicts, and one directory per event channel (`.sdd/events/<channel>/`). Each event type belongs to one channel, and each channel has its own sharing mode, so fast-moving events and slow work items do not share a channel. Each write adds a small segment; no segment is rewritten. Tiered compaction merges small segments into a larger one past a count, dropping expired and healed events and duplicate records. Names never collide, so no git merge driver exists | Git stores each segment once, so push and pull traffic and checkout size stay small; no merge driver to build and maintain; expiry and healing keep the stream at open work | Monthly shards rewritten each commit with a union merge driver (rewrite traffic, a driver to maintain); one file each with a driver (whole file rewritten every commit); one stream for every event type (one sharing mode would apply to WIP markers, recheck items and outside events alike) | Compaction logic and its tests; duplicate records between concurrent compactions until the next one (harmless: verdicts join, events dedupe by id); Git history keeps compacted-away segments |
| D18 | Name of the pair relation (open question 13, part) | `relevance` replaces `bearing` in every identifier: the verdict folder `.sdd/relevance/`, the `llm_prefilter` operation `relevance`, verdict values "relevant" and "not relevant", and the `gather` sections "relevant", "to judge" and "set aside". Prose elsewhere in this document that says "bearing" means "relevant" | A plain word every reader and model knows, used for the same judgment in rerank APIs (Cohere's `relevance_score`) | `pertinence` (rare word); `concern` (also means worry or aspect); keeping `bearing` (also a direction or machine part, odd as a folder name) | Docs keep search ranking as "rank" so "relevance" means only this judgment |
| D19 | Sharing of event channels (open question 32) | Each channel has a sharing mode: `git` (new segments ride the next sdd auto-commit, never commits of their own) or `local` (machine state only); a configured hosted store replaces both. `recheck` defaults to `local` and may be set to `git` with `events.channels.recheck.share: git`. Every other channel, `wip` included, defaults to `local`; unknown channels default to `local` | Matches the off-by-default line, and one key turns team sharing on; slow work items can use Git without dragging fast-moving events into it | `git` by default for `recheck` (projects commit event segments without opting in); hosted or local only (sharing needs a hosted service) | Until a project sets `git` or a hosted store, recheck items stay on the machine that found them and catch-up elsewhere shows nothing |
| D20 | Content of the committed verdict ledger (open question 8) | Configurable: `relevance.content: reader \| all`. `reader` commits reader verdicts with their codes only (about 7 to 9 MB at 100k entries, estimate); `all` also commits filter scores with the model fingerprint (about 35 to 50 MB, estimate). Default `all`: a project that turns Git sharing on gets score reuse and pooled calibration with no second key, matching the committable-prefilter-findings requirement (open question 33). The `all` default holds only while the immutable segment layout of D17 stands; any layout that rewrites ledger files per commit reopens it | A project chooses between repo size and team-wide reuse of prefilter scores and pooled calibration | One fixed content (either size or reuse is lost for every project) | One more config value against the smallest-config requirement |
| D21 | First dedicated prefilter adapter (open questions 6, 7) | Cohere rerank ships in the first prefilter slice beside the generate bridge. Jev via Opper follows once access is open: Jev sat behind a gate (support contact or terms acceptance) when last checked, a few weeks before 2026-11-08 | Cohere rerank is openly available, batched (one call per reading, up to 1,000 documents) and of known cost | Jev first (gated access); both at once (two clients before any calibration data); generate bridge only (slowest and most expensive per pair) | One direction only: a "not relevant" rerank verdict answers only the draft-to-candidate direction. Reopened by the access probe of 2026-11-08: Jev answers through Opper without a gate (see the probe row below) |
| D22 | First dedicated prefilter adapter, revised (replaces D21) | Jev (`jev-1.14.0`) over the System One protocol through Opper (`POST https://api.opper.ai/v3/compat/v1/systemone`) ships first, beside the generate bridge. Cohere rerank (`aws/cohere/rerank-v3.5` via `POST https://api.opper.ai/v3/rerank`) follows as the second adapter. Both run behind one Opper key in `llm_prefilter` | In the probe Jev separated relevant from not relevant more widely (gap 0.33 against 0.21), costs least at normal reading sizes (about $0.001 per 150-pair reading against $0.002 per rerank call, estimates), reads both texts so a "not relevant" verdict answers both directions, and is the client the eval judge already plans | Cohere first (one direction, narrower gap); both in the first slice (two clients before any calibration data) | Jev's data is hosted in the US, with zero retention for enterprise customers only; one question per pair, batched per call within the 32k-token state limit |
| D23 | Prefilter port and protocol names (open questions 5, 25) | A batch-shaped public port in `pkg/llm/relevance`: `Scorer` with `Score(ctx, Request{Subject, Candidates}) (Result{Scores, Identity, Usage}, error)`, `Direction()` (both or forward) and `MaxBatch()`. Decorators mirror `pkg/llm` (`Bounded`, `RateLimited`, `Observed`) plus `PerItem` for one-pair providers. Protocol names in `llm_prefilter`: `systemone`, `cohere-rerank`, `generate` (the bridge over any chat provider) | Both chosen adapters are batch-native (Jev: many questions over one shared state; Cohere: many documents against one query); a new provider adds an adapter and a protocol name, never a core change | A pairwise port (each adapter re-derives the shared subject to batch); a typed-question port shaped like 20261106-155245-d-cpt-eit (more surface than relevance needs, couples to an open design D8 kept separate) | A second public package beside `pkg/llm` to keep stable, with its conformance suite |
| D24 | How the required searches stay on topic (open questions 4, 15, 27) | (1) An LLM writes the semantic and keyword queries from the draft. (2) That step runs only when the graph holds enough entries; below a size limit (about 50 to 100 entries, value open: question 34), the agent gets the briefs of every live entry and no search runs. (3) `grounding.enforce: true` requires a configured and reachable embedding model: sdd checks it before any step that costs LLM tokens and fails fast with a clear error when the model is missing or unreachable | Consistency needs searches that bear on what the draft commits to; a fixed rule such as the draft's first sentence cannot guarantee that (a draft may open with framing prose). On a small graph, all briefs cost less than any search and miss nothing | Queries from the draft's first sentence and key terms (the first sentence may be prose that bears on nothing); checking each agent query by embedding distance (a tuned limit; valid searches with other words refused); any search counts (off-topic searches satisfy the gate); waiving the semantic search without an embedder | One LLM call per gather above the size limit; enforcement is unavailable in projects without an embedding model |
| D25 | Size limit for all-briefs mode, and the query writer (open question 34) | `grounding.brief_all_below`, default 100 live entries: below it `gather` sends every live brief and runs no search. Above it, the `llm` block (generation) writes the queries as its own operation (`gather-query`), which can point at a small model; `llm_prefilter` cannot serve it because Jev generates no text | The search path starts early and a project tunes the limit; query writing needs a text-generating model | A fixed limit (no tuning); default 200 (measured cost parity: about 20 tokens per brief, so 100 briefs about 2k tokens, 500 about 10k, against about 3k to 5k for the search path, estimates) | Graphs between 100 and about 300 entries pay for search where briefs would cost less, until tuned |
| D26 | Reported contradiction between existing entries (open question 9) | A reported inconsistency between live entries A and B becomes a recheck item (D5, D14). The capture is refused while its draft refs A or B and the A-B item stays unhealed; a draft citing neither continues. On refusal the agent decides, bringing it into the dialogue and letting the owner decide where the context demands: heal now, redraft around the conflict, hold the draft (WIP), drop the topic, or another choice the dialogue opens | No new entry builds on a known conflict, while unrelated work is not frozen | Never blocking (more recheck items later, entries built on conflicts); always blocking (any reported conflict freezes unrelated captures) | Captures citing a conflicted entry wait for a healing run or a redraft |
| D27 | Prefilter takeover threshold (open question 35) | `grounding.prefilter_from`, default 30 pairs to judge: below it the agent judges the briefs; from it on the prefilter judges first and the agent judges the band it leaves | About one screen of briefs (about 600 tokens, estimate) is judged carefully; the prefilter's money cost is near zero (Jev: 5 pairs for about $0.00003 in 0.49 s), so the limit guards the agent's attention, not cost | 50 (lists of 30 to 50 briefs, where skimming starts); 15 (prefilter on nearly every reading, more calibration data needed early) | Readings of 30 or more pairs make one prefilter call |
| D28 | Reading window (open question 30) | Only reads made inside the capture instance count. A full view of an entry the agent was already served is not served again: sdd prints its id with its brief (title or first sentence, never cut) as a pointer, and the read still counts for the instance. The brief always shows, as the reference implementation settled after an id-only pointer proved too thin. How the pointer survives an agent context compaction: open question 36 | The strictest tie between reads and the draft, without paying twice for content the agent already holds | From the end of the previous capture (reads made for other work can cover the draft); the whole session (a second capture passes on the first capture's reads) | Agents start the capture before reading; the pointer scheme depends on the agent still holding what was served |
| D29 | No truncation in agent-facing grounding output | Nothing sdd serves for grounding (`gather` sections, briefs, pointers, refusal lists, drift lists) is truncated: no character cuts, no "first N" with the rest dropped. Long lists are paged or folded with an explicit count and the call that fetches the rest, never silently cut | Measurements in the reference implementation showed truncation lowers the agent's judging accuracy | Cut briefs at a fixed length (fewer tokens, lower accuracy); cap list lengths (entries silently unjudged) | Larger serves on big readings; the prefilter and the size limits (D7, D25, D27) carry the token budget instead |
| D30 | Text sent to the prefilter (open question 37) | Whole entries (kind, topics, title, body), never cut. Each call is filled up to the provider's limit (Jev: 32k tokens for state plus question); an entry too long for one call on its own is not scored and goes uncut to the agent's "to judge" list | The accuracy argument of D29 holds for the prefilter too: a constraint deep in a body must not be lost, and no entry is dropped silently | A fixed cut per entry (scores judged on fragments); cutting only the draft (loses detail on the side every pair depends on) | More calls on readings with long entries; a size estimate per entry |
| D31 | `show` depth and the credited read path (open question 10) | Plain `show` keeps up 2, down 1 and changes in nothing. Under enforcement, a capture requires `gather`: grounding reads count only through `gather`, which serves at the configured grounding depth (default down 2, up 1). Opening a "to judge" entry in full for credit also goes through `gather` (naming the entry ids), so the depth rule is enforced in one place | `show` keeps its own purpose (an entry's "why") for every user, while the capture gate gets one read path whose depth it controls | `show` following the grounding depth under enforcement (a config-dependent `show` default); a refusal on short `show` reads followed by a second `show` (body paid twice) | Agents learn one more verb; `show` reads never count toward grounding |
| D32 | The widen report under enforcement (open question 11) | The engine seeds `widenReport` from the capture's reading, so `hasWidenReport` holds without agent prose. The seed lists every read verbatim: each semantic query text, each keyword term, each topic listing, with the ids each surfaced; then the entries read and every verdict with its reason code. The agent may add prose below. Text inherited from a dispatching move stays above the seed. A role that shares no context with the drafting agent (a dispatched capture, a reviewer, a healing run) learns from it what was already searched and why the counts stand; it gets no read credit from it (D28), but stored verdicts (D14) return already-judged pairs as decided, so re-running the same reads costs no new judgment | Agents in different roles share no context, so the queries behind the counts must travel with the handoff; the log is the record and the prose only retold it | Keeping the agent's narrative (repeats the log, can disagree with it); retiring the field under enforcement (spec branch on enforcement, the handoff text loses its field); counts only (a later role cannot tell what was searched) | Part of `widenReport` becomes engine-written text; the seed grows with the number of reads |
| D33 | When the prefilter may set entries aside (open question 12, recall part) | The prefilter sets an entry aside only once the Wilson lower bound (95%) of its recall reaches 0.98; recall counts the entries a capture ended up referencing. No fixed label minimum and no starting thresholds per adapter: until the bound holds, the prefilter only sorts, the agent judges every candidate, and labels accrue. The bound needs 189 referenced-entry labels at 0 misses, 280 at 1, 361 at 2 | Nothing leaves the agent's view before the system has measured that it almost never hides a relevant entry, which the consistency goal requires; 0.98 reaches savings about twice as fast as 0.99 | Floor 0.99 as in the reference (381 labels at 0 misses, about 125 captures before savings, estimate); starting thresholds from day one with 0.98 after 200 labels and 5 misses allowed (uncalibrated set-asides early; 200 labels with 5 misses only reach a floor of about 0.94) | No savings for about the first 60 captures (estimate, at 3 referenced entries per capture) |
| D34 | The pointer for an entry served in full earlier in the same capture (open question 36) | The pointer carries the id, the brief and the way back: `gather` with the entry ids, or `resume_session`. No check word | Measured on Claude Code and Codex with real compaction: 80 of 80 verdicts correct for the pointer with brief and way back against 68 of 70 for a check word, at about a tenth of the output tokens; counting words drew attention from the verdicts | A check word at a random position the agent must quote (6 to 16 times the output tokens, 2 wrong verdicts in 70; it caught invented reads in one run); deciding only after a second spike with lossy compaction of tool-result serves (the design stays open meanwhile) | An agent that judges an entry from memory after losing its text is not caught; neither harness produced that loss in the spike (`compaction-spike/report.md`) |
| D35 | What the full block of a `gather` serve holds, and its bound (open question 12, cap part) | The full block holds each prefilter-rated relevant entry's own body, once, up to a constant 12 in code. The grounding depth (default down 2, up 1) decides which entries become candidates, not what is printed: chain members are rated and shown like any other candidate. Overflow moves to "to judge" as one line each, with the count and the `gather --ids` call that opens them (D29). Amends D31: its depth is candidate scope | The agent must read every relevant body in full before relying on it, and serving it in the same call saves a round trip; printing chains inside the full block repeated entries that are candidates anyway. Measured: entries median 2.9 KB, p90 7 KB, max 61 KB (1,941 entries); one entry with its chain at down 2, up 1 is 8 to 31 KB | Cap 12 with chains inside (one serve up to about 93k tokens); no cap (no bound, hub chains reach tens of thousands of tokens); a byte budget (one more number without a measurement); a config key `grounding.full_cap` (more config surface) | A broad draft needs one more call; 12 stays an estimate until `sdd grounding status` reports overflow |
| D36 | Audit share and judge change (open question 12, audit part) | A constant 3% in code of the set-aside entries is shown blind to the agent as ordinary "to judge" lines; a referenced sample counts as a miss for the drift rule (above 2% over the latest 200 audit labels, minimum 50, steps the threshold down at most once a day). On a judge change the new judge scores the stored labelled pairs and its recall floor (D33) is computed from them; no boosted sampling | Reader labels do not depend on the judge, so stored pairs calibrate a new judge at once for the cost of one scoring pass; 3% lets the drift rule act after about 17 captures (estimate, at about 100 set-asides per capture) | 2% as in the reference (drift minimum after about 25 captures); 5% (about 5 lines per capture); 6% after a judge change until 200 labels (moot once D33 dropped the label minimum and stored pairs re-score) | About 3 brief lines per capture (estimate); one scoring pass over the stored labelled pairs per judge change |
| D37 | Upstream vocabulary (open question 13) | Kept: `gather` (the read command, CLI and MCP), `reading` (the record of the reads inside one capture), `strains` (two existing entries that contradict, duplicate or outdate each other; groom already uses "strained pairs"), `verdicts` (D9), `relevance` (D18). The capture step at `write` that serves entries landed after the reading began is `lateEntries` | Each name says what it holds and collides with no name in the procedure specs; capture already uses drift for a summary that no longer matches its body (choice `drifted`), so a second drift would confuse | `graphMoved` (vaguer about what the step serves); `groundingDrift` (two meanings of drift in one procedure) | None beyond the rename |
| D38 | Kinds exempt from grounding (open question 14) | `actor` and `role` skip the grounding gate as drafts and never become prefilter candidates; every other kind, `focus` and `procedure` included, is gated as a draft and is a candidate. Amends D11: the `meta` set is `actor` and `role` | Only records of who takes part and in which role cannot contradict, duplicate or outdate a claim; a focus commits to a direction and a procedure sets process rules, so both can clash with other entries. The draft exemption and the candidate exclusion cover the same kinds, so nothing checked as a draft is invisible to later drafts | All four kinds exempt both ways (focus and procedure captures go unchecked, a process decision can contradict a procedure unseen); `actor` and `role` exempt as drafts with D11 unchanged (procedures checked when drafted but never surfacing as candidates) | Long procedure specs cost prefilter tokens as candidates; an oversize one goes uncut to "to judge" (D30) |
| D39 | Entries read in full but not referenced (open question 18) | Every entry served in full inside a capture ends either in the draft's refs or in `verdicts` as `{ids, code}` (D10); the assemble gate refuses otherwise, naming the open ids. The reader state "opened but not referenced" no longer exists; each such dismissal is a negative label for calibration | A relevant entry read and silently left out is the gap grounding exists to close; it applies the requirement that capture passes only once every entry not relevant is dismissed to full reads | Required only for the prefilter full block, `gather --ids` opens exempt (those reads can still be skipped silently); not required (a contradicting entry can be read and ignored, labels lose those negatives) | Up to 12 short `{ids, code}` items per capture (D35 bound) plus any entries opened through `gather --ids` |
| D40 | Relation to the graph-fit guide of 20260810-141555-d-cpt-20r (open question 20) | The capture's reading (relevant entries, the "to judge" set, refs, verdicts, strains) feeds the graph-fit guide as its neighbourhood; a judge outside the drafting context checks the draft against it. A small model suffices only while the judge works on graph entries alone; a verifier that opens referenced files and URLs, searches the web and calls other tools needs a strong model. The verifier therefore comes in a light and a deep variant chosen by configuration (amended by verifier V-D15 and V-D21: light is pre-flight over the reading on the `llm` model, deep is the tool-using exchange); its procedure (findings, accept or contest, proposal as a diff, bounded rounds, escalation to the owner) is settled in its own QA round (open question 38) | One graph search per capture, upstream's planned guide gets built, and a second judge checks the drafting agent's dismissals | Dropping the guide (the drafting agent stays the only judge; upstream would replace its own decision); a guide with its own retrieval (two searches per draft whose lists can disagree) | One more LLM call per capture in light mode; a strong-model run with tool calls in pro mode |
| Finding | Jev access probe, 2026-11-08 | `POST https://api.opper.ai/v3/compat/v1/systemone`, model `jev-1.14.0`, one `noul` relevance question over a draft and a candidate (two short decisions on grounding enforcement): HTTP 200, `noul` 0.56, 362 input tokens, 22 output tokens | At the documented $0.042 per million input tokens (output free), one pair costs about $0.000015 (estimate), against about $0.0004 per pair on a small chat model and about $0.002 per rerank call (reference implementation figures); several questions per call cut cost further | 0.56 on a pair a reader would call relevant shows raw scores need calibrated thresholds, not a fixed 0.5 cut | |
| Finding | Jev against Cohere rerank, 2026-11-08 | One draft (`sdd new` refuses under enforcement), five candidates (three relevant, two not), both through Opper with one key. Jev (`jev-1.14.0`, five `noul` questions in one call): relevant 0.37, 0.59, 0.74; not relevant 0.04, 0.04; 0.49 s; 706 input tokens (about $0.00003, estimate). Cohere (`aws/cohere/rerank-v3.5` via `POST https://api.opper.ai/v3/rerank`, instruction-prefixed query): relevant 0.40, 0.26, 0.36; not relevant 0.05, 0.05; 0.60 s; $0.002 per call (reported) | Both separate the two groups. Jev's gap between the lowest relevant and the highest not-relevant score is 0.33, Cohere's 0.21. Jev costs per token and reads both texts; Cohere costs per call (batches up to 250 documents) and reads one direction | Five pairs prove nothing statistically; the eval specimen set must measure recall before thresholds are trusted |

## Contents

0. Owner decisions
1. Purpose, goal, requirements
2. Findings from the reference implementation
3. Findings from the reference gate
4. Ground truth in sdd today
5. Design points (5.1 to 5.14)
6. Tools weighed (prior art)
7. Consolidated plan
8. Open questions for the owner
9. Rejected alternatives

## 1. Purpose, goal, requirements

### 1.1 Goal

Agents that draft graph entries (signals, decisions) must bring drafts that do not:

1. contradict decisions in live entries;
2. duplicate signals or decisions already in the graph;
3. state something outdated;
4. bring the graph into an inconsistent state.

The graph must also heal: when a reading of several entries shows one of these rules broken between existing entries, that becomes graph work.

These are not deterministic checks; every graph differs. The approach is enforced reading plus judgement:

1. Before an agent can capture, it must have run at least one semantic search bearing on the topic, at least one keyword search bearing on the topic, and a full listing of all entries on the topic.
2. With the results at hand, the agent decides what bears. Entries that look bearing must be read at a configured depth. Entries that do not bear must be set aside (dismissed).
3. Capture proceeds only when the agent has told the engine it did the readings and every non-bearing entry is dismissed.
4. Before the final write, the engine re-checks the graph. Entries that appeared after the readings began make it refuse the capture and ask the agent for its verdict on them.

### 1.2 Requirements (decided)

| # | Requirement | Source |
|---|---|---|
| R1 | Enforcement lives inside sdd (engine, procedures, application layer). No agent hooks, no harness plugin: sdd is harness-agnostic, CLI and MCP are shells over one application layer, upstream refuses harness-specific machinery. | brief, task |
| R2 | A new `gather` command (CLI and MCP read) returns in one call everything the agent needs to ground a draft topic. Token savings are key. Bearing entries print in full at the configured depth, others in brief or as counts. | brief |
| R3 | Stored verdicts: bearing or not bearing between entry pairs, and between a draft and an entry. Bearing is symmetric. Verdicts are reused. | brief |
| R4 | A new LLM surface, the prefilter: a cheap model judges whether two entries bear on each other, so the main agent is spared most of the work. `gather` prints only bearing entries in full. | brief |
| R5 | The prefilter uses an adapter architecture: Cohere, Jev and future providers plug in with no change to sdd's core. | brief, task |
| R6 | The prefilter is off by default and has the smallest possible configuration surface. | brief, task |
| R7 | Prefilter calls run in parallel: large graphs must not make performance collapse. | brief, task |
| R8 | The prefilter calibrates itself: threshold or prompt tuned against ground truth, drift detected. | brief |
| R9 | Prefilter findings can be committed behind a setting that is off by default: a ledger file, binary, small even for large graphs, most likely with a git merge driver. | brief, task |
| R10 | Grounding is configurable. Read depth `--down 2 --up 1` is only the default. | brief, task |
| R11 | Follow `AGENTS.md`: CQRS layering, push logic down into `internal/model`, fail loud, tests at the owning layer, minimal comments, cost framed as maintenance surface and dependencies. | brief |
| R12 | Durable output never names the reference projects. They are "the reference implementation" and "the reference gate". | brief |
| R13 | The design must be acceptable upstream: opt-in, harness-agnostic, generic surfaces. | brief |
| R14 | Every sdd verb that returns anything about an entry (metadata, title, summary, body) follows the no-repeat rule: what was already served to the agent in the same session is not served again; a repeat shows as a pointer with the brief and the way back (D28, D34). The no-repeat cuts happen only in MCP mode; the CLI never cuts, in session mode too. Scope check over all such verbs follows the design. | owner |

## 2. Findings from the reference implementation

### 2.1 The flow

1. Every graph read runs through a wrapper that names a subject entry. A per-subject ledger records what each read listed. The ledger is keyed by subject, not session, because subagent calls carry the parent's session id.
2. A capture needs the subject's chain read at `--down 2 --up 1`, every area topic of the subject listed (live members only), one semantic and one literal search, and every listed candidate opened or dismissed.
3. A filter answers most candidates before the reader sees them: a trained logistic score (no model call), a reranker for what the score cannot cover, and a small chat model for the band between.
4. Verdicts live in SQLite per (subject, candidate, source). They never expire. They also answer the reversed pair unless a one-way scorer made them.
5. At capture, a door judges every entry that landed since the capturer started, against the subject and against the draft. It refuses until a reader answers each entry that may bear.

### 2.2 Rules

| Rule | Detail |
|---|---|
| Owed reads | subject chain at depth floors 2 down, 1 up; each area listing (again when the area grew); a semantic search; a literal search; one call (`gather`) for the three |
| Owed answers | every result a read lists owes open or dismiss. A rule that owed only the first 8 per read left 12% of query results a later entry cited unweighed, at ranks 9 and 10 |
| Live listing | retired and `done` entries owe nothing |
| Excused | entries the tool holds outside the repository (procedure definitions, participants) |
| Brief view | answers nothing; still owes open or dismiss |
| Dismissal | reason required; empty call refused; every name resolves before anything lands |
| Term hits | never sent to the filter: a literal token is a lookup and owes a reader's answer |
| Bearing (model prompt) | a candidate bears when it decides, constrains, contradicts, supersedes, renames, refines, or is required by anything the subject commits to, or already owns part of the subject's question. Sharing only an area or a word is not bearing. Unsure means bearing |
| Bearing (reader) | opening in full records bears; dismissing records not bearing with the reason |
| Bearing (training label) | a candidate bears where the entry the reading produced references it |
| Query terms | chosen by the agent; one hand-off derives query = first sentence of the summary cut at 160 characters, term = first area label, else the id |
| Inherited reading | a recorded reading with no entry landed since its corpus mark answers "grounded: inherited" |
| Draft references | a draft reference to an entry the reading never opened is refused |

### 2.3 `gather` in the reference implementation

1. Builds the reads (query search, each term search, each area's live listing). Runs them up to 6 at once.
2. Records each read in the ledger in fixed order. A failed read stops the call.
3. Dedupes: each entry once, tagged by the reads that found it, sorted by number of reads that found it.
4. Sends ids with no verdict and no term hit to the filter in one call.
5. Output tiers: shown entries one full line each; left-out query hits one line with the filter's reason (cut at 280 characters); left-out listing-only hits as counts per source; optional rest of each area (retired or done).
6. Every entry left out lands in the ledger as dismissed with a source prefix (`filter:`, `reversed:`, `deferred:`).
7. Lines already held print as "shown above, unchanged". A third identical `gather` prints one line; the 20th raises an alarm.

### 2.4 Numbers measured

| Measurement | Figure |
|---|---|
| Chat filter, 300 labelled pairs, flash-class model | kept 138 of 150 bearing (recall 92%), set aside 111 of 150 others, about $0.0004 per pair |
| Chat filter, same pairs, `claude-haiku-4-5` | kept all bearing, set aside 27%, about $0.003 per pair |
| Fallback routes, 240 questions | agreement 82% ($0.21 per 1,000), 83% ($0.42), 75% ($0.64) |
| Reranker | every bearing candidate the chat model missed scored at least 0.096; about $0.002 per call |
| Trained score at first recording | recall 99.1%, set aside 20.6%, 14.8% sent to the chat model |
| Trained score, latest training | test on 390 readings, 49,329 pairs, 883 bearing; recall 99.09%, set aside 9.9%, to chat model 6.9% |
| Chat model alone, latest training | recall 99.3%, set aside 1.2% |
| Chat model alone, first measurement | left about 7% of referenced entries set aside |
| Audit, referenced label | no referenced miss among 195 judged set-asides |
| Candidates per read | median 213 ids, 519 at most |
| Bearing share | 883 of 49,329 pairs (1.8%) |
| Graph reads in a job | about a quarter of what a job reads back |
| Repeated text | 21% of chain and listing lines, 18% of bodies repeated text already held |
| Depth 3 down | 89% more text for a level opened 0.7% of the time; 11% of subjects carry a three-step refinement chain |
| Hidden done and closed entries | 11% got opened later, 5% named later |
| `gather` itself | no measurement |

### 2.5 Thresholds and limits in use

| Item | Value |
|---|---|
| Recall target for training | 0.99 |
| Model share of a pass | 0.15 |
| Audit share of set-asides | 0.02 |
| Audit tighten trigger | referenced-miss share above 0.02 over the latest 200 labelled pairs (minimum 50), one step down at most once a day, never up |
| Reranker bands | below 0.06 not bearing, from 0.25 bearing |
| Chat calls | one candidate per call, up to 32 at once, pass deadline 75 s |
| Reranker batch | 250 documents per call, batches in parallel within half the deadline |
| Wrapper wait for the filter | 90 s |
| Door deadline | 45 s; up to 3 tries when entries land during the wait |
| Text cut | 6,000 characters per entry for the chat model; 8,000 for the reranker query |

### 2.6 Failure behaviour

| Failure | Result |
|---|---|
| Every chat route refused, unreadable answer twice, transport failure three times | candidate counts as bearing |
| Pass deadline | unanswered, counted as bearing, not stored |
| Reranker fails or no key | candidates go to the chat model |
| Filter command fails, times out or prints non-JSON | nothing filtered; every line prints in full |
| Model share exhausted | deferred: set aside for this pass only, no verdict |
| At the door, no answer | "unknown (no answer): judge it yourself", blocks |

### 2.7 Never built

1. Grounding a draft with no existing subject entry (the subject is always an existing entry).
2. Invalidation of a verdict when a candidate is superseded, by age, or when the subject's draft changes.
3. Any path for a person to issue a verdict outside the reader role.
4. Healing as a mechanism: no code detects a contradiction, duplicate or outdated statement among entries one reading opened, and no code turns one into graph work. The bearing prompt treats "contradicts" as bearing, but the reason goes only to the ledger and the log. Around it: role texts file findings, verifier briefs flag duplicates, findings are deduplicated at 0.9 cosine.
5. A measurement of `gather`'s own saving.

### 2.8 Rejected in the reference implementation, with reasons

| Rejected | Reason |
|---|---|
| Batching candidates in one chat call | recall unmeasured for that form ("would halve the input at a recall nobody measured") |
| Counting a refused question as bearing instead of falling back | sends the whole throttled share to the reader |
| Two weaker fallback models | agreed least (75%, 62%) |
| Reranker as first stage | calls grow with candidates, where the trained score costs none |
| Bounding collected candidates per reading | drops bearing entries in large areas |
| Larger chat-model share | recall on what the score would pass is unmeasured |
| Reranker and chat model on every candidate | full chat price, same recall |
| Chat model alone | left about 7% of referenced entries set aside (first measurement) |
| Filtering for verifiers | rests quality on filter recall alone |
| Three reads with dedupe instead of `gather` | saves less, still three calls |
| Owing only the head of each read | 12% of later-cited query results sat at ranks 9 and 10 |
| Reading three levels down | 89% more text for a level opened 0.7% of the time |
| Hiding done and closed entries | 11% opened later, 5% named later |
| Duplicate refusals in two hooks | every refusal arrived twice |
| Gradient-boosted score | logistic regression held close recall with the standard library only |

## 3. Findings from the reference gate

### 3.1 What it enforces

| Rule | Detail | Strength |
|---|---|---|
| Reads owed | a semantic search, a literal search, and a listing (all-active counts, or one topic carrying a configured prefix) before a question, a draft edit, a capture wrapper call, `sdd new`, or an engine capture start | questions and draft edits: let through with a note; graph writes: hard refusal |
| What counts | any limit, any subject; only a call that succeeded | |
| Show depth | every `show` owes `--down 2 --up 1`; an omitted MCP `down` counts as the server default 1 and is refused; `prc`-layer entries may read shallower | refusal echoes the call at the owed depth |
| Discarded stream | any `sdd` call redirecting a stream to `/dev/null` is refused, because a wrong flag prints usage to stderr and reads as an empty graph | refusal |
| Engine first | the CLI capture wrapper is refused where the project declares an sdd MCP server | refusal |
| Engine capture rules | topic prefix on `topics`; project lint on every body the capture holds (whole body, patch, staged handle) | refusal |
| Credentials | every graph write scanned (gitleaks default set plus local rules), fail-closed | refusal |
| Credit lifetime | until graph state changes (file count plus newest file name) or the context compacts; any session's capture voids every session's credit, except the session's own capture when it is the only change | |

Two facts: the reference gate keeps no ledger of returned, opened and dismissed entries, and it has no whole-entry check.

### 3.2 How it tracks state

1. One JSON file per harness session (and per subagent) under the graph repository's scratch folder.
2. Keyed by the harness's hook payload (session id, agent id). Subagent reads never credit the parent.
3. Fields: read counters per mode, listings credited, graph state at record time, the last note's missing set, grounded draft hashes, open engine captures by instance handle, staged bodies.
4. Lifecycle: deleted at session end; swept after a week without a clean end.
5. Parsed from: shell command text (parsed as bash) to find `sdd` calls and flags; capture wrapper stdout; MCP serve JSON (instance handle, status, produced entry id); `next` reports and `start_procedure` params; `sdd --help` and `sdd info` capability probes.

### 3.3 Stock-sdd quirks it works around, and the sdd behaviour that retires each

| # | Quirk | Workaround in the reference gate | Retired when stock sdd | Retired by this design |
|---|---|---|---|---|
| 1 | Agents ask, draft and capture from belief; sdd records nothing about what was read | reads ledger, gate on question, draft, write | records the session's reads (both search modes, a listing) against the graph revision and refuses capture until they stand | yes (5.1, 5.4, 5.5) for capture; questions and draft edits stay ungated (reads free, d-tac-dbk) |
| 2 | `show` defaults to `down 1` | show-depth refusal | defaults grounding reads to `down 2, up 1`, or owes that depth inside a move | yes: depth owed inside capture for credit; `gather` is the credited read path at grounding depth (5.2) |
| 3 | Wrong flag: usage on stderr, non-zero exit, swallowed by `2>/dev/null` | discarded-stream refusal | moot once reads go through MCP; else errors that cannot read as an empty result | partly: MCP path moot; CLI unchanged (out of scope) |
| 4 | Engine capture ignores the project's topic prefix and lint | prefix and lint checks on every engine report | reads both from project config and enforces them in the capture procedure | no (out of scope; listed for completeness) |
| 5 | No credential scan on a write | gitleaks scan, fail-closed | scans every write path with the pinned set plus local rules | no (out of scope) |
| 6 | Body through a shell word runs backticks | wrapper passes the body as one argv item | engine capture or a body-from-file flag (`bodyHandle` exists) | already retired by `bodyHandle` |
| 7 | Pre-flight provider faults fail the capture; no per-try bound or fallback | retries, limits, route fallback | retries provider faults itself, bounded, with fallback models | no (eit's caller-side retry decorator covers it) |
| 8 | Concurrent captures on one graph collide | file lock | serialises its own writes | no (out of scope) |
| 9 | Schema or title mismatch surfaces as an opaque failure | header and capability checks | states the mismatch and the fix | no (out of scope) |
| 10 | CLI capture beside a served engine skips playback | engine-first refusal | moot once the CLI capture wrapper retires | yes when `sdd new` refuses under enforcement (5.10) |
| 11 | Served-once token filter does not track compaction, clear, resume or per-subagent context | per-agent collapse sets, emptied at compaction | served-once memory resets on a context reset and is per session handle | partly: `gather` reuses the served-block memory, which `resume_session` already resets |

### 3.4 Known holes, and whether this design closes each

| # | Hole | Closed by this design |
|---|---|---|
| 1 | One harness only; other harnesses or sessions without the plugin are ungated | yes: enforcement is in the engine (R1) |
| 2 | Credit is "the call ran", not "the agent read it"; any subject counts; a pipe to `head -1` still credits | partly: every listed candidate owes an answer; listings tie to the draft's topics; search relevance stays unverified (open question 4) |
| 3 | No check that relied-on entries were shown | already closed by `refsInspected` (`internal/engine/predicates.go:224-244`); depth now checked too |
| 4 | Graph state is file count plus newest name; a summary rewrite does not void credit | yes: corpus mark holds an id set; summary rewrites do not change bearing (bodies immutable) |
| 5 | Any other session's capture voids everyone's credit | yes: late entries are checked per capture instance (5.5), not globally |
| 6 | Questions and draft edits only noted | by design: reads and questions stay ungated (d-tac-dbk) |
| 7 | Engine report to an instance the gate never saw start passes | yes: the engine sees every instance |
| 8 | User- or local-scope MCP server not seen | yes: no detection needed |
| 9 | One-call reads credited by word match | yes: `gather` logs what it ran |
| 10 | Scanner limits | not in scope |
| 11 | Compaction known only through one harness event | yes: evidence is engine state, not context |
| 12 | Subagent reads never credit the parent | by design: a delegate's child session keeps its reads out of the parent's evidence (20260827-232430-d-cpt-voa); seeded children in the same session do get the parent's reads (`internal/engine/session.go:45-50`) |

### 3.5 Retired with the plugin, no sdd counterpart needed

Draft deletion after capture, the session-start machine check, the daily release-folder prune, the binary launcher.

## 4. Ground truth in sdd today

### 4.1 Facts (merged from both plans, each re-checked)

| # | Fact | Location |
|---|---|---|
| 1 | `EventRead` records tool, entry ids, depth; "tracking is logging, not gating (d-tac-dbk)"; session-level, so a dispatching parent's reads count for seeded children | `internal/engine/session.go:45-50` |
| 2 | `ReadDepth` has only `summary` and `full`; no down/up span | `internal/engine/session.go:66-74` |
| 3 | Session field `reads` folds read events into the deepest depth per entry | `internal/engine/session.go:231-235` |
| 4 | `LogRead(tool, full, summary)` appends one event per call; `foldReads` dedupes, full never downgrades | `internal/engine/session.go:329-348`, `:350-366` |
| 5 | Served-block memory (`ServedBefore`, `RecordServed`) for "served above" stubs | `internal/engine/session.go:374-395` |
| 6 | Replay restores only ids and depth from `full` and `summary` | `internal/engine/session.go:1080-1096` |
| 7 | `Context.Reads` is the folded read set handed to predicates; wired at three sites | `internal/engine/registry.go:37-49`; `internal/engine/instance.go:221`, `:344`; `internal/engine/mutation.go:69` |
| 8 | `Predicate.FailDetail` renders a refusal against the live context | `internal/engine/registry.go:63-73` |
| 9 | `refsInspected` predicate: every draft ref served at full depth; refusal names the ids | `internal/engine/predicates.go:224-244`; helper `uninspectedRefs` at `:539-575` |
| 10 | Guard grammar: predicate names with `and`, `or`, `not`, parentheses | `internal/engine/guard.go:9-20` |
| 11 | `Store.StateSnapshot` hashes every state-provenance value; a confirmation binds to it, so any state edit after confirm reopens playback | `internal/engine/store.go:351-375` |
| 12 | Base types are a closed set | `internal/engine/types.go:22-50` |
| 13 | `VarDecl` has `Type`, `Optional`, `Desc`, `Default`, `PatchOf`; no confirm-exempt attribute; YAML shape mirrors it | `internal/engine/spec.go:58-78`, `:216-222` |
| 14 | Capture spec: `serveBudget: 58000` | `internal/baseprocedures/entries/20260703-094500-d-prc-cap.md:6` |
| 15 | Capture spec state declarations; `widenReport` is required prose ("searches run and entries inspected before drafting") | same file `:19-50`, `:37` |
| 16 | Capture assemble gate: `hasBody and hasWidenReport and refsResolve and refKindsValid and refsInspected and participantsCanonical and roleActorResolves and draftValidates and specLoads` | same file `:52-78` (when-clause `:74-77`) |
| 17 | Playback (user chooser, confirm, adjust, abort), preflight after confirm, `write` (`capturePreflightCurrent` or back to preflight), `publish` (`newEntry`), `verifySummary` | same file `:94-101`, `:102-108`, `:109-114`, `:115-119`, `:127-135` |
| 18 | Grounding lane text: search from several angles, record in `widenReport`; inspection checked mechanically | same file `:157-165` |
| 19 | Groom dispatches seeded captures (`widenReport: candidates`) | `internal/baseprocedures/entries/20260706-171500-d-prc-grm.md:32-37` |
| 20 | Groom already probes "strained pairs" by hand (directives that contradict, premise undercut) | same file `:60` |
| 21 | Application registers `capturePreflightCurrent`, `preflightEntry`, `newEntry` (with `Prepare`) | `pkg/application/workflow_registry.go:286-312` |
| 22 | `runWorkflowNewEntry` publishes and logs the written entry as a full read | `pkg/application/workflow_registry.go:447-479` (log at `:477`) |
| 23 | `entryChains` inject calls `LogRead`, so it is not serve-safe | `pkg/application/workflow_registry.go:138-190` |
| 24 | `prepareWorkflowNewEntry` runs before the intent is recorded: verifies preflight, mints the id | `pkg/application/workflow_capture.go:65-90` |
| 25 | `capturePreflightState` scans stored session events: precedent for predicates over the event log | `pkg/application/workflow_capture.go:160-246` |
| 26 | Confirm-time check precedent: staged header checked at playback confirm; `callsConfirmPlayback` | `pkg/application/workflow_staged_body.go:89-107`, `:109-121` |
| 27 | Application forwards reads to the engine session | `pkg/application/workflow.go:953-957` |
| 28 | MCP reads log through `logRootRead`; search logs hit ids as summary, show logs full and summary ids, read_attachment logs the id as summary | `pkg/mcpapp/tools.go:922-924`, `:958`, `:1164`, `:1213` |
| 29 | MCP `view` logs nothing | `pkg/mcpapp/tools.go:973-1013` |
| 30 | MCP search default 8 hits | `pkg/mcpapp/tools.go:24-27` |
| 31 | MCP show: omitted `up` means 2, omitted `down` means 1 | `pkg/mcpapp/tools.go:319-325`; `internal/query/show.go:23-26`; re-exported `pkg/application/read_api.go:10-13` |
| 32 | `ShowResult` carries `FullIDs`, `SummaryIDs`, no depth | `pkg/application/read_api.go:79-84` |
| 33 | `SearchResult.EntryIDs` | `pkg/application/read_api.go:105-114` |
| 34 | MCP tool registrations for search, view, show, read_attachment | `pkg/mcpapp/tools.go:493-519` |
| 35 | `SearchQuery`: `Terms` are text mode, `Phrase` is vector mode; superseded excluded by default; default limit 10 | `internal/query/search.go:39-79`, `:53-57`, `:25` |
| 36 | `SearchQuery.Mode()` picks text, vector or hybrid from the fields set | `internal/query/search.go:82-93` |
| 37 | Vector and hybrid search refuse without an embedder | `internal/finders/search.go:78-81`, `:83-103` |
| 38 | Hybrid search fuses with reciprocal rank fusion (k = 60) | `internal/finders/search.go:227`, `:732` |
| 39 | `Application.Search` turns a phrase holding a credential into a literal term, so a vector request can run as text | `pkg/application/application.go:370-377` |
| 40 | `showFromSnapshot` derives full and summary ids; `viewFromSnapshot` runs layouts | `pkg/application/application.go:313-344`, `:236-289` |
| 41 | `topic(L)` filter matches by prefix over effective topics, so a label listing includes its sub-labels | `internal/finders/topic_filter.go:19-35` |
| 42 | `Graph.EntriesAfter(id)` in stable id order | `internal/model/entry_order.go:9-17` |
| 43 | Full entry id shape (suffix `[a-z0-9]+`) | `internal/model/entry_selection.go:12` |
| 44 | CLI `show` and `search` take no session | `internal/cliapp/app.go:694-768`; `internal/cliapp/search.go:359` |
| 45 | CLI `sdd new` writes through `handler.NewEntry` directly; `--skip-preflight` annotates a bypass | `internal/cliapp/app.go:769`, `:1095`, `:857` |
| 46 | `pkg/llm.Purpose` is a closed set (preflight, summarize, title, writing-guide, two extract purposes) | `pkg/llm/llm.go:22-37` |
| 47 | `Runner` contract: no internal retries, bound your own calls | `pkg/llm/llm.go:125-132` |
| 48 | Decorators `Bounded`, `RateLimited`, `Observed`, `ByPurpose` | `pkg/llm/decorate.go:15`, `:26`, `:43`, `:72` |
| 49 | `CallStat` and `StatsSink` | `pkg/llm/stats.go:12-36` |
| 50 | `embed.Embedder` port with `Fingerprint()` | `pkg/llm/embed/embed.go:44-47` |
| 51 | Local embed factory | `internal/llm/embed/embed.go:35-60` |
| 52 | Chat factory `New` composes generation only; `buildProvider` switch | `internal/llm/factory/factory.go:34-66`, `:166-175` |
| 53 | No classifier port or classify code exists under `pkg/llm` | `pkg/llm/` holds `llm.go`, `decorate.go`, `stats.go`, `embed/` only |
| 54 | Runtime injects composed `Embedder` and `LLM` | `pkg/application/runtime.go:34-45` |
| 55 | `BaseConfig` (user and machine), `PerRepoConfig` (repository), `LLMConfig` | `internal/model/config.go:95-109`, `:125-183`, `:489-539` |
| 56 | `.sdd/config.yaml` carries no grounding keys | `.sdd/config.yaml` |
| 57 | Committed project config as the application reads it | `pkg/application/project_config.go:13-20` |
| 58 | Entry merge driver command falls back to git's text merge on any exit but 0 or 10 | `internal/git/merge_driver.go:13-24` |
| 59 | `EnsureMergeDriver` writes the driver idempotently, leaves foreign drivers alone | `internal/git/merge_driver.go:29-68` |
| 60 | `EntryMergeDriver = "sdd-entry"`; attribute line builder | `internal/model/entry_merge.go:12-14`, `:278-298` |
| 61 | `ConfigMergeDriver`; `RouteMergeAttributes` drops only sdd's own driver lines | `internal/model/graph_clone.go:27-29`, `:138-153` |
| 62 | Init writes entry routing | `internal/handlers/handler_init.go:55` |
| 63 | Binary format precedent: index segments (header, uvarint table, read-only mapping, rename-only writes); sqlite-vec rejected for CGO or a WASM runtime | `internal/index/segments.go:1-42` (rejection `:36-42`), constants `:72-83` |
| 64 | Index measurement: a search on a 15k-row store opening one file per row took 1 s warm, 16 s cold | `internal/index/segments.go:3-5` |
| 65 | Machine state root `StateRoot` (default `$XDG_STATE_HOME/sdd`); repo key | `internal/repos/config.go:32-34`, `:75`; `internal/index/store.go:41-47` |
| 66 | Session-scoped blob port `StagedBlobStore`; `Application.StageBlob` | `pkg/application/blobstore.go:26-30`; `pkg/application/write_api.go:127` |
| 67 | Conformance suites, e.g. staged blobs and embedders | `pkg/sddtest/conformance.go:380`, `:463` |
| 68 | `errgroup` already used; `golang.org/x/sync` and `github.com/gofrs/flock` already in `go.mod` | `internal/handlers/handler_new_entry.go:11`, `:133`; `go.mod:14`, `:23` |

### 4.2 Graph facts this design builds on

| Entry | What it settles |
|---|---|
| 20260705-010751-d-tac-dbk | Session read log; `refsInspected` on the write path; reads stay free; "tracking is logging, not gating"; the park affordance for seeded drafts |
| 20260703-094500-d-prc-cap | The capture procedure |
| 20260810-141555-d-cpt-20r | LLM judgment moves before playback; after confirmation only deterministic validation remains (pending: the spec still runs pre-flight after confirm; the graph-fit guide is not built) |
| 20261106-155245-d-cpt-eit | Own provider clients under `pkg/llm`, three ports: generation, embeddings, a classifier "answering typed questions with probabilities"; caller-side retry decorator; `Purpose` renamed `Operation`; generic decorators. Pending, not built |
| 20261106-162723-d-tac-31y | Named provider entries under `llm.providers` declaring protocol and base URL per capability (generate, embed, classify); per-capability selections with per-operation overrides for generate and classify; classify protocols `systemone`, `openai-decisions`; keys user or machine-local. Pending, not built. It grounds in eit; nothing else builds on it yet |
| 20261031-174216-d-tac-p83 | First judge candidate: Jev over System One, typed yes/no, choice and score questions over a JSON state, answered with probabilities; Opper claims no calibration, so calibration stays sdd's; `sdd eval` replaces the eval-tag suites |
| 20261004-134853-d-cpt-sw8 | A schema-carrying request is enforced natively or refused |
| 20260921-190614-d-cpt-t8i | Typed assertion first, snapshot behind it |
| 20260827-232430-d-cpt-voa | A delegate's child session keeps its reads out of the parent's evidence |
| 20260531-163027-s-cpt-ji9 | Open gap: semantic conflicts between entries go undetected at scale |
| 20260520-132944-s-cpt-psu | Open question: echo-chamber drift |

### 4.3 Measured on this repository (2026-11-08)

| Read | Size |
|---|---|
| `sdd view --layout "topic('portability/mcp'):active:as-list"` | 132,350 B (plan S: 144 entries, about 920 B per line) |
| `sdd show 20260705-010751-d-tac-dbk --down 2 --up 1` | 31,428 B |
| `sdd search --term grounding --limit 10` | 12,171 B |
| Entries in the graph | 1,941, since 2026-04 (about 280 a month, estimate) |

### 4.4 Dropped and corrected claims

| Plan | Claim | Finding |
|---|---|---|
| N | `sdd show 20260705-010751-d-tac-dbk --down 2 --up 1` is about 15 KB, about 4k tokens | dropped: measured 31,428 B (about 7.9k tokens, estimate at 4 B per token) |
| N | Cohere rerank adapter `MaxBatch = 100` | dropped: Cohere's API reference recommends at most 1,000 documents per request; 100 has no source |
| N | Cohere rerank costs about $2 per 1,000 searches | dropped: unverified (plan N marked it so) |
| N | Cohere chat plugs in through the `openai-chat` protocol on Cohere's compatibility URL "with no new code" | dropped as fact: whether that endpoint enforces a response schema natively (required by sw8) is unverified |
| S | `sdd search --limit 10`: 9.5 KB with citations | dropped: query not stated, not reproducible; one measured instance in 4.3 |
| N | MCP default search hits at `tools.go:28` | corrected: `:27` (comment `:24-26`) |
| N | `IncludeSuperseded` at `query/search.go:54-57` | corrected: field `:57`, comment from `:53` |
| N | `EnsureMergeDriver` at `merge_driver.go:29-66` | corrected: `:29-68` |
| S | `LogRead` at `session.go:329-366` | corrected: `LogRead` `:329-348`, `foldReads` `:350-366` |
| S | `VarDecl` at `spec.go:55-78` | corrected: `:58-78` |
| N, S | `runtime.go:33-46`, `factory.go:34-65`, `config.go:94-108` and similar | corrected by one or two lines in table 4.1 |

## 5. Design points

The design as decided. Each point names the section 0 rows it rests on; the rows carry the reasons, the rejected alternatives and the accepted costs. Verification of a draft (light and deep) has its own design: `verifier/verifier-design.md`.

### 5.1 Reading window and required reads

1. Only reads made inside the capture instance count (D28). Reads made before the capture starts credit nothing.
2. Required reads for a draft (D24, D25):
   1. Graph below `grounding.brief_all_below` live entries (default 100): `gather` serves the brief of every live entry and runs no search.
   2. Graph at or above the limit: the `llm` block's `gather-query` operation writes one semantic query and the keyword terms from the draft; `gather` runs them, plus a live listing of each draft topic.
3. `grounding.enforce: true` needs a configured and reachable embedding model. sdd checks it before any step that spends LLM tokens and fails fast with an error naming the missing or unreachable model (D24).
4. Candidates are live entries only (D12). Search uses its default (superseded excluded, `internal/query/search.go:53-57`); listings are live listings. Plain `view` listings also default to the live population, with an explicit modifier for history (today `:active` is opt-in, `internal/viewlayout/reference.go:40`).
5. Kinds `actor` and `role` skip the gate as drafts and never become candidates. Every other kind is gated and is a candidate (D38).
6. Open: whether `prc`-layer drafts are exempt from the depth rule (open question 26).

### 5.2 Depth and the credited read path

1. Grounding reads count only through `gather` (D31). Plain `show` keeps its defaults (up 2, down 1) and never counts.
2. The grounding depth (`grounding.depth.down`, `grounding.depth.up`, default 2 and 1) is candidate scope: chain members within that depth become candidates and get rated like any other candidate (D35). It does not decide what the full block prints.
3. Opening a "to judge" entry in full for credit goes through `gather` with the entry ids (D31).

### 5.3 The `gather` serve

1. Sections, named per D18: header, "relevant", "to judge", "set aside".
2. Header: the reads run (each query text, term and topic listing, verbatim), the corpus mark, the prefilter status.
3. "relevant": each prefilter-rated relevant entry's own body, once, at most 12 (a constant in code). Overflow moves to "to judge" as one line each, with the count and the `gather --ids` call that opens it (D35).
4. "to judge": one line per entry: id, kind, status, brief, topics, found-by tags, score.
5. "set aside": counts by source, with the call that lists them.
6. Nothing is truncated: no character cut, no "first N" with the rest dropped. Long lists are paged or folded with an explicit count and the call that fetches the rest (D29).
7. An entry already served in full inside the capture is not served again. sdd prints its id, its brief (never cut) and the way back (`gather` with the ids, or `resume_session`) as a pointer; the read still counts (D28, D34).
8. CLI `sdd gather` prints the same and never cuts (R14).

### 5.4 Answers and verdicts

1. The agent answers on the capture report: `verdicts: [{ids, code}]`, declared `confirmExempt`, at `assemble` and at `lateEntries` (D9).
2. Codes, no free text: `topic-only`, `other-scope`, `term-clash`, `covered`, `meta`, `other`. The code has its own byte in the ledger record, with headroom (D10, D12: `superseded` removed, its slot free).
3. Every entry served in full inside the capture ends in the draft's refs or in `verdicts` with a code; the assemble gate refuses otherwise, naming the open ids (D39).
4. An id must have been listed by a credited read of this capture.
5. CLI: `sdd dismiss --session S <id>... --because <code>` records into the same verdicts once the session-aware CLI ships (D9, D3).

### 5.5 Entries that landed after the reading began

1. At `assemble`: every entry new since the reading began joins the candidates and takes the D7 path (briefs below the prefilter threshold, prefilter above) (D13).
2. At `write`, after the confirm: step `lateEntries` (D37) lists as briefs only the entries new since the assemble check. The agent answers each; no LLM runs there, per 20260810-141555-d-cpt-20r.
3. `lateEntries` options: set aside (verdicts; the confirmation holds, since `verdicts` is `confirmExempt`), revise (back to `assemble`, playback reopens), abort.
4. The corpus mark is an id snapshot staged at the first credited read, plus count and set hash for the fast path; it catches older-id entries that arrive by merge.
5. Own captures of the session are excluded.

### 5.6 Widen report

The engine seeds `widenReport` from the reading, so `hasWidenReport` holds without agent prose (D32). The seed lists every read verbatim with the ids each surfaced, then the entries read and every verdict with its code. The agent may add prose below. Text inherited from a dispatching move stays above the seed. A role sharing no context with the drafter learns from it what was searched; it gets no read credit (D28), but stored verdicts return already-judged pairs as decided.

### 5.7 Verdict model and store

1. Key: unordered pair of entry ids. Pairs with a draft live in session events until publish, then re-key to the new id.
2. Reader record: lattice code `dismissed < opened < referenced`, reason code byte (D10), audit bit, relation bits for strains. Join: max code, OR of bits; commutative, associative and idempotent, so merges converge.
3. Filter record: judge index, direction, score byte. Join: max score per judge and direction.
4. Lookup: a reader record decides; else the current judge's score against current thresholds. A one-direction score answers the reverse pair only when relevant.
5. Bodies are immutable, so no expiry. A judge change ignores old scores and re-scores the stored labelled pairs (D36).
6. Store: directories of immutable segment files `<time>-<random>.seg` under `.sdd/relevance/` (D17, D18), the same codec locally in the machine state root and in the committed copy. Tiered compaction merges small segments; names never collide, so no merge driver exists (D17).
7. Committed content: `relevance.content: reader | all`, default `all` (D20). Committing is behind its own setting, off by default (D14).
8. The CLI exports every binary file to JSON and imports it back (D16).

### 5.8 Prefilter

1. Runs only when the pairs to judge reach `grounding.prefilter_from` (default 30); below it the agent judges the briefs (D7, D27). An explicit switch turns it off even with its LLM configured (D7).
2. Config: an `llm_prefilter` block, the shape of `llm`, fully independent of it (D8).
3. Port: `pkg/llm/relevance`, `Scorer` with `Score(ctx, Request{Subject, Candidates}) (Result{Scores, Identity, Usage}, error)`, `Direction()`, `MaxBatch()`; decorators `Bounded`, `RateLimited`, `Observed`, `PerItem` (D23).
4. Protocols: `systemone` (Jev, first), `generate` (bridge over any chat provider, first), `cohere-rerank` (second) (D22, D23).
5. Input: whole entries (kind, topics, title, body), never cut. Each call fills up to the provider's limit; an entry too long for one call alone goes uncut to "to judge" (D30).
6. Set-aside rule: the prefilter sets an entry aside only once the Wilson lower bound (95%) of its recall reaches 0.98; until then it only sorts and the agent judges every candidate (D33).
7. Calibration labels: negatives only from `topic-only`, `other-scope`, `term-clash`; `covered` excluded; `meta` kinds never reach the prefilter; `other` counted only (D11, D38).
8. Audit: a constant 3% of set-asides shown blind as "to judge" lines; drift above 2% over the latest 200 audit labels (minimum 50) steps the threshold down at most once a day (D36).
9. Parallelism: a semaphore of 16 calls; pass deadline 45 s; constants in code. Pairs unanswered at the deadline go to "to judge", unstored, and the header names the count. Open: whether to accept up to 45 s per `gather` or judge in the background between reads (open question 29).
10. Failure: provider down, deadline, unreadable answer: every affected candidate goes to "to judge", nothing is stored, and `prefilter.status` (`off`, `ok`, `partial`, `failed`) plus the header name it. No prefilter call runs after the confirm.
11. Cosine stage: a later slice, once calibration has labels; cosine set-asides are recomputable and never stored.

### 5.9 Strains, recheck and healing

1. A strain is a clash between two existing entries (contradicts, duplicates, outdates), reported by the agent on the capture (`strains`) or by a verifier (verifier axis `strain`).
2. Strains and hand edits of committed entries (found from the git log) become recheck items: events of type `sdd.recheck` in the project event store, subject the entry ids, kept until healed (D5, D14).
3. The event store of 20261107-115150-d-cpt-0mu is built in this work on its own `pr/` branch: interface, envelope (type, time, writer, subject, payload), reserved `sdd.` namespace, append and query by type, subject and time; local default a binary append log sharing the ledger's codec (D15). Layout per channel `.sdd/events/<channel>/` with immutable segments (D17). Sharing per channel: `local` by default, `git` by `events.channels.<channel>.share: git` (D19).
4. `sdd catchup` shows unresolved items and a healing suggestion to the participants config names as responsible for recheck runs (D5). A healing run proposes changes that become new entries through the normal capture; healed items leave the list.
5. A capture whose draft refs A or B while the A-B item stays unhealed is refused; a draft citing neither continues. On refusal the agent decides with the owner where needed: heal now, redraft, hold the draft, drop the topic (D26).
6. Two people healing one item is avoided by coordination; network or hosted coordination is a later feature (D6).

### 5.10 CLI under enforcement

1. First slice: `sdd new` refuses under enforcement and names the engine capture (D3).
2. Recorded bypass: `--skip-grounding` on `sdd new` and the same option on the MCP capture; the entry carries `grounding: skipped` and `confidence: low`; every reader treats it as low confidence (D4).
3. Follow-up slice: a session-aware CLI, `--session` on `gather`, `search`, `show`, `view`, `new`, and `sdd dismiss` (D3, D9).

### 5.11 No-repeat rule for every entry-returning verb

Every sdd verb that returns anything about an entry (metadata, title, summary, body) serves it once per session; a repeat shows as the pointer of 5.3 item 7. The cut happens only in MCP mode; the CLI never cuts, in session mode too (R14). The audit of all such verbs is its own slice (7.5).

### 5.12 Verification and the graph-fit guide

The capture's reading (relevant entries, "to judge" set, refs, verdicts, strains) is the neighbourhood the graph-fit guide of 20260810-141555-d-cpt-20r judges the draft against (D40). Light verification is the capture's pre-flight run over that reading; deep verification adds a strong-model exchange with tool calls (verifier design V-D21). `verifier.mode` other than `off` requires `grounding.enforce` (V-D7).

### 5.13 Configuration surface

| Key | Default | Layer | Row |
|---|---|---|---|
| `grounding.enforce` | `false` | global and committed set it, committed wins; local may only set `true` | D1, D2 |
| `grounding.depth.down`, `grounding.depth.up` | 2, 1 | committed | D31, D35 |
| `grounding.brief_all_below` | 100 | committed | D25 |
| `grounding.prefilter_from` | 30 | committed | D27 |
| prefilter switch | off | any layer | D7 |
| `llm_prefilter` | absent | global or local (keys are secret) | D8 |
| `llm` operation `gather-query` | the `llm` default | global or local | D25 |
| `relevance.content` | `all` | committed | D20 |
| ledger commit setting | off | committed | D14 |
| `events.channels.<channel>.share` | `local` | committed | D19 |
| responsible participants for recheck runs | none | committed | D5 |

Verifier keys (`verifier.mode`, `verifier.launch`, `verifier.driver`, `verifier.skip`, `llm.context_tokens`): verifier design section 6.

Open: the key names of the prefilter switch, the ledger commit setting and the responsible participants.

Constants in code: 16 calls in flight; pass deadline 45 s; full block cap 12; audit share 0.03; recall floor 0.98; drift window 200 (minimum 50).

### 5.14 Token savings

Inputs: listing line about 920 B (measured, 4.3); one-line brief about 230 B (estimate); one entry with its chain at down 2, up 1 is 8 to 31 KB (measured); entries median 2.9 KB, p90 7 KB (measured, D35).

Estimate: `gather` alone saves 10 to 45% of grounding reads per capture depending on topic size; with the prefilter, once its recall floor holds (D33), 65 to 70%. The `gather` slice and the prefilter slice measure both (7.5).

## 6. Tools weighed (prior art)

No library is adopted before the owner confirms.

### 6.1 Search 1: compact binary set of symmetric pair verdicts, mergeable

Question: a Go library for a compact, binary, on-disk set of symmetric pairwise verdicts between up to 100k entries, small, fast membership, mergeable by a git merge driver (union).

| Candidate | License | Dependents | Last release | Releases in 12 months | Scorecard | Open advisories |
|---|---|---|---|---|---|---|
| Standard library (`encoding/binary`, `slices`, `bytes`) | BSD-3-Clause | unverifiable (no count on the registry page) | go1.27.3 (go1.27.0 on 2026-08-19) | 2 majors, 13 minors; exact count by date unverified | 6.2 (2026-11-02) | 0 open (stdlib advisories filed under "stdlib"; an old `encoding/binary` advisory is fixed) |
| RoaringBitmap/roaring (roaring64) | Apache-2.0 | 67 packages import `v2/roaring64`; 2,954 stars | v2.30.0, 2026-10-19 | 23 | 5.3 (2026-11-02; Code-Review 2, Token-Permissions 0, Pinned-Dependencies 0) | 0 |
| bits-and-blooms/bitset | BSD-3-Clause | 440 packages; 1,514 stars | v1.29.0, 2026-10-31 | 8 | 7.2 (2026-11-03; Fuzzing 10) | 0 |
| bits-and-blooms/bloom (dropped) | BSD-2-Clause | 263 packages; 2,835 stars | v3.7.1, 2025-10-26 | 0 | 3.9 (Maintained 1) | 0 |

Ranking and trade:

| Rank | Candidate | Reason | Trade |
|---|---|---|---|
| 1 | Standard library | sorted pair keys, delta-varint (about 1 to 3 B per pair), binary search for membership, linear sorted merge for the union; no new dependency | own the format, its versioning, the merge and fuzz tests (about 150 to 250 lines); O(log n) membership; a conflict rule needed (any option needs one) |
| 2 | roaring64 | compressed `uint64` set, portable serialization, fast `Contains`, `Or` union | fast release cadence, weakest Scorecard of the viable options; on sparse keys no smaller than delta-varint; deterministic bytes only if run-optimize is applied the same way on every write |
| 3 | bitset | strongest security profile | dense: a full 100k by 100k triangle is about 625 MB; unfit as the ledger; fits only as an in-memory scratch set |
| dropped | bloom | probabilistic false positives would report unjudged pairs as judged; cannot list members; stalled | n/a |

Every option needs the id-to-index mapping and the conflict rule, so roaring64 would save only the encoding and union code.

Proposed verdict (open for QA): standard library. Reason: no dependency, smallest-or-equal file, canonical bytes under sdd's control, and the index-segment format is a working precedent (fact 63).

### 6.2 Search 2: one adapter interface over hosted rerank and pairwise relevance providers

Question: a Go library giving one adapter interface over hosted rerank providers (Cohere, Jina, Voyage, others) so the core scores query-document pairs with no provider-specific code.

| Candidate | License | Dependents | Last release | Releases in 12 months | Scorecard | Open advisories |
|---|---|---|---|---|---|---|
| Standard library (`net/http`) | BSD-3-Clause | 1,776,917 importers | go1.27.3, 2026-10-19 | 14 | 6.2 (2026-11-02) | 0 open against go1.27.3 |
| cohere-go v2 | MIT | 52 packages; 70 stars | v2.20.1, 2026-11-04 | 8 | 4.9 (2026-11-02; Code-Review 5) | 0 |
| chroma-go `pkg/rerankings` | MIT | unverifiable | v0.4.1, 2026-04-08 (latest) | 12 (8 stable), none after 2026-04-08 | 4 (Maintained 3, Code-Review 1) | 0 |
| redis-vl-golang `extensions/rerank` (dropped) | MIT | 1 importer, same repository; 7 stars | v0.2.0, 2026-07-14 | 4 | unverifiable (no score published) | 0 |

Ranking and trade:

| Rank | Candidate | Reason | Trade |
|---|---|---|---|
| 1 | Standard library | strongest record; no dependency; a thin port fits beside the existing chat and embedding ports | no rerank abstraction; each adapter (request, response, retry, rate limit) is sdd's |
| 2 | cohere-go v2 | official Cohere SDK, maintained | Cohere only; no parallel batching; an official SDK conflicts with the dependency-hygiene rule eit grounds in (20260601-153050-d-cpt-n3y) |
| 3 | chroma-go `pkg/rerankings` | the only multi-provider rerank adapter | stale since 2026-04-08; pulls the whole Chroma client module for one package; dependents unverifiable |
| dropped | redis-vl-golang | no external dependents; ties rerank to a Redis library | n/a |

Proposed verdict (open for QA): standard library `net/http`, one adapter package per protocol, tested with `httptest`. Reason: matches eit's "own clients over HTTP" decision; the lasting cost is each provider's API shape, which no library removes.

### 6.3 Not searched, already present

| Need | Already in `go.mod` | Use |
|---|---|---|
| Bounded parallel calls | `golang.org/x/sync` (`errgroup`) | prefilter fan-out |
| File lock | `github.com/gofrs/flock` | local store compaction |
| CRC | standard library `hash/crc32` | shard checksum |

## 7. Consolidated plan

### 7.1 End-to-end flow, agent's view

1. Start the capture (`start_procedure capture`). Reads made before it credit nothing (D28).
2. Call `gather` with the running capture. sdd writes the queries from the draft, or serves every live brief on a small graph (D24, D25).
3. Read the serve: "relevant" bodies in full, "to judge" lines, "set aside" counts, pointers for entries already served (5.3).
4. Open the "to judge" entries that look relevant through `gather` with their ids (D31).
5. Report the draft at `assemble` with refs, `verdicts: [{ids, code}]` for every other entry served in full or listed, and `strains` for any clash seen between existing entries (D9, D10, D39).
6. A refusal names the owed reads or ids and the one call that clears them.
7. Writing guide, playback, confirm, then pre-flight; in light or deep mode the verifier runs as the verifier design states.
8. At `write`: `lateEntries` lists entries new since the assemble check; answer each (5.5).
9. Publish, then `verifySummary`. Reported strains become recheck items (5.9).

### 7.2 End-to-end flow, engine's view

1. Read tools log `EventRead` with a grounding payload inside the capture instance: reads as run, listed ids, full ids.
2. The first credited read stages the corpus mark (id snapshot blob, count, set hash).
3. `gather` (application layer) writes or takes the queries, runs the semantic search, the keyword search and the topic listings on one snapshot, looks up stored verdicts, sends unjudged pairs to the prefilter when it runs, renders the sections, and logs every read.
4. On each `assemble` report, predicates fold the instance's events into `model.GroundingEvidence` and evaluate pure functions in `internal/model`: reads owed, candidates answered, entries current, strains inspected, full reads ended as ref or coded dismissal. All pass vacuously when `grounding.enforce` is off.
5. At `write`: the late-entries check on a fresh snapshot (fast path: count and hash); no LLM call.
6. Before the entry is written: the same check once more, closing the race.
7. At publish: draft-keyed records re-key to the new id; reader and filter records go to the verdict store; with commit on, the new segment rides the capture's own commit.
8. At completion: labels counted; thresholds recomputed when enough new labels arrived.

### 7.3 Surfaces

MCP:

| Tool | Change |
|---|---|
| `gather` | new read (5.3); the only credited read path |
| `search`, `show`, `view` | log the reads; no credit; follow the no-repeat rule (5.11) |
| capture procedure | `verdicts`, `strains`, step `lateEntries`, seeded `widenReport`, `--skip-grounding` option |

CLI:

| Command | Use |
|---|---|
| `sdd gather` | prints a serve; never cuts; no credit until the session-aware CLI |
| `sdd grounding status` | judge, labels, recall bound, thresholds, audit share, drift steps, precision per code (D11) |
| `sdd new` under enforcement | refused, naming the engine capture (D3); `--skip-grounding` records a bypass (D4) |
| export and import of the binary files to and from JSON | D16 |
| `--session` on `gather`, `search`, `show`, `view`, `new`; `sdd dismiss` | session-aware CLI slice (D3, D9) |

### 7.4 Components and where each lives

| Component | Package (new unless it exists) | Layer |
|---|---|---|
| Grounding policy, evidence fold, owed reads, unanswered candidates, late entries, pure | `internal/model/` | model |
| Pair, reader code, reason code, relation bits, filter score, join, lookup, pure | `internal/model/` | model |
| Calibration (Wilson bound, drift step), pure | `internal/model/` | model |
| Segment codec, compaction | `internal/relevance/` (new) | library |
| Read payload, fold and replay; types `verdict`, `strain`; `confirmExempt` | `internal/engine/session.go`, `types.go`, `spec.go`, `store.go` | engine |
| Gather query, finder, presenter | `internal/query/`, `internal/finders/`, `internal/presenters/` | CQRS |
| Query writer and generate bridge prompts | `internal/llmops/` | llmops |
| Relevance port and decorators; Jev and Cohere clients | `pkg/llm/relevance/` (new) | public port |
| Prefilter factory | `internal/llm/factory/` | composition |
| Verdict store port; gather; grounding predicates; publish re-key | `pkg/application/` (edits in `workflow_registry.go`, `workflow_capture.go`) | application |
| File and in-memory verdict stores | `pkg/local/` | adapter |
| Conformance suites for the verdict store and the scorer | `pkg/sddtest/` | tests |
| Event store port, binary append log, channels | `pkg/application/`, `pkg/local/` | application, adapter |
| MCP `gather` and read logging | `pkg/mcpapp/tools.go` | shell |
| CLI commands | `internal/cliapp/` | shell |
| Config keys | `internal/model/config.go` | model |
| Capture, catch-up and groom specs | `internal/baseprocedures/entries/` (`20260703-094500-d-prc-cap.md`, `20260703-194500-d-prc-cat.md`, `20260706-171500-d-prc-grm.md`) | procedure |
| Skill text naming `gather` and the owed reads | `internal/bundledskills/templates/` | skills |

### 7.5 Slices in order, with acceptance criteria

Each slice ships alone. Tests use external test packages at the owning layer.

1. Read log facts. Read payload inside the capture instance; fold and replay; `search`, `show`, `view` log their reads; no gate.
   - AC: replay restores every logged read; reads outside a capture instance credit nothing (D28); an empty payload appends nothing.
2. `gather` without prefilter. Query writer (`gather-query`), all-briefs mode below `grounding.brief_all_below`, embedder fail-fast, sections, pointers, no truncation, live candidates, live default for `view` listings, corpus mark, MCP and CLI.
   - AC: a graph below the limit gets every live brief and no search (D25); a missing or unreachable embedder fails before any LLM call (D24); no serve cuts text or drops list items (D29); an entry served in full earlier in the capture prints as id, brief and way back (D34); superseded entries never appear as candidates (D12); the token saving against plain reads is measured on this repository.
3. No-repeat audit of every entry-returning verb (R14). Every verb returning entry metadata, title, summary or body serves an entry once per session in MCP mode; the CLI never cuts.
   - AC: a table of every such verb with its no-repeat test; an MCP repeat prints the pointer; the same CLI call prints in full, with and without `--session`.
4. Capture gate and late entries (`grounding.enforce`). Layered enforcement (D2); `verdicts` with codes and `confirmExempt`; full reads end as ref or coded dismissal; `lateEntries`; race re-check; seeded `widenReport`; `sdd new` refusal; `--skip-grounding`; `actor` and `role` exempt.
   - AC: each refusal text names the one call that clears it; a local `false` is refused naming the committed file (D2); an entry landing between confirm and write reaches `lateEntries` and returns to `write` without a new confirm (D13); an older-id entry arriving by merge is caught; a bypassed entry carries `grounding: skipped` and `confidence: low` (D4); enforcement off passes unchanged.
5. Verdict store and reuse. Model join and lookup; segment codec; local store; publish re-key.
   - AC: join is commutative, associative and idempotent; the reason code survives a round trip; a stored verdict answers a repeat pair without a new judgment; conformance passes on file and in-memory stores.
6. Prefilter. `pkg/llm/relevance` port and decorators; generate bridge; Jev over `systemone` through `llm_prefilter`; takeover threshold; switch; whole-entry input; failure reporting.
   - AC: below `grounding.prefilter_from` no prefilter call runs (D27); no input is cut (D30); until the recall bound holds the prefilter sets nothing aside (D33); a provider failure lists every affected candidate under "to judge" and stores nothing; wire tests cover request shape, score mapping, 429 and malformed bodies.
7. Calibration and audit. Labels by code; Wilson bound; blind 3% audit; drift step; re-score on judge change; `sdd grounding status`.
   - AC: labels follow D11; drift steps down at most once a day and never up; a judge change computes its bound from stored labelled pairs (D36); status prints precision per code.
8. Committed ledger. Commit setting; `relevance.content`; segment per commit; compaction; JSON export and import.
   - AC: two branches each adding segments merge with no conflict and no merge driver (D17); compaction drops duplicates; `reader` and `all` sizes are measured on a synthetic 10k-entry ledger; export and import round-trip byte for byte (D16).
9. Event store, on its own `pr/` branch. Port, envelope, `sdd.` namespace, binary append log, channels, sharing modes, JSON export.
   - AC: append and query by type, subject and time; `local` channels never reach a commit; a `git` channel's segments ride the next sdd auto-commit and never commit on their own (D19).
10. Recheck and healing. `strains`; `sdd.recheck` events; git-log detection of hand edits; catch-up section for responsible participants; healing run; the D26 refusal.
    - AC: a draft citing A while the A-B item is unhealed is refused, a draft citing neither passes (D26); a healed item leaves the list; catch-up shows the items only to the responsible participants (D5).
11. Cohere rerank adapter (`cohere-rerank`), second after Jev (D22).
    - AC: wire tests; its one-direction verdict answers only the draft-to-candidate direction.
12. Cosine stage, after slice 7 has labels.
    - AC: a calibrated floor; cosine set-asides never stored.
13. Session-aware CLI (D3 follow-up). `--session` on the read commands and `new`; `sdd dismiss`.
    - AC: CLI reads log into the session and credit its capture; `sdd new --session` passes the same gate as the engine capture.
14. Verification, in the order of verifier design V-D14: light (pre-flight over the reading, after slices 2 and 4), then the voa and zpe refinements, then deep. Acceptance criteria: verifier design section 7.

Dependencies: 1 before 2 and 4; 3 after 2; 5 after 4; 6 after 2 and 5; 7 after 6; 8 after 5; 9 before 10; 10 after 4 and 9; 11 after 6; 12 after 7; 13 after 4; 14 after 2 and 4.

## 8. Open questions for the owner

1. Where enforcement policy lives. Decided: D2.
2. `sdd new` under enforcement. Decided: D3, D4.
3. Enforcement default upstream. Decided: D1.
4. Search relevance. Decided: D24.
5. Prefilter port. Decided: D23.
6. Jev over System One. Decided: D22 (TypeSafe's Jev via Opper; thresholds come from calibration, per the probe findings).
7. First adapter. Decided: D22.
8. Committed ledger content. Decided: D20.
9. Reported contradiction blocking. Decided: D26.
10. Show default depth. Decided: D31.
11. `widenReport` under enforcement. Decided: D32.
12. Defaults. Decided: D33, D35, D36.
13. Vocabulary for upstream. Decided: D37.
14. Kinds exempt from the gate. Decided: D38.
15. No embedder. Decided: D24 (enforcement fails fast without a configured, reachable embedding model).
16. Drift step keeps the user's confirmation when only answers change. Decided: D9 (`verdicts` is `confirmExempt`).
17. Heal offer. Decided: D5, D26 (a conflict the draft cites blocks and opens the choice, heal now among them; any other reported conflict is filed to the recheck list only).
18. Entries read in full but not referenced. Decided: D39.
19. Reader reasons. Decided: D10 (fixed codes in the ledger, no free text).
20. Relation to the graph-fit guide (20r). Decided: D40.
21. Answer surface. Decided: D9.
22. Drift scope. Decided: D13.
23. Merge driver failure. Moot: D17 needs no merge driver.
24. Prefilter switch. Decided: D7 (explicit switch, works even when the LLM is configured).
25. Rerank placement and protocol names. Decided: D8, D23.
26. `prc`-layer entries: exempt from the depth rule (as in the reference gate), or not (proposed).
27. Query default. Decided: D24 (an LLM writes the queries from the draft).
28. Ledger layout. Decided: D17.
29. `gather` latency: accept up to 45 s per call (proposed), or judge in the background between reads.
30. Reading window. Decided: D28.
31. Event store dependency. Decided: D15.
32. Sharing of recheck events. Decided: D19.
33. Default of `relevance.content`. Decided: D20 (`all`).
34. All-briefs size limit and query writer. Decided: D25.
35. Prefilter takeover threshold. Decided: D27.
36. Served-entry pointers across an agent context compaction. Decided: D34 (spike: compaction-spike/report.md).
37. Prefilter input length. Decided: D30. Context: D29 covers agent-facing output only. Both plans cut entry text sent to the prefilter (6000 characters per entry; the reference cuts the rerank query at 8000); Jev allows 32k tokens for state plus question. Keep a cut there, or send whole entries and split batches by size.
38. The verifier procedure (D40): light and pro variants by configuration; structured findings (severity, pointer into the draft, verdict in words); the drafter accepts or contests each with a reason; the verifier writes a proposal as a copy of the draft with changes applied and maps each finding to its diff hunks, explains or drops contested findings, applies accepted contests; the drafter accepts the proposal or disagrees per hunk; up to 3 rounds, then open points go to the owner. Settled in its own QA round. The verifier design lives in its own file, verifier/verifier-design.md, attached to its own graph entry for the graph-fit and verifier procedure.

## 9. Rejected alternatives

| Rejected | Reason | From |
|---|---|---|
| Agent hooks or a harness plugin | harness-specific; upstream refuses it; other harnesses stay ungated | brief, N, S |
| Gating reads or questions | contradicts d-tac-dbk; reads are what grounding needs | N, S |
| Per-subject ledger keyed by entry | exists to work around shared session ids; sdd sessions are explicit handles and cross seeded dispatch | S |
| Per-session credit as in the reference gate | credit for reads of any subject; voided by any other capture | N |
| SQLite verdict store | CGO or a WASM runtime, the reason sqlite-vec was rejected (fact 63) | N, S |
| Text ledger with `merge=union` | about 60 B per pair (about 66 MB at 10k entries, estimate); duplicate lines on conflicting verdicts | N |
| Storing only bearing verdicts | every gather would re-pay the non-bearing majority (98.2% in the reference implementation) | N |
| Hashed 64-bit ids in the ledger | irreversible; 7 B packed ids are reversible at similar cost | N |
| One ledger file | whole file churns on every commit | N |
| Segment-per-capture files with compaction | file count and compaction machinery grow; two compactions collide | S |
| Reasons in the ledger | size and privacy | S |
| Bloom filter for the ledger | false positives report unjudged pairs as judged; cannot list members | prior art |
| Batching pairs into one chat call | recall unmeasured for that form | reference, N, S |
| Filter set-asides counting at the door | a new entry is exactly the case a stale model misses | N, reference |
| Classifier or LLM call after confirm | 20r keeps the post-confirm path mechanical | S |
| Trained logistic score as stage 1 | feature pipeline and training code to own; cosine floor plus calibrated thresholds cover the cut | N, S |
| Storing cosine set-asides | recomputable; would dominate the ledger | S |
| Requiring an open in full for every listed result | token cost with no added judgement | S |
| Reranker as first stage | calls grow with candidates where a free stage costs none | reference |
| Bounding candidates per reading | drops bearing entries in large topics | reference |
| Owing only the head of each read | 12% of later-cited results sat at ranks 9 and 10 | reference |
| Reading three levels down by default | 89% more text for a level opened 0.7% of the time | reference |
| Hiding done and closed entries | 11% opened later, 5% named later; counted and listable instead | reference |
| Keeping three reads with dedupe instead of `gather` | saves less, still three calls | reference |
| Counting a refused question as bearing instead of falling back | sends the whole throttled share to the reader | reference |
| Official vendor SDKs as adapters | dependency hygiene (20260601-153050-d-cpt-n3y, eit) | eit, prior art |
| Keep-ours merge fallback | silently drops theirs; violates fail loud | this design (5.7: D17 needs no merge driver) |
| Text-merge fallback for the binary ledger | meaningless on binary | this design (5.7: D17 needs no merge driver) |
