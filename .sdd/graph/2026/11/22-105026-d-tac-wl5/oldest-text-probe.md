# Oldest-text question across two linked sessions

Does an agent answer a question about the oldest text of an earlier session far back, and does the question catch lost text after a compaction? Run on 2026-11-22 with headless Claude Code 2.1.328 against a probe MCP server. Main agents: Opus 5.5 and Sonnet 5.5. Haiku was not run as the main agent: a main agent is a costly model by design.

## Method

1. Tools: `start` (session 1), `start2(continues)` (session 2), `next(report, answer)`. All arguments required. `start2` refuses a wrong handle.
2. Session 1: steps 1 to 12 of invented manual text, about 1,800 to 2,000 characters each, about 12,000 at steps 4, 8 and 12. A filler appendix of about 20,000 characters follows steps 2, 4, 6, 8, 10 and 12. That makes 18 responses and about 173,400 characters. The last response names session 1's handle and tells the agent to call `start2` with `continues` set to it.
3. Every session-1 response asks about step 1, the oldest text: 18 questions on the same text.
4. Session 2: step 13 is a new text. Steps 14 to 24 repeat the texts of session-1 steps 1 to 11. Response 13 asks about step 1, response 12+i about step i: always the oldest session-1 text not yet asked about in session 2. The answer about step i decides how the repeat of step i comes next. Right: a pointer of about 270 characters ("Its text is the same as step i of session 1; read it there"). Anything else: in full. The `start2` response says this in one sentence.
5. Question kinds: seven phrase-anchored kinds (`after`, `before`, `sent_first`, `sent_last`, `between`, `next_first`, `number`), drawn from a deck that holds each kind once per seven questions, shuffled per run. Kinds that count paragraphs or words are left out.
6. Guards for the repeated questions on step 1: no answer repeats an earlier answer, and no answer word lies on a position an earlier question or answer already showed, so an answer cannot be pieced together from the agent's own earlier calls. A question is dropped when another text gives the same answer.
7. Distance: tokens of context after the asked text, from the usage on the turn stream. It is the context size of the request that gives the answer minus that of the request that answered in the asked step's own response.
8. Each turn is one headless call with its own config folder, only the probe server, no other tools. For a compaction run the server holds the next call once a given response is out; the driver interrupts the turn, runs `/compact`, then asks the agent to continue. Cut points: after the close of session 1 (`compact-between`), and after step 18 with the agent holding response 19 (`compact-mid`).
9. The analyzer scores the logs, reads the compaction summary and reports the share of each text's 5-word runs the summary holds. For each answer after the cut it checks whether the summary holds the stretch of text from the question's phrase to its answer.

## Results

R right, L lost. No answer in any run was wrong, first word only or missing.

| run | model | answers | right | lost | session 1 | session 2 | reports right | pointers sent | session 2 chars sent / full | cost USD | wall s |
|---|---|---|---|---|---|---|---|---|---|---|---|
| opus-plain-1 | Opus | 30/30 | 30 | 0 | 18R | 12R | 30/30 | 11 | 5,572 / 44,117 | 0.9836 | 86.1 |
| opus-plain-2 | Opus | 30/30 | 30 | 0 | 18R | 12R | 30/30 | 11 | 5,523 / 44,068 | 0.9827 | 64.3 |
| opus-compact-between | Opus | 30/30 | 19 | 11 | 18R | 1R 11L | 30/30 | 1 | 42,483 / 44,103 | 1.0098 | 101.9 |
| opus-compact-mid | Opus | 30/30 | 28 | 2 | 18R | 10R 2L | 30/30 | 10 | 17,414 / 44,088 | 1.1652 | 129.6 |
| sonnet-plain-1 | Sonnet | 30/30 | 30 | 0 | 18R | 12R | 30/30 | 11 | 5,552 / 44,097 | 0.5061 | 81.4 |
| sonnet-plain-2 | Sonnet | 30/30 | 30 | 0 | 18R | 12R | 30/30 | 11 | 5,580 / 44,125 | 0.4966 | 84.2 |
| sonnet-compact-between | Sonnet | 30/30 | 18 | 12 | 18R | 12L | 30/30 | 0 | 44,050 / 44,050 | 0.5155 | 100.5 |
| sonnet-compact-mid | Sonnet | 30/30 | 25 | 5 | 18R | 7R 5L | 30/30 | 7 | 22,662 / 44,073 | 0.5249 | 100.1 |

Totals per model: Opus 120 answers, 107 right, 13 lost. Sonnet 120 answers, 103 right, 17 lost. Every `lost` came after a compaction. Every run completed with no nudge and no host error. Each `start2` got the right handle on its first call.

**Text in view** (all answers of the runs without compaction and every answer before a cut): 204 answers, 204 right.

| model | 0-20k tokens | 20-60k | 60k+ |
|---|---|---|---|
| Opus | 24 of 24 right | 68 of 68 | 10 of 10 |
| Sonnet | 24 of 24 right | 68 of 68 | 10 of 10 |

- The farthest answer sat 66,050 tokens after its text. The question about step 1 at the opening of session 2 came at 65,400 to 66,050 tokens in every run without a cut there. Context peaked at 69,000 to 74,000 tokens. No answer came from past about 66k.
- Per kind, all right: after 26, before 27, between 24, next_first 25, number 26, sent_first 25, sent_last 27.
- 44 pointers in the runs without compaction; every report after a pointer held the right sign-off phrase.

**Characters of session 2** (13 responses; full means every repeat in full):

- Without compaction: 5,523 to 5,580 sent against 44,068 to 44,125 in full, 12.6%. 11 pointers of about 270 characters stood in for 1,800 to 12,000 characters of text each.
- Compaction between the sessions: Opus 42,483 (one pointer), Sonnet 44,050 (none).
- Compaction after step 18: Opus 17,414, Sonnet 22,662.
- Over all 8 runs: 148,836 of 352,721 characters sent.

## What the compaction did

All four summaries were written by the main agent's own model, with the rule and the questions in view.

- **Opus, between the sessions** (69,320 to 1,538 tokens): the summary kept the whole body of step 1 word for word, "kept for any future questions", and nothing of steps 2 to 12. The first question about step 1 came back right from that copy, and the repeat of step 1 went out as a pointer. Steps 2 to 12: 11 lost, all in full. No text the summary lacked was claimed.
- **Sonnet, between the sessions** (69,530 to 1,598 tokens): the summary kept 15 sentences of step 1, several cut off (140 of 203 five-word runs). The question about step 1 was a `sent_last` whose span the summary did not hold: answered `lost`. All 12 session-2 answers lost, all 11 repeats in full.
- **Opus, after step 18** (71,551 to 4,863 tokens, a summary of 18,400 characters): it kept the short texts of steps 7, 9, 10, 11 and 13 word for word and about a quarter of the long steps 8 and 12. Answers about steps 7, 9, 10 and 11 were right, each with its span in the summary. Steps 8 and 12 were lost. The 4 pointers after the cut went to texts the summary held in full.
- **Sonnet, after step 18** (72,184 to 1,505 tokens): the summary held the answer to the pending question, not the text. It named the answer to the question about step 7 and quoted that one sentence of step 7 (11 of 197 five-word runs). That answer came back right. Step 20 then went out as a pointer to step 7, a text the agent no longer held. Its report was still right, because the summary lists every sign-off phrase reported so far. Steps 8 to 12: 5 lost.

Answers after a cut: Opus 1R 11L and 4R 2L, Sonnet 12L and 1R 5L. Every right answer had its span in the summary. No answer after a cut was wrong or a guess.

The one false result is the question pending at the cut, which the summary answers ahead of time. One right answer does not show the text is still held when it answers the question pending at the cut. A report holding the sign-off phrase does not show it either: the summaries carry the phrases as a list.

## Limits

- Few runs: two per model without compaction, one per model per cut point. Each compaction finding rests on a single run.
- Distance reached about 66,000 tokens, and only 20 answers lie past 60k. Nothing was tested past 74,000 tokens of context. The texts were right at every distance, so this probe found no limit.
- Step 1 was asked about 19 times. The guards keep answer words out of earlier questions and answers, but those still put about 90 words of step 1 into recent context. The session-2 texts were each asked about once and also came back all right.
- The summary writer saw the rule and that every question so far was about step 1, which likely explains why Opus kept step 1 word for word. A procedure whose questions rotate shows the summary writer less to aim at.
- Pointer correctness was judged by the sign-off phrase, which the summaries keep. Only the next question shows the loss.
- Manual `/compact` only, after an interrupt the summary mentions. Automatic compaction at a full window was not tested.
- The agent had no file tools. Every summary names the transcript file, so an agent with tools could read lost texts back.
- The step texts are slot-filled sentences. Real procedure text is more uniform and repeats boilerplate.

## Cost

6.18 USD at list prices: Opus 4.14, Sonnet 2.04. Per run without compaction: Opus 0.98, Sonnet 0.50. A compaction added 0.03 to 0.16. Wall time of the 8 runs: 748 s.
