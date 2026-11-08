# Reference implementation A: a draft verifier run by an orchestrator

A project that runs sdd through an orchestrator of agent jobs built a second reader for every graph draft. This report states how that verifier works and what it measured, as far as it bears on sdd. Command names are generalised: `<wrapper>` is the orchestrator's command that wraps `sdd`, "the orchestrator" is its job scheduler.

## Summary

1. Every graph draft except a `done` reaches the graph only after a second reader, the verifier job, judged its exact registered bytes. Capture refuses otherwise.
2. The verifier is a separate Claude Code process (Opus, high effort; medium for delta readings) that the orchestrator starts in a tmux window and keeps open between rounds, waking it through a `Stop` hook.
3. The protocol: provisional findings, then the drafter's `accepted` or `contested` answers, then the verdict that stands, with a proposal copy whose `diff -U0` hunks map onto numbered findings. A proposal that leaves no finding open is adopted and captured with no further reading.
4. After three non-clean verdicts, registration refuses a fourth version without a `--past-cap '<why>'` reason. No light or small-model verifier exists. Small models do only the pre-flight and the candidate filter, and the verifier never uses the filter.
5. Measured: 93 of 1971 first readings came back clean. Drafters accepted 1445 of 1458 answered findings. 783 of 1718 landed drafts landed as an adopted proposal. A verifier session costs a median of 1.08 USD (Claude Code's own estimate) and 716 s wall time.

## Data windows

| Source | What it holds | Window |
|---|---|---|
| Orchestrator database, subject ledgers | 2205 subject ledgers, 2139 draft records, 3884 recorded readings | about 6 weeks |
| Orchestrator database, job starts | 2752 verifier job starts | about 5 weeks |
| Verifier session transcripts | 508 transcripts (Claude Code's own records, with cost state) | 6 days |
| Orchestrator log | log lines | about 6 weeks |

## 1. Who starts the verifier, how, on which model, with which tools

### 1.1 Start path

1. A drafting job registers its draft with `<wrapper> graph draft`, then asks for a reading with `<wrapper> verify <entry> --draft <path>`.
2. The request becomes a document in state `waiting`. A caller that is no orchestrator job is refused. The drafter's window stays open, awaiting.
3. A dispatch pass batches waiting requests, checks the orchestrator-wide start refusals, claims a roster name `verify-<id3>-<round>` and starts the job.
4. The job is a separate OS process: `claude --session-id ... --system-prompt-file <built definition> --tools ... --effort ... --mcp-config ... --strict-mcp-config --settings ...`. It runs in its own tmux window, under macOS `sandbox-exec`. It is not a subagent and not a direct API call.
5. A warm standby process may take the start instead of a cold one.
6. A second path exists outside the orchestrator: any agent may run a reader of its own, provided that reader holds a roster name for the subject. Only an orchestrator job is held to the open request. A job may never claim or release a roster name itself.
7. The same definition also audits `done` entries after capture, as a separate job kind.

### 1.2 Model and effort

| Setting | Value |
|---|---|
| Model | `opus` |
| Effort, full and refs readings | `high` |
| Effort, delta readings | `medium`, chosen per start |
| Recorded starts | 2752 of 2752 on `opus`; effort `high` 2069, `medium` 436, unrecorded 247 |

A code comment states that every verification that let a high finding through ran on Sonnet. No record carries that measurement.

### 1.3 Tools

1. The verifier gets `Bash, Read, Edit, Write, Grep, Glob, WebSearch, WebFetch` and a documentation-lookup MCP server. `WebSearch`, `WebFetch` and MCP tools are pre-allowed; Bash, Edit and Write go through the project's allow list.
2. Writes to the orchestrator: none.
3. Graph: every read goes through the wrapper. Every search and listing must bypass the candidate filter (`--unfiltered`).
4. The verifier's definition drops instruction sections it never acts on (setup, prose lint, capture steps, the `done` kind).
5. Measured use in 508 transcripts: Bash in 508 sessions, Read 401, Edit 352 (the proposal copy), Grep 76, WebFetch 41, Write 37, WebSearch 14.

## 2. Pause and resume between findings and proposal

1. The verifier keeps its context by staying alive: its window stays open after each turn.
2. Delivery: a `Stop` hook marked `asyncRewake` runs a waiter that blocks on a per-session socket. The orchestrator writes a wake file and sends a datagram, and Claude Code takes the text as the next prompt. Nothing gets typed into the window.
3. The sequence:
3.1 The verifier records `<wrapper> graph verified ... --verdict findings --provisional --report ...`. The ledger record gets `provisional: {agent, hash, report}`.
3.2 The orchestrator relays the report to the drafter with the contest instruction.
3.3 The drafter answers with `<wrapper> contest <entry> --draft <path> --answers '<n>: accepted | <n>: contested, <evidence>'`. The request turns `answered`.
3.4 The orchestrator sends `contest: <roster name>\n[draft: <path>\n]<answers>` to the verifier as its next task.
3.5 The verifier weighs the answers, writes the proposal, and records the verdict that stands without `--provisional`. The orchestrator relays verdict, proposal path and report to the drafter.
4. A silent drafter gets one nudge after 300 s.
5. A drafter who is gone: after 3 dispatch passes the reading settles `lost`, and the report goes to the work item as a finding. Measured: 56 such settlements.
6. Between versions, a reader whose verdict stood as `findings` rests, keeping its window and roster name. The next version goes to the same reader as a delta reading. Measured: 384 hand-overs to a resting reader.
7. A reader whose turn ends with no verdict gets asked once.
8. Findings raise the drafter to a higher effort level; its window restarts on the same session (`claude --resume`) with the answer as its task.

## 3. Formats

### 3.1 Verifier task message

```
entry: <entry id>
draft: <absolute draft path>
agent: verify-<id3>-<round>
target: <branch>

<delta>
mode: full|delta|refs ...
</delta>
<entry-chain>...</entry-chain>
<ground>...</ground>
<landed>...</landed>
<draft path="<path>" hash="<first 12 hex of sha256>">...registered bytes...</draft>
```

### 3.2 Findings

1. The call: `<wrapper> graph verified <draft> --entry <E> --verdict clean|findings --agent <roster name> [--provisional] [--proposal --hunk '<n>[,<n>]=@@ -a,b +c,d @@' ...] [--read-hash <hash>] --report '<lines>'`.
2. A finding is any report line that opens with `high`, `medium` or `low`. Findings get numbers from 1 in report order. There is no structured pointer into the draft; the pointer is free text (a section, a criterion, a quoted phrase). A closing line `checked, holds: ...` is no finding.
3. The verdict is `clean` or `findings`. There is no per-finding verdict field. Each line should name its repair and whether it narrows, removes or adds.
4. Severity in the reports: 1633 high, 7219 medium, 3651 low lines over 2883 findings readings. 220 findings readings carry no line that opens with a severity, so no hunk could map onto them.

### 3.3 Ledger record of one reading

Shape of a real record, content replaced by placeholders:

```json
{
 "agent": "verify-<id3>-4",
 "hash": "<sha256 of the read bytes>",
 "mode": "delta",
 "verdict": "findings",
 "report": "medium: 1. <passage> ... No hunk: not a wording fix. Fork for the owner ... Accepted, open.\nmedium: 2. <passage> ... Hunks: \"three\" -> \"these\" in both (narrows). Accepted.\nlow: 3. <passage> ... Hunks: both -> <new wording> (narrows). Accepted.\nchecked, holds: <what was verified>",
 "proposal": {
  "hash": "<sha256 of the proposal>",
  "path": "<scratch>/proposals/<draft>.proposal.md",
  "hunks": [
   {"hunk": "@@ -17,1 +17,1 @@", "findings": [2, 3]},
   {"hunk": "@@ -28,1 +28,1 @@", "findings": [2, 3]}
  ],
  "open": [1],
  "findings": ["medium: 1. ...", "medium: 2. ...", "low: 3. ..."]
 }
}
```

Draft record fields beside the reading history: `hash`, `verified`, `provisional`, `preflight`, `scope {scope, note}`, `self_check`, `drafter`, `tier`, `raised`, `landed`.

### 3.4 Contest answers

1. One line per finding: `<n>: accepted` or `<n>: contested, <evidence>`. The evidence is a `file:line`, an entry id or quoted command output. The answers are stored as one string; nothing parses them.
2. Shape as the verifier receives it:

```
contest: verify-<id3>-1
1: accepted
2: contested, <measured evidence: the command run and its output, the commit that landed it>
3: accepted
```

### 3.5 Request document

Fields: `entry, draft, drafter, state, at, round`, then `agent, verifier, started, batch`, `answers, relayed`, `nudged, held_back, undelivered, recorded, rested, asked`. States: `waiting, reading, provisional, answered, resting, handing, done, lost`.

## 4. How the proposal is built

1. A wrapper command copies the draft's bytes to a proposal file in scratch and prints the path. The verifier edits that copy, never the draft.
2. Hunks come from a unified diff with zero context lines between draft and proposal (the same as `diff -U0`). A hunk is named by its normalised header `@@ -a,b +c,d @@`. There is no other numbering; findings carry the numbers.
3. Mapping: one `--hunk '<n>[,<n>]=<header>'` per hunk. The record call refuses a claim naming a finding the report lacks, a header that matches no real hunk, or a real hunk that no finding claims. Findings left unclaimed become `open`.
4. Further refusals on the proposal:
4.1 It adds an entry id, a `path:line`, a criterion or a code surface that no finding's text names (widening).
4.2 It would not land as written: a stale citation, the entry shape, the prose linter, the criterion form.
4.3 It drops a required line-number stamp sentence.
4.4 Its citations have no place on the target branch.
5. The proposal bytes are kept by hash.
6. Measured: 2126 readings carried a proposal, mean 5.26 hunks (median 5). 1597 left no finding open; mean open count 0.45.
7. The drafter's options: adopt takes the proposal whole (runs the pre-flight, moves the proposal over the draft, registers it; the draft captures with no further reading); adopt is refused while a finding stays open. Otherwise the drafter edits and registers again, and the next delta runs against the proposal, so only the drafter's own change gets read. There is no partial or per-hunk adoption.

## 5. The up-to-three-rounds exchange

1. Who counts: the wrapper, at registration. It counts the readings in the draft's history whose verdict is not `clean`. Cap: 3.
2. A provisional verdict counts toward no cap, except once where the drafter answered it with a repair. A later settlement takes its place rather than counting again.
3. The request's `round` field only names the roster name; it enforces nothing.
4. Agreement: capture passes where a `clean` verdict stands on the bytes about to land (with no "next version" line), or the draft equals a proposal that leaves nothing open. The highest-impact drafts (supersedes, strategic or conceptual layer, aspiration or actor) also need the standing reading to be a full one.
5. Drafter disagreement inside a round is the contest. The verifier alone decides whether a contest holds.
6. At the bound the fourth registration is refused:

```
refused. <n> verifications sent this draft back; a draft still failing after that many rests on something no edit settles.
  split it, or put the open question to the owner
  the next round provably converges: --past-cap '<why>'
```

7. The `--past-cap` reason is checked for being non-empty and kept nowhere. Drafting roles allow it only where exactly one finding is left and its repair narrows or removes a single claim.
8. What the owner receives: no cap-specific package. The work item reaches the owner through the ordinary hold.
9. Measured non-clean readings per draft: 0: 261, 1: 1302, 2: 374, 3: 114, 4: 45, 5: 16, 6 or more: 27. So 88 drafts carry four or more. Whether each fourth registration used `--past-cap` cannot be shown, because the reason is not stored.

## 6. Light graph-only judge versus strong verifier

1. A light verifier: not built. A light or pro switch: not built. The verifier runs on every draft that is no `done`.
2. What divides the work, cheapest first:

| Step | Who | What it judges |
|---|---|---|
| Script checks at registration | wrapper | citations, unopened references, open forks, self-check note, shape |
| Pre-flight | small model (`glm-5.3-flash`) through `sdd new --dry-run` | the draft text; high findings block, medium ones block until dismissed |
| Candidate filter | small model and a trained score | which graph entries bear on a drafter's subject; the verifier never runs it |
| Verifier | `opus`, high or medium effort | the whole draft against graph and repository |
| Done audit | the same verifier, after capture | one `done` in 10 per role, more after a high defect |

3. Tiers: a `done` skips the verifier and the model pre-flight (script-checked, audited by sample). Ordinary drafts get the verifier and may share one reader per subject within 120 s. The highest-impact drafts always get a full reading.
4. Audits measured: 93 settled, 37 defects, 6 of them high.
5. Rejected: moving a delta reading to the small model, "since a missed finding lands in the graph for good". The verifier stays on `opus` by inference from review replays.

## 7. Measured results

### 7.1 Findings and verdicts

| Measure | Value |
|---|---|
| Readings recorded | 3884: 2883 `findings`, 1001 `clean` |
| Mode of those readings | full 1377, delta 483, refs 32, provisional (folded) 214, settled 143, unrecorded 1635 |
| First reading of a draft | 1878 findings, 93 clean (of 1971 drafts with a reading) |
| Verdicts by mode | full: 1229 findings / 148 clean; delta: 114 / 369; refs: 12 / 20 |
| Finding lines per findings reading | mean 4.34, median 4, max 19 |
| Landed drafts | 1718: 783 as an adopted proposal, 726 on a clean verdict, 209 neither (dones on the script path, among others) |
| Readings per landed draft | mean 1.83, median 1 |

### 7.2 Accept and contest

1. 508 transcripts: 454 contest messages carried 1445 `accepted` and 13 `contested` answers, a contest rate of 0.9%. Whether the verifier dropped each contested finding is not recorded in structured form.
2. Orchestrator log: 1502 provisional-findings pushes and 1437 contest-answer pushes.

### 7.3 Cost and time per verifier session (508 transcripts)

| Measure | Median | p90 | Sum |
|---|---|---|---|
| Notional cost, Claude Code estimate | 1.08 USD | 2.35 USD | 645.62 USD |
| API time | 161 s | 344 s | 95049 s |
| Wall time, first to last message (includes waiting on the drafter) | 716 s | 1757 s | |
| Output tokens | 15004 | 34542 | 9.1 M |
| Cache-read input tokens | 1.44 M | 3.50 M | 914 M |
| Cache-creation input tokens | 59328 | 127349 | 35.0 M |
| Model turns | 19 | | |

1. By start mode: full readings 472 sessions, median wall 760 s; delta readings 36 sessions, median 33 s.
2. The jobs run on a subscription; the USD figure is Claude Code's estimate, not a bill.

### 7.4 Numbers from the implementation's own decision record

1. Over one span, 61% of drafts took more than one reading. In rounds two and later, delta readings at medium effort raised findings in 37% of readings, full readings at high in 72%. One effort level down missed twice the medium findings on `done` drafts.
2. Review replays: `mistral-large-4` recall 4%, `glm-5.3` 28% of 25 confirmed findings. `glm-5.3-flash` at max effort matched `opus` on 6 of 10 validation decisions and 14 of 20 triage routes.
3. One draft took five fresh readings while two owner answers arrived at different times and every proposal widened the draft.

### 7.5 Failure modes on record

1. The drafter is gone before the answer: 56 readings settled `lost`; 160 "took no reply" retries; 65 "waits on the drafter" lines.
2. A verifier read the file while the drafter edited it, and the verdict got booked on another version. Fixed by inlining the registered bytes in the task and adding `--read-hash`.
3. Proposals widened drafts round after round. Fixed by the widening refusal and a scope bound on findings.
4. 220 findings verdicts carry no severity-led line, so hunks could not map and the next delta prints the raw report.
5. A work item held past the round cap was sent to triage about 60 times in one day before a fix.
6. A verifier whose turn ended without a verdict gets asked once, otherwise settles `lost`; no count kept.

## 8. Rejected or regretted

1. Regretted and reversed: rejecting reuse of one verifier across versions as "the opposite of an independent reading". The resting reader is now reused. Accepted cost: the reader's context grows with each version.
2. Rejected: refusing every draft edit while a verification runs, since the drafter edits by design while answering.
3. Rejected: putting forks to the owner first as the only answer to round growth, since proposals kept widening drafts. Accepted cost: a fork found only while reading still costs a round, and a reader cannot raise a problem outside the changed lines until the next version.
4. Rejected: an offline replay of later full readings at medium against high (declined on cost).
5. Rejected: moving a delta reading to the small model; a small model (DeepSeek V4.1 Flash) returned valid pre-flight JSON 1 time in 10.
6. Rejected: sampling the verifier on drafts rather than on records, since about eleven high defects a day would land. Accepted cost: an audit finds a defect only after it stands in the graph.
7. Rejected: batching alone as the answer to reading cost.
8. Rejected for the answer nudge: a hand-off kind, nudging on every pass, counting the grace from the pass rather than the push.
9. Rejected for proposals (planning note, not the decision record): hunks mapped in prose, one proposal file per finding, tool-applied per-finding edits, line ranges in the proposal. Also rejected: a delta reading checking the whole draft for contradictions ("delta readings let none through; full readings caught each one the next round").
10. Stated limits:
10.1 `--past-cap` reasons are kept nowhere.
10.2 Answers are free text; nothing parses them.
10.3 A finding's pointer into the draft is free text.
10.4 The loop is tied to Claude Code (`claude` CLI, `Stop` hook rewake), tmux and macOS `sandbox-exec`. No harness-agnostic interface exists.
