
# Hardened text question: in context and after compaction

Status: done, 2026-11-22. Re-run of the option-1 half of `../RESULTS.md` against branch
`next/text-question-hardening` at `1c6ffe59b` (plan `20261121-230157-d-tac-ym8`). 6 real compactions
(Sonnet s1-s3, Opus o1-o3, seeds 11-13 and 21-23 as before), 192 forks, no fork nudged, no answer missing.
Table `score.md`, every response `items.jsonl`.

## Verdict

- **0 false right after compaction.** Sonnet 0/60, Opus 0/60 (Wilson 0-6.0% each; pooled 0/120,
  0-3.1%). Every part of every post-compaction answer was `lost` (240 parts). No summary holds a
  probe's phrase or answer.
- **In context: Opus 92/93 right, 98.9% (94.2-99.8); Sonnet 89/93, 95.7% (89.5-98.3).** Previous
  design: Opus 74/81, Sonnet 72/81. Wrong answers fell from 14 to 3, all three Sonnet, all at step 1.
- **The parser-reader disagreements are gone.** No wrong answer involves an arrow, sign, entry ID,
  code span or sentence boundary. What is left is word counting at the first answer, and recall
  misses that come out as `lost`, the safe side.
- **Target:** after compaction met (0 false right). In context near the target, not at 100%: Opus
  1 miss in 93, Sonnet 4 in 93. No miss is a false right. A miss costs one full re-serve.

## Results

n = responses, two questions each, one `answer` parted by `;`. Right = `model.RoundAnswered` (both
right, either order). Wrong = any part neither right nor `lost`, so a response with one `lost` part
counts as wrong. Wilson 95% intervals.

| model | condition | n | right | wrong | lost |
|---|---|---|---|---|---|
| Sonnet | build (0-15k back) | 57 | 54/57, 94.7% (85.6-98.2) | 3 | 0 |
| Sonnet | ctx20 | 18 | 18/18, 100% (82.4-100) | 0 | 0 |
| Sonnet | ctx60 | 18 | 17/18, 94.4% (74.2-99.0) | 1 | 0 |
| Sonnet | in context, pooled | 93 | 89/93, 95.7% (89.5-98.3) | 4 | 0 |
| Sonnet | post (3 compactions) | 60 | 0/60, 0% (0-6.0) | 0 | 60/60 |
| Opus | build | 57 | 57/57, 100% (93.7-100) | 0 | 0 |
| Opus | ctx20 | 18 | 18/18, 100% (82.4-100) | 0 | 0 |
| Opus | ctx60 | 18 | 17/18, 94.4% (74.2-99.0) | 1 | 0 |
| Opus | in context, pooled | 93 | 92/93, 98.9% (94.2-99.8) | 1 | 0 |
| Opus | post (3 compactions) | 60 | 0/60, 0% (0-6.0) | 0 | 60/60 |

Parts right in context: Sonnet 181/186, Opus 185/186.

## Every miss in context (`python3 -I around.py`, `items.jsonl`)

Wrong words (3 parts, all Sonnet, all build step 1: the first answer, given in the response that
served the guide):

| run | question | expected | given | class |
|---|---|---|---|---|
| s1 | lane open: 2 words after "and leaving the" | session parks | session | count: one word of two; the second asked word is a verb ("the session parks or ends it") |
| s2 | lane intro: 2 words after "sdd writes them" | on the | on the entry | count: three words; the asked pair ends on "the" and the agent completed the noun phrase |
| s3 | lane open: 2 words after "wants to wrap" | up the | up the session | count: as s2, the pair ends on "the" |

`lost` with the text in context (3 parts; each response's other part right):

| run | question | expected | back | class |
|---|---|---|---|---|
| s2 build step 1 | lane open: 2 words after "to wrap up" | the session | same response | recall miss, safe side |
| s3 ctx60-03 | lane guideFindings: 2 words before "would act on" | yet retirement | 56k tokens | recall miss, safe side |
| o2 ctx60-03 | lane guideFindings: 2 words before "act on the" | retirement would | 55k tokens | recall miss, safe side |

- The two ctx60 misses are the same sentence of `lane guideFindings` ("…dialogue-input change, yet
  retirement would act on the whole entry."), served once, in no other text. Both models answered
  `lost` for it; the thinking is redacted, so the cause is not visible.
- No wrong words after step 1: 0 of 108 build responses at steps 2-19 (54 per model), 0 of 72 band
  responses.
- Two of the three wrong parts ask for a pair ending in a function word ("on the", "up the"). A
  window that ends on a function word is what invites completing the phrase. Opus never erred on one.

## Summaries

- 2,961 to 4,012 characters. None quotes a probe phrase or answer (0/120 parts per model). One
  8-word run of a served text appears (s3: the kind list of `lane open`), not asked.
- 4 of 6 summaries quote the step-19 answer (already asked, never asked again), as before.
- No question was pending at the compaction (the server paused after step 19), by design.

## Method

As in `../RESULTS.md` (texts, steps, server, agent, forks, compaction), with these changes:

- **Questions:** the branch's `internal/model/question.go`, verbatim (sha1 `1a7ed6e3…`), built in
  `tool/` (`qtool cands|check|round`): four kinds (between, number-after, words-after, words-before),
  phrase and asked words a single-spaced run on one line, no code span or entry ID, ask at most 90
  characters; `Answered`, `IsLost` and `RoundAnswered` score the answers.
- **Drawing** (`plan.py` `Drawer`, as `engine.Session.Ask` over `model.DrawQuestion`): kinds in random
  order, then candidates in random order, the first usable: not asked before, answer no earlier
  question's, phrase and second phrase in no other held text. The second question comes from
  another text where one yields it, never reusing the first's phrases. Agent-written lanes (`lane
  draft`, `exchange`, `answers`, `playback`) and texts under 300 characters are held but never asked;
  a phrase found in them is rejected. Difference from the engine: the first text is drawn at random
  from the window (build: 1-3 steps back; probes: a depth band), not the text held longest.
- **No tags.** Texts are served as logged. Each response ends with two `q …` lines; the guide and the
  `answer` description are the branch's `questionGuide` and `Answering` text, tool names aside.
- **Probes per conversation:** 6 forks at ctx20 (15k-25k tokens back), 6 at ctx60 (52k-68k), 20 after
  `/compact`. Conversations ended at 97.6k-98.7k tokens (2.74-2.77 characters per token).

## Limits

- Probe server, not sdd: plain-text responses, no short serves, no report schemas.
- Band samples are small (18 responses per band and model).
- Manual `/compact` at about 98k tokens; automatic compaction not tested.
- No pending question at compaction and no agent-written text asked, so the guess study's leak paths
  are excluded by the design, not measured.

## Cost

Sonnet 3.25 USD (build 0.53, compaction 0.03, 12 in-context forks 0.40, 20 post forks 0.13 per
conversation), Opus 6.54 USD (1.04-1.05, 0.05-0.06, 0.79-0.80, 0.28-0.31). **Total 9.79 USD**
(`cost.py`).


---

## Verdict

- No variant reaches zero false rights against every leak. Two leaks, both from the compaction
  summary, none from guessing:
  1. **Draft quoted in the summary.** Every right answer without the text is about `lane draft`,
     the agent's own draft, which both summaries reproduce word for word. Option 1 does not stop it
     ("likely", "s-cpt-asd so" pass the filter). Option 2 does here (0/60 both right, all models),
     only because the second question fell on a text the summary lacks; one of two was right in
     4 (Haiku), 5 (Sonnet), 2 (Opus) of 60.
  2. **Pending question quoted with its answer.** Both real summaries write the open question and
     its answer ("The answer is "not have"", "to be answered with "with a""). Haiku copies it in
     7/10, Sonnet 8/10, Opus 0/10. Neither option touches this: a summary would quote both answers.
- Without any context (bare) no model guessed right: 0/60 in all 9 cells.
- Least loss at zero (leak 1): option 2. In context, against today: Sonnet 20k -3.3 points, Opus
  20k 0, Haiku 2k -6.7, Haiku 20k +10 and 60k +43 (fewer `lost`, see the confound below). Option 1
  costs nothing (equal or better in every cell but Haiku 60k, -3.3) but does not reach zero.
- A fix for the root cause, outside both options: ask no question about agent-authored text
  (`lane draft`), and never let the answer to a pending question reach a summary (the summarizer
  reads it from the response; sdd cannot stop that, so a question pending at compaction counts as
  known).
- Setup caveat: in run 3 Opus answered the pending question from the summary ("not have"); here
  Opus 0/10. The measurement's system line ("this turn checks that question") likely makes models
  more careful than a live agent, so the rates here are lower bounds.

## Setup

- Question code: `internal/model/question.go` at `fbd656439` (unchanged since run 3's build `c9171a75b`),
  copied verbatim to `tool/model/question.go` (sha1 `7a180ffd…`); `tool/model/export.go` adds
  `Candidates` (all kinds' candidates), option 1's filter `Distinctive` and a stub `Entry`. No repo
  file changed. Draws go through the real `DrawQuestion`, with the engine's `Ask` filters
  re-applied (`internal/engine/pieces.go`): a question already asked is out, so is a phrase or
  second phrase found in another held text, and an answer equal to an earlier question's.
- Variants: **today** any drawn question; **option 1** the answer holds a word that is no function
  word (question.go's `functionWords`), has >= 5 characters and occurs once in the text (whole
  word, case aside); **option 2** two independent questions in one response (distinct keys and
  answers), right only when both are right.
- Calls: `claude -p --safe-mode --tools "" --no-session-persistence --output-format json --model
  <haiku|sonnet|opus> --system-prompt-file <f>`, prompt on stdin, from `probe/`. `--safe-mode`
  drops CLAUDE.md, plugins, hooks, MCP (a probe: 779 tokens of context, no CLAUDE.md seen).
  `CLAUDE_EFFORT` and the parent's `CLAUDE_CODE_*` variables are removed: default effort.
  Models: claude-haiku-5-5, claude-sonnet-5-5, claude-opus-5-5 (model usage in `results.jsonl`).
- System prompt: an agent preamble, `questionGuide` verbatim (`pkg/mcpapp/instructions.go:87`), and
  one line: this turn checks the question, reply only with the answer value. A first pilot without
  that line had Haiku follow the summary's "resume directly" and write the playback instead
  (`pilot1.jsonl`).
- Every prompt ends with an sdd `next` result whose `question` field is the q line (option 2: a
  `questions` list of two and "answer both, separated by ;").
- Conditions:
  - **summary** (1a): run 3's two real compaction summaries (`summary-r3o.txt`, `summary-r3s.txt`,
    from the `isCompactSummary` records of the r3o/r3s transcripts), then "[user] Looks right,
    confirm it", then the question. Questions about the texts the summarised conversation's two
    sessions held (session logs), 30 per summary per variant.
  - **bare** (1b): the question alone; texts from every gating session log.
  - **ctx** (2): run 3's Opus transcript (r3o, rendered user/assistant/tool-call/tool-result blocks as
    the agent saw them) in the system prompt (so `claude -p` caches it), the question after it. Each
    text's copies located in the transcript by its JSON-escaped head; a question counts only if every
    occurrence of its phrase lies in a copy of its text; depth = tokens from the phrase's last
    occurrence to the end of the prompt (2.66 characters per token, measured: 333,559 characters =
    ~125.4k tokens). ctx2k and ctx20k use the transcript cut at ~90k tokens (inside session 2, after
    the capture's lanes), ctx60k the whole pre-compaction transcript.
    Bands: ctx2k 0.5k-4k (drawn 3.0k-4.0k, median 3.6k: participants, remainingFields lanes);
    ctx20k 15k-25k (median 18.8k: verifySummary, open, guideFindings, playback);
    ctx60k 52k-68k (54k-60k, median 55.4k: playback, verifySummary, guideFindings).
  - **control**: the question pending at each compaction, which the summary quotes with its answer,
    plus two other asked questions whose phrase the summary holds; 5 calls each.
- Scoring: `Answered` and `IsLost` from question.go on the reply (quotes, backticks, `answer:`
  stripped; option 2 split at `;`).
- 60 items per variant, condition and model; Wilson 95% intervals.

## Results

Right = right answer (option 2: both right). n = 60 per cell (control 20). Wilson 95% intervals.

### 1. False right: the text not in context

| condition | model | today | option 1 | option 2 (both) | option 2, one of two right |
|---|---|---|---|---|---|
| summary | Haiku | 3/60, 5.0% (1.7-13.7) | 2/60, 3.3% (0.9-11.4) | 0/60, 0% (0-6.0) | 4 |
| summary | Sonnet | 3/60, 5.0% (1.7-13.7) | 2/60, 3.3% (0.9-11.4) | 0/60, 0% (0-6.0) | 5 |
| summary | Opus | 0/60, 0% (0-6.0) | 0/60, 0% (0-6.0) | 0/60, 0% (0-6.0) | 2 |
| bare | Haiku | 0/60 (0-6.0) | 0/60 | 0/60 | 0 |
| bare | Sonnet | 0/60 | 0/60 | 0/60 | 0 |
| bare | Opus | 0/60 | 0/60 | 0/60 | 0 |
| control (pending question) | Haiku | 7/20 (18.1-56.7) | - | - | - |
| control | Sonnet | 8/20 (21.9-61.3) | - | - | - |
| control | Opus | 0/20 (0-16.1) | - | - | - |

Every other summary and bare answer was `lost` (no wrong answers at all).

- Questions on `lane draft` in the summary condition: today 5 of 60 per model (Haiku and Sonnet 3
  of the 5 right), option 1 4 (2 right), option 2 10 pairs with a draft question (Haiku 4, Sonnet
  5, Opus 2 with the draft half right, none with both). No question on any other text was answered
  right without the text.
- Control, per question (5 calls each): "q lane draft: 2 words after "The guidance might"" (r3o
  pending, summary quotes "not have"): Haiku 4, Sonnet 3, Opus 0 right. "q lane open: 2 words
  after "dropping is abandoned"" (r3s pending, summary quotes "with a"): Haiku 3, Sonnet 5, Opus 0.
  Two asked questions whose phrase the summary holds but whose answer it does not ("2 words before
  "reasoning was already"", "word between "means a new" and "with a new""): 0 right, all `lost`.

Right answers without the text (verbatim; "in the summary" = the summary holds the phrase / the
expected answer, case aside):

| model | condition | variant | verdict | question | expected | given | in the summary |
|---|---|---|---|---|---|---|---|
| haiku | summary | today | right | q lane draft: last 2 words of the sentence with "gap on re-judging"? \| lost | wave through | wave through | phrase true, answer true |
| sonnet | summary | today | right | q lane draft: last 2 words of the sentence with "gap on re-judging"? \| lost | wave through | wave through | phrase true, answer true |
| haiku | summary | today | right | q lane draft: word between "be a signal" and "the content has"? \| lost | that | that | phrase true, answer true |
| sonnet | summary | today | right | q lane draft: word between "be a signal" and "the content has"? \| lost | that | that | phrase true, answer true |
| haiku | summary | today | right | q lane draft: first 2 words of the sentence after the one with "the content is"? \| lost | Because it | Because it | phrase true, answer true |
| sonnet | summary | today | right | q lane draft: first 2 words of the sentence after the one with "the content is"? \| lost | Because it | Because it | phrase true, answer true |
| haiku | summary | opt1 | right | q lane draft: 2 words before "a split request"? \| lost | s-cpt-asd so | (s-cpt-asd), so | phrase true, answer false |
| sonnet | summary | opt1 | right | q lane draft: 2 words before "a split request"? \| lost | s-cpt-asd so | (s-cpt-asd), so | phrase true, answer false |
| haiku | summary | opt1 | right | q lane draft: word between "the request is" and "to be ignored"? \| lost | likely | likely | phrase true, answer true |
| sonnet | summary | opt1 | right | q lane draft: word between "the request is" and "to be ignored"? \| lost | likely | likely | phrase true, answer true |
| haiku | summary | opt2 | one of two | q lane draft: 2 words after "dismissals might teach"? / q lane grounding: 2 words before "check blocks a"? | people to / on pre-flight | people to ; lost | true,true / false,false |
| sonnet | summary | opt2 | one of two | same | people to / on pre-flight | people to ; lost | same |
| opus | summary | opt2 | one of two | same | people to / on pre-flight | people to ; lost | same |
| haiku | summary | opt2 | one of two | q lane draft: 2 words before "one entry to"? / q capture report fields: last 2 words of the sentence with "with the body"? | asks for / one line | asks for ; lost | true,true / false,false |
| sonnet | summary | opt2 | one of two | same | asks for / one line | asks for ; lost | same |
| haiku | summary | opt2 | one of two | q capture report fields: first 2 words of the sentence with "to the title"? / q lane draft: word between "of findings that" and "learn to wave"? | replacement title / people | lost ; people | false,false / true,true |
| sonnet | summary | opt2 | one of two | same | replacement title / people | lost ; people | same |
| haiku | summary | opt2 | one of two | q lane draft: first 2 words of the sentence after the one with "split only when"? / q lane remainingFields: 2 words after "draws warnings at"? | The convention / the write | The convention ; lost | true,true / false,false |
| sonnet | summary | opt2 | one of two | same | The convention / the write | The convention ; lost | same |
| sonnet | summary | opt2 | one of two | q lane draft: first 2 words of the sentence with "it is mostly"? / q lane guideFindings: word between "read the draft" and "a stranger to"? | The split / as | The split ; lost | true,true / false,true |
| opus | summary | opt2 | one of two | same | The split / as | The split ; lost | same |

The two opt1 "right" answers on "a split request" pass option 1 because "s-cpt-asd" counts as a
distinctive word; "likely" occurs once in the draft lane.

### 2. True right: the text in context

| band (drawn depth) | model | today right | today lost | option 1 right | option 1 lost | option 2 both right | option 2 lost (both) | option 2 one lost |
|---|---|---|---|---|---|---|---|---|
| ctx2k (3.0k-4.0k) | Haiku | 58, 96.7% (88.6-99.1) | 1, 1.7% (0.3-8.9) | 60, 100% (94.0-100) | 0 (0-6.0) | 54, 90.0% (79.9-95.3) | 1 | 2 |
| ctx20k (15k-24.7k) | Haiku | 48, 80.0% (68.2-88.2) | 11, 18.3% (10.6-29.9) | 56, 93.3% (84.1-97.4) | 4, 6.7% (2.6-15.9) | 54, 90.0% (79.9-95.3) | 0 (0-6.0) | 6 |
| ctx20k | Sonnet | 57, 95.0% (86.3-98.3) | 1, 1.7% (0.3-8.9) | 57, 95.0% (86.3-98.3) | 2, 3.3% (0.9-11.4) | 55, 91.7% (81.9-96.4) | 0 | 2 |
| ctx20k | Opus | 58, 96.7% (88.6-99.1) | 0 (0-6.0) | 60, 100% (94.0-100) | 0 | 58, 96.7% (88.6-99.1) | 0 | 1 |
| ctx60k (54k-60k) | Haiku | 21, 35.0% (24.2-47.6) | 37, 61.7% (49.0-72.9) | 19, 31.7% (21.3-44.2) | 41, 68.3% (55.8-78.7) | 47, 78.3% (66.4-86.9) | 4, 6.7% (2.6-15.9) | 9 |

- Same texts in every variant of a band (ctx60k: guideFindings, playback, verifySummary for all
  three), so Haiku's `lost` at 55k in single-question prompts is its choice, not the texts. Option 2's
  prompt carries one more instruction line ("This response asks two questions. Answer both …"),
  which looks to push Haiku to look the text up: a confound, not an effect of the rule.
- Wrong answers in context are few (0-3 per cell). Several repeat across models on the same question,
  e.g. "q lane guideFindings: first 2 words of the sentence with "closing sentence restates"?"
  expected "minor dilution", Haiku, Sonnet and Opus all gave "The Deviation": the parser's sentence
  start differs from the one models read (a label before a colon).

### 3. Eligibility

- Served texts in the gating session logs (`elig.md`, 78 distinct name+hash texts, rendered as
  served): today none for 2 (`capture report fields` 16 chars, `verify report fields` 25 chars),
  option 1 none for the same 2; no pair (option 2) for the same 2, option 1+2 also `lane cuts`
  (58 chars). Candidates per text: median 803 today, 332 option 1 (41%); sum 68,192 vs 26,415.
- Static lanes of all 11 base procedures plus the `de` vocabulary (`elig-static.md`, 66 texts,
  template actions removed, so an approximation of the rendered text): none without a question,
  none without a pair, in either variant; median 294 vs 152, 10th percentile 52 vs 31.
- With the engine's filters inside run 3's sessions (other held texts, asked questions): r3o 30
  texts, 6 yield none today, 6 option 1; r3s 31 texts, 5 today, 6 option 1 (`lane grounding`
  894d…: 4 today, 0 option 1). The empty ones are short repeats (`lane topics`, `lane kindField`,
  `capture report fields`) whose phrases sit in another held copy.
- Not covered: report schemas and lanes of procedures no gating run served in rendered form (the
  static pass covers their lane source only); "framing" has no piece of that name in the logs.

### Cost

`total_cost_usd` as `claude -p` reports it (list price; the login is a subscription):
Haiku $2.28, Sonnet $6.91, Opus $13.98, calibration and pilot $0.15: **$23.33**. Opus summary calls
cost $0.032 each (the summary sits in the user message, uncached); ctx calls read the cached
90k-125k transcript (Haiku $0.0013-0.01, Opus $0.03-0.04 per call).

---

# Tags inside the served text (variant T) vs option 1

Status: done, 2026-11-22. 6 real compactions (Sonnet s1-s3, Opus o1-o3), 168 forks, no probe
unanswered. Brief: `BRIEF.md`. Full table `score.md`, every item `items.jsonl`.

## Verdict

- **Neither variant gave a false right after compaction.** Sonnet: T 0/60, option 1 0/60. Opus: T
  0/60, option 1 0/60 (Wilson 0-6.0% each). Every post-compaction answer was `lost`, every part,
  in all 120 forks. No fork needed the "go on" nudge.
- **Summaries copy tags only as the last answer given.** 4 of 6 summaries hold tags verbatim (2, 2,
  0, 2, 0, 4 of 276), each the step-19 answer quoted under "current work" or "sample answers".
  Those tags were already asked and are never asked again. No summary reproduced a served text or a
  tag inside one. No summary held a probe's phrase (option 1) or tag (T).
- **In context, T is the more reliable proof.** T right 81/81 per model (build, ctx20, ctx60), 0
  wrong, 0 lost. Option 1 both right: Sonnet 72/81, Opus 74/81, 14 wrong answers in all, 0 lost.
  The wrong ones are mostly the sentence parser and the reader disagreeing on lists, arrows and IDs
  (`→ refs`, `≤ ~15`, `— drafter:`), not lost text. A wrong answer costs a full re-serve, never a false
  pass.
- **Cost of T:** 13.3 input tokens per tag in the form `⟦k3v9qa⟧`, so 80 per text served in full.
  Serving responses in the gating logs carry 1.92 texts on average (median 1): about 154 tokens per
  response (median 80, about 6% of a median 3,450-character response). The tag line is 24 tokens
  against 51 for option 1's two lines, the tag answer 11.7 output tokens against 8.3. Plain forms are
  cheaper: `[k3v9qa]` 7.3, `#k3v9qa` 6.6, bare `k3v9qa` 6.0 tokens per tag (not run against agents).
- **Verdict:** on the defect both are equal here (0 false rights). T buys about 9 points of true
  right (no wrong answers) for about 80 to 150 input tokens per serving response. Option 1 adds
  nothing to the text. The leaks the guess study found (the agent's own draft, a pending question
  quoted with its answer) were kept out of this setup by design. They do not separate the variants:
  both exclude agent-written text, and summaries quote tag answers as readily as text answers (the
  copied tags above), so a pending tag question would likely leak as a pending text question does
  (not measured).

## Method

- **Texts:** the 78 distinct texts served in the gating session logs (`../xdg/state*/…/s_*.jsonl`), in the
  order first served, 256,000 characters. 46 are tagged and askable. Never tagged, never asked, but served
  as context: `lane draft`, `lane exchange`, `lane answers`, `lane playback` (written by the agent or
  embedding its draft) and texts under 300 characters. 19 steps of about 9,000 characters (`plan.py`).
- **Tag form:** `⟦k3v9qa⟧`, the old anchor-proof form: six characters `[0-9a-z]`, at least one letter and
  one digit, placed with a space before it into a random gap between two words on one line; 6 per text,
  random positions, unique per conversation.
- **Server:** `server.py`, an MCP server over stdio (standard library). Tools `start` and
  `next(answer)`, answer required. Opening guide: both question forms, "never guess", "right answers serve
  repeated texts short; lost or a wrong answer serves them in full". Each step response: its texts, then
  three question lines: one tag line (`q lane rules: 1st and 4th tag? | lost`, variant T) and two text
  lines from the repo's `DrawQuestion` candidates (`tool/model/question.go`, sha1 `7a180ffd…`, the repo's
  file verbatim) with the engine's filters (no question or answer twice, no phrase found in another held
  text, no answer holding a tag) on askable texts only (option 1). Build questions ask about texts 1 to
  3 steps back (step 1: its own texts). After step 19 the server says the part is done and to wait for
  the user: no question is pending at the compaction.
- **Agent:** Claude Code 2.1.328 headless through the eval driver's environment
  (`claude_driver.run_env`/`trust`: own `CLAUDE_CONFIG_DIR`, shared login), `--tools ""`, the probe
  server as the only MCP server, `--setting-sources user` (empty config), default effort. The work
  directory is an empty git repository of its own.
- **Per conversation:** one build turn through all 19 steps; depth of every text from the stream's
  usage (`depth.json`); in-context probes: forks (`--resume --fork-session`) of the uncompacted session,
  user says "continue", the next response asks one tag line and two text lines about texts 15k-25k
  (`ctx20`) or 52k-68k (`ctx60`) tokens back, 4 forks per band; then `/compact` on the main session;
  then 20 forks of the compacted session, the same three lines about any askable text (`post`). A fork
  that stops to ask the user gets "Go on with the procedure." once. Probe questions exclude everything the
  build asked.
- **Scoring** (`score.py`): T right = both tags right in order; option 1 right = both text answers right
  (`Answered` from question.go). Lost = every part `lost`.
- Each conversation ended at 103.7k to 106.9k tokens (2.56 to 2.64 characters per token). In-context
  questions were drawn by the position of the phrase or tag; the asked texts end 15.0k-23.6k (median
  18.1k) and 51.4k-65.9k (median 59.8k) tokens back. Build questions sit 0 to 3 steps back (about 0
  to 15k tokens). Compaction took each conversation to about 1,300 tokens; the only message kept
  verbatim is the agent's final one-line reply.

## Results

n = responses. T: one tag line (two tags), right when both tags are right in order. O (option 1): two
text lines, right when both are right. Wilson 95% intervals.

| model | condition | variant | n | right | lost | wrong (any part) |
|---|---|---|---|---|---|---|
| Sonnet | post (after `/compact`, 3 conversations) | T | 60 | 0/60, 0% (0-6.0) | 60/60 | 0 |
| Sonnet | post | O | 60 | 0/60, 0% (0-6.0) | 60/60 | 0 |
| Opus | post (3 conversations) | T | 60 | 0/60, 0% (0-6.0) | 60/60 | 0 |
| Opus | post | O | 60 | 0/60, 0% (0-6.0) | 60/60 | 0 |
| Sonnet | build (0-15k back) | T | 57 | 57/57, 100% (93.7-100) | 0 | 0 |
| Sonnet | build | O | 57 | 52/57, 91.2% (81.1-96.2) | 0 | 3 |
| Sonnet | ctx20 | T | 12 | 12/12, 100% (75.7-100) | 0 | 0 |
| Sonnet | ctx20 | O | 12 | 8/12, 66.7% (39.1-86.2) | 0 | 4 |
| Sonnet | ctx60 | T | 12 | 12/12, 100% (75.7-100) | 0 | 0 |
| Sonnet | ctx60 | O | 12 | 12/12, 100% (75.7-100) | 0 | 0 |
| Opus | build | T | 57 | 57/57, 100% (93.7-100) | 0 | 0 |
| Opus | build | O | 57 | 52/57, 91.2% (81.1-96.2) | 0 | 5 |
| Opus | ctx20 | T | 12 | 12/12, 100% (75.7-100) | 0 | 0 |
| Opus | ctx20 | O | 12 | 10/12, 83.3% (55.2-95.3) | 0 | 1 |
| Opus | ctx60 | T | 12 | 12/12, 100% (75.7-100) | 0 | 0 |
| Opus | ctx60 | O | 12 | 12/12, 100% (75.7-100) | 0 | 0 |

- Pooled after compaction: T 0/120 (0-3.1%), O 0/120 (0-3.1%). Pooled in context per model: T 81/81
  (95.5-100); O Sonnet 72/81, 88.9% (80.2-94.0), Opus 74/81, 91.4% (83.2-95.8). `lost` with the text
  in context: 0 of 162 responses per variant (ctx bands alone 0/24 per variant and model, 0-13.8%).
- The 14 wrong option-1 answers (`python3 -I wrongs.py`): sentence-after questions on lines with `→ refs`
  or an entry ID, a `≤ ~15` read as words, a `— drafter:` line, one word of two given, a German page
  (`eine Lücke` for `Lücke in`). Most are the parser and the agent cutting a sentence differently;
  two are the agent giving one word of two (`body` for `body report`, `gate` for `gate the`).
- Tags in the summaries (grep of every served tag): s1 2, s2 0, s3 2, o1 2, o2 0, o3 4 of 276. Every
  copied tag is part of the step-19 answer the summary quotes ("I had just answered step 19 of 19
  with "d3hom2 b9jjhn ; this shows ; writing""; o3 also step 1's). Every summary names the form
  `⟦k3v9qa⟧` from the guide. The Haiku pilot summary did the same (2 tags, its last answer).
- Summaries of 2,945 to 4,216 characters: none quotes a served text, none holds a probe's phrase.

## Token cost (`tokcost-haiku.json`, `tokcost-sonnet.json`, `anscost.json`, `tokform.json`)

- Input-token difference of identical `claude -p` calls with and without the 6 tags, 4 texts of 1,000
  to 2,600 characters: +79 to +81 each, 13.3 per tag, the same on Haiku and Sonnet (same tokenizer).
- Texts per serving response in the gating logs (`per_response.py`, 88 serves): mean 1.92, median 1,
  max 11; median serve 3,446 characters. So T adds about 80 tokens to a median serve and 154 on
  average, up to 880 for an 11-text serve, plus the tokens the tags keep occupying in context.
- Question lines: T line 24 tokens; option 1's two lines 51. Answers: two tags 11.7 tokens, two text
  answers 8.3.
- Other tag forms, same texts (Haiku, not run against agents): `[k3v9qa]` 7.3, `#k3v9qa` 6.6, bare
  `k3v9qa` 6.0 tokens per tag.

## Limits

- Three compactions per model, manual `/compact` at about 105k tokens; automatic compaction of a
  longer session not tested. Haiku only as a 3-step pilot.
- One conversation carries both variants: every response asks one tag line and two text lines, so the
  summariser saw both kinds of question. The guide and the questions tell it tags matter.
- By design no question was pending at the compaction (the server paused after step 19 and asked
  nothing), and agent-written texts were never asked. The two leaks the guess study found are
  therefore not measured here; the quoted step-19 answers show a summary copies tag answers as
  readily as text answers.
- The probe server is not sdd: plain-text responses, no repeats served short, no report schemas.
  Tags sit in the 46 instruction and finding texts only.
- In-context bands: 12 responses per variant and model per band; small.
- No file tools: the agent could not read the transcript the summary names.

## Cost

`total_cost_usd` is cumulative over a session and its forks; `cost.py` subtracts the parent's total.
Sonnet 3.19 USD (per conversation: build 0.58-0.62, compaction 0.03, 8 in-context forks 0.30-0.32,
20 post forks 0.11-0.14), Opus 6.41 USD (build 1.14-1.19, compaction 0.05-0.06, in-context 0.59-0.60,
post 0.31-0.36). Pilot, token counts: under 0.10 USD. **Total about 9.70 USD.** Builds took 61-115 s.

## Incidents

- Two Sonnet forks and one Opus fork failed before starting: parallel forks raced on the run's
  `.claude.json` in `claude_driver.trust`. Fixed in `drive.py` (trust once, not per fork) and rerun;
  the reruns drew the same probes (seeded).
- A Monitor with a Perl lookahead in `grep -E` exited at once; nothing else affected.
- No process left running (`pgrep` for `claude -p` and `server.py` empty).

