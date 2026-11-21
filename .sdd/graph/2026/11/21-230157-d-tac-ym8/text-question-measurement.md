# Measurement: a required check answer, and what agents answer after a compaction

Run on 2026-11-21 with headless Claude Code 2.1.328. Two variants on one probe: a code handed out beside the text (variant C), then a question bound to the text (variant T).

**Haiku rows are an aside.** In every run the model named is the main agent that works the procedure. A main agent is a costly model by design, so the rows and figures in which `claude-haiku-5-5` played it do not carry the decision. They are marked "aside" and reported because they are informative. The decision rests on the Opus and Sonnet rows.

## Variant C: a code handed out beside the text

### Method

1. A probe MCP server over stdio, Python standard library. Tools `start` and `next(report, code)`, both arguments required.
2. 24 steps per run. Each response: about 2,000 characters of invented manual text (same in every run), three lines `code <number>: <code>` with 6-character codes, one line naming the number due on the next call.
3. The number comes from the response before (steps 2 to 5), then cycles: response before, 5 steps back, oldest response with an unasked number. Step 1 names a number of its own response, since no earlier one exists. No number is asked twice.
4. Each step's `report` must hold a sign-off phrase stated only in that step's text.
5. Two sentences at `start` explain the rule. Nothing else coaches the agent. A wrong or `lost` answer still gets the next step.
6. The server logs every call: step, number asked, code given, verdict, characters sent.
7. One `claude -p` per turn with a configuration directory of its own, `--strict-mcp-config`, `--tools ""`, `--setting-sources user`. The agent has the two probe tools and nothing else; no instruction file, skill or plugin of a project loads (first request: 4,481 tokens).
8. Runs without compaction: one turn with the given prompt. Two per model.
9. Compaction run: the driver interrupts the turn when the agent holds response 13, resumes the session with `/compact`, then with "continue the procedure". One per model.
10. A scoring script reads the server logs and takes the compaction summary from the turn stream.

### Results

R right, W wrong, L lost. "Before the cut" counts answers after the compaction for numbers issued in responses 1 to 13; "after the cut" for numbers issued later.

| run | model | note | steps | right | wrong | lost | missing | reports right | numbers from before the cut | numbers from after the cut | cost USD | wall s |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| haiku-plain-1 | claude-haiku-5-5 | aside | 24/24 | 24 | 0 | 0 | 0 | 24/24 | - | - | 0.0105 | 31.9 |
| haiku-plain-2 | claude-haiku-5-5 | aside | 24/24 | 24 | 0 | 0 | 0 | 24/24 | - | - | 0.0104 | 47.7 |
| haiku-compact-1 | claude-haiku-5-5 | aside | 24/24 | 19 | 0 | 5 | 0 | 24/24 | 1R 0W 5L | 6R 0W 0L | 0.0119 | 59.9 |
| sonnet-plain-1 | claude-sonnet-5-5 | | 24/24 | 24 | 0 | 0 | 0 | 24/24 | - | - | 0.1557 | 45.2 |
| sonnet-plain-2 | claude-sonnet-5-5 | | 24/24 | 24 | 0 | 0 | 0 | 24/24 | - | - | 0.1586 | 41.4 |
| sonnet-compact-1 | claude-sonnet-5-5 | | 24/24 | 24 | 0 | 0 | 0 | 24/24 | 6R 0W 0L | 6R 0W 0L | 0.1895 | 65.9 |
| opus-plain-1 | claude-opus-5-5 | | 24/24 | 24 | 0 | 0 | 0 | 24/24 | - | - | 0.3175 | 79.9 |
| opus-plain-2 | claude-opus-5-5 | | 24/24 | 24 | 0 | 0 | 0 | 24/24 | - | - | 0.2995 | 57.0 |
| opus-compact-1 | claude-opus-5-5 | | 24/24 | 24 | 0 | 0 | 0 | 24/24 | 6R 0W 0L | 6R 0W 0L | 0.3724 | 137.1 |
| codex-plain-1 | gpt-6-luna | not measured | 0/24 | - | - | - | - | - | - | - | n/a | 4.7 |

Opus and Sonnet, six runs: 144 answers, 144 right. With the three Haiku runs (aside): 216 answers, 211 right, 5 lost, 0 wrong, 0 missing. No copy error, no code of another number, no invented code. All 216 reports held the right phrase. No run needed a nudge; the host rejected no call.

What the compaction did (each cut the conversation from about 14,000 to 16,000 tokens down to about 1,300):

- **sonnet:** the summary held a table of all 39 numbers with their codes. All 6 answers right.
- **opus:** the summary listed the 12 numbers already used and kept the 27 unused numbers with their codes "verbatim". All 6 answers right.
- **haiku (aside):** the summary kept 1 of the 39 codes issued so far, the one due on the next call. That call was right; the other 5 numbers from before the cut were answered `lost`. Nothing made up.

In the Sonnet and the Opus compaction run the step texts were gone and every code still came back right. A right code therefore does not show that the earlier response is still in context: the summariser carries the codes across.

### Limits

- Few runs: two without compaction and one with compaction per model. Each model's behaviour after compaction rests on a single run.
- Codex was not measured: the first turn failed with `usageLimitExceeded`. The Codex driver's interrupt and compaction paths never ran.
- Manual `/compact` only, on a small conversation. Automatic compaction on a long real session was not tested, and its summary may keep less.
- The summariser is the same model and had the rule in view, so it could tell the codes mattered; that is the likely reason it kept them, not a tested one.
- The agent had no file tools. A real Claude Code session has them, and the compaction summary names the transcript file, so an agent could read lost codes back from disk.
- The interrupt leaves a trace: each summary notes that the user interrupted before step 13 was reported.
- Default thinking was on. The model aliases resolved to the ids in the table.
- `missing` could only occur if a call reached the server without `code`; none did.

### Cost

- Reported by Claude Code (`total_cost_usd`, list prices, cumulative per session): 1.53 USD. haiku 0.03, sonnet 0.50, opus 0.99.
- Codex: no model call succeeded, no cost.
- Wall time of the nine runs: 566 s.

## Variant T: a question bound to the text

Second measurement, same day, Claude Code only.

### Method

- Responses carry no codes. Each ends with one question line about an earlier step's text, for example `q s7: 2 words after "the draft must"? | lost`. `next` takes `report` and a required `answer`.
- Ten question kinds, drawn from a deck shuffled per run seed: 2 words after a phrase (`after`), 2 words before (`before`), first 2 words of the phrase's sentence (`sent_first`), last 2 words of it (`sent_last`), the word between two phrases (`between`), first 2 words of the following sentence (`next_first`), first 2 words of paragraph K (`para_first`), last 2 words of paragraph K (`para_last`), word N (3 to 6) of the sentence starting with two given words (`word_n`), the number after a phrase (`number`).
- A phrase is three words that occur exactly once in the asked step's text. The server picks the question when it renders the response and computes the answer from the stored text. No phrase is asked twice.
- Step texts are built per step from 24 slot-dense sentence patterns filled from seeded word lists. Every answer word is a slot word, and a question is dropped when another step's text gives the same answer to it. So what follows a phrase is not a template string.
- Asked step as in variant C: itself for response 1, the step before for 2 to 5, then the cycle before, 5 back, oldest step asked fewer than three times. After a cut at step 12, six calls ask about steps from before the cut.
- Verdicts: right, first word only, wrong, lost, missing. A wrong answer is classed as off by one (neighbouring paragraph or word), the same question answered on another step's text, words from elsewhere in the same text, words from another step's text, or a guess found in no text.
- Runs: 24 steps at about 2,000 characters per step, 24 steps at about 6,000 characters, and two runs per model with `/compact` after step 12 at 2,000 characters.

### Results

R right, F first word only, W wrong, L lost.

| run | model | note | text chars | steps | right | first word only | wrong | lost | missing | reports right | steps from before the cut | steps from after the cut | cost USD | wall s |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| t-2k-haiku | claude-haiku-5-5 | aside | 2000 | 24/24 | 24 | 0 | 0 | 0 | 0 | 24/24 | - | - | 0.0130 | 49.2 |
| t-2k-sonnet | claude-sonnet-5-5 | | 2000 | 24/24 | 24 | 0 | 0 | 0 | 0 | 24/24 | - | - | 0.1687 | 51.3 |
| t-2k-opus | claude-opus-5-5 | | 2000 | 24/24 | 23 | 0 | 1 | 0 | 0 | 24/24 | - | - | 0.3249 | 136.7 |
| t-6k-haiku | claude-haiku-5-5 | aside | 6000 | 1/24 | 1 | 0 | 0 | 0 | 0 | 1/1 | - | - | 0.0071 | 43.5 |
| t-6k-haiku-retry | claude-haiku-5-5 | aside | 6000 | 7/24 | 5 | 1 | 1 | 0 | 0 | 7/7 | - | - | 0.0084 | 35.9 |
| t-6k-sonnet | claude-sonnet-5-5 | | 6000 | 24/24 | 24 | 0 | 0 | 0 | 0 | 24/24 | - | - | 0.3723 | 116.5 |
| t-6k-opus | claude-opus-5-5 | | 6000 | 24/24 | 24 | 0 | 0 | 0 | 0 | 24/24 | - | - | 0.7132 | 106.2 |
| t-compact-haiku-1 | claude-haiku-5-5 | aside | 2000 | 24/24 | 11 | 0 | 1 | 12 | 0 | 24/24 | 6L | 6L | 0.0127 | 71.8 |
| t-compact-haiku-2 | claude-haiku-5-5 | aside | 2000 | 24/24 | 13 | 0 | 0 | 11 | 0 | 24/24 | 1R 5L | 6L | 0.0128 | 109.4 |
| t-compact-sonnet-1 | claude-sonnet-5-5 | | 2000 | 24/24 | 14 | 0 | 1 | 9 | 0 | 24/24 | 1R 5L | 6R | 0.1848 | 88.6 |
| t-compact-sonnet-2 | claude-sonnet-5-5 | | 2000 | 24/24 | 18 | 0 | 1 | 5 | 0 | 24/24 | 1R 5L | 6R | 0.1944 | 220.5 |
| t-compact-opus-1 | claude-opus-5-5 | | 2000 | 24/24 | 24 | 0 | 0 | 0 | 0 | 24/24 | 6R | 6R | 0.6171 | 166.5 |
| t-compact-opus-2 | claude-opus-5-5 | | 2000 | 24/24 | 24 | 0 | 0 | 0 | 0 | 24/24 | 6R | 6R | 0.6118 | 138.3 |

**Text in view** (the runs without compaction plus the first 12 steps of the compaction runs).

Opus and Sonnet: 144 answers, 137 right, 3 wrong, 4 lost, 0 missing. With Haiku (aside): 200 answers, 190 right, 1 first word only, 5 wrong, 4 lost, 0 missing.

- Per model: opus 71 of 72 right (1 wrong). sonnet 66 of 72 (2 wrong, 4 lost). haiku (aside) 53 of 56 (2 wrong, 1 first word only).
- At 6,000 characters: opus 24 of 24, sonnet 24 of 24. haiku (aside) 6 of 8 before the runs stopped.
- Per kind, right of asked:

| kind | Opus and Sonnet | with Haiku (aside) |
|---|---|---|
| `sent_first` | 14/14 | 21/21 |
| `sent_last` | 14/14 | 19/19 |
| `between` | 13/13 | 19/19 |
| `next_first` | 13/13 | 18/18 |
| `number` | 15/15 | 22/22 |
| `after` | 14/15 (1 lost) | 17/20 (2 wrong, 1 lost) |
| `before` | 14/15 (1 lost) | 18/20 (1 first word only, 1 lost) |
| `word_n` | 16/17 (1 lost) | 22/23 (1 lost) |
| `para_first` | 13/14 (1 wrong) | 18/19 (1 wrong) |
| `para_last` | 11/14 (2 wrong, 1 lost) | 16/19 (2 wrong, 1 lost) |

- The seven kinds that count neither paragraphs nor words, Opus and Sonnet: 97 of 99 right, 2 lost, 0 wrong. The three counting kinds (`word_n`, `para_first`, `para_last`): 40 of 45 right, 3 wrong, 2 lost.
- The 6 answers that were wrong or half right:
  - opus answered a `para_last` with the next paragraph's words (off by one).
  - sonnet answered a `para_first` and a `para_last` on the current step's text instead of the step asked. Only the paragraph kinds have no phrase to anchor the step.
  - haiku (aside) gave one word of the two, three times (`after` twice, `before` once).
- The 4 `lost` are one sonnet run answering `lost` at steps 2, 3, 4 and 6 with the text in view (kinds `before`, `after`, `word_n`, `para_last`). No reason is visible in its output.
- No answer was a guess found in no text.

**After `/compact`** (two runs per model, 12 answers per model for steps from before the cut, 12 for steps after):

- **opus:** both summaries reproduced all 13 step texts word for word (26,000 characters; 14,400 to 6,900 tokens). All 24 answers right. The texts were still in view, so the check rightly did not fire.
- **sonnet:** the summaries held none of the step texts (about 14,500 to 15,000 tokens down to about 1,200). Steps from before the cut: 10 `lost`, 2 right. Steps from after the cut: 12 right.
- **haiku (aside):** the summaries held none of the step texts. Steps from before the cut: 11 `lost`, 1 right. Steps from after the cut: 12 `lost`, although those texts were in view.
- The 3 right answers of sonnet and haiku were all the first call after the compaction: the summary had already worked out the answer to the pending question.
- No wrong answer and no guess after a compaction, in any kind. Each kind was asked 1 to 5 times for steps from before the cut; no kind stands out.

**Added output per call** (output tokens per call including thinking, runs without compaction at 2,000 characters; variant C first): opus 123 to 128, sonnet 132 to 145, haiku (aside) 159 to 299 (thinking 24 to 163). At 6,000 characters: sonnet 209, opus 160. The argument grows from 6 to about 10 characters. The question line is about 35 to 90 characters against about 140 for three code lines and the ask line.

### Limits

- haiku (aside) at 6,000 characters was not measured: the provider's content filter stopped both attempts after 1 and 7 answers. The filter also stopped a first haiku run at 2,000 characters on an earlier vocabulary; that run is not counted and the vocabulary was replaced before any counted run. The invented text sets the filter off, not the check.
- One run per model and text size without compaction, two per model with it. With the text in view each kind has 13 to 17 answers of Opus and Sonnet (18 to 23 with Haiku), and 1 to 5 for steps from before a cut.
- The step texts are slot-filled sentences, more uniform than real procedure text and free of repeated boilerplate. Counting words and paragraphs was nearly error-free here; that may not carry over.
- Manual `/compact` on a small conversation, no file tools, and an interrupt trace in every summary, as in variant C.
- Claude Code puts the enclosing repository's branch and status into the session; one summary mentions them. No project instruction file or skill loaded.
- Codex was not measured in this variant either.

### Cost

3.25 USD reported for this measurement: haiku 0.06, sonnet 0.92, opus 2.27. Wall time of the counted runs: 1,334 s.
