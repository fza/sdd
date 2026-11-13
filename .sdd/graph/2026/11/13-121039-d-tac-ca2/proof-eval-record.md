# Proof coverage eval: three Codex capture runs

Build under test: `fza-fork` at `369cb6fe`, after the proof wording "proofs never carry over: list the tags from every earlier result, not only the last".

Shape: the eval-codex driver, one scratch repository per run, `verify.mode: deep`, `verify.runner: codex`, one decision capture per run with a subagent playing the user from the capture card (decision: the parser module rejects empty input with an explicit error; reason: empty input hid upstream bugs twice).

Method: per sdd tool call, the tags served in all earlier results of the same agent context, how many of them the call proved in `held`, whether it proved only the last result's tags while older tags existed, wrong words, and paragraphs of at least 200 characters received again after an earlier result carried them (tags stripped before comparing).

| Run | Calls | Mean proof coverage | Calls proving only the last result | Repeated characters | Total characters | Real wrong proofs |
|---|---|---|---|---|---|---|
| 1 | 36 | 0.11 | 17 | 52,027 (29%) | 177,258 | 2 (a space after the colon) |
| 2 | 33 | 0.07 | 15 | 67,179 (34%) | 197,715 | 2 (a word taken from before the tag; a normalized word) |
| 3 | 22 | 0.09 | 9 | 40,507 (29%) | 141,604 | 1 (a word guessed where a pointer prints a tag followed by a dash) |

Wrong-proof counts exclude pointer lines that re-print a tag followed by a dash, which the counting script first read as the tag's word.

No pointer named text the agent no longer held. All three captures completed. The largest single result was 15,827 characters.
