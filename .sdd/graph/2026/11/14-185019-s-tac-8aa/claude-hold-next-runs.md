# Hold-next live runs with Claude Code agents

One deep-verifier decision capture per run in a scratch repository, the same user words in every run, driven headless through `claude -p` (Claude Code client name `claude-code`). The verifier ran as a subagent in every run.

- gated: fza-fork at 506a8d18, hold-next served to Claude clients; every result carried `hold_next`
- baseline: d98b01fc, the fza-fork commit before the first hold-next merge (every-tag proof rule)

Measured per agent context with the held-proof measurement script: `held` = characters of the proofs the agent sent; `every` = characters of proving every tag served so far in that context; `result` = bytes of the results sdd returned; `repeat` = bytes of paragraphs served again in full; `wrong` = proofs whose word does not follow that tag in the served text, or whose tag was never served.

| run | model | context | calls | held | every | result | repeat | wrong |
|---|---|---|---|---|---|---|---|---|
| cur1 | Sonnet | main | 18 | 74 | 9,960 | 145,318 | 29,850 | 1 |
| cur1 | Sonnet | verifier | 7 | 414 | 648 | 52,982 | 1,362 | 0 |
| cur2 | Sonnet | main | 17 | 3,555 | 6,414 | 100,220 | 10,700 | 10 |
| cur2 | Sonnet | verifier | 5 | 304 | 463 | 47,091 | 908 | 0 |
| cur3 | Sonnet | main | 16 | 416 | 8,032 | 121,536 | 25,802 | 1 |
| cur3 | Sonnet | verifier | 7 | 473 | 717 | 48,639 | 1,362 | 0 |
| opus1 | Opus | main | 16 | 3,701 | 6,702 | 107,571 | 11,029 | 0 |
| opus1 | Opus | verifier | 7 | 456 | 756 | 65,174 | 1,362 | 0 |
| base1 | Sonnet | main | 14 | 81 | 4,707 | 97,854 | 18,596 | 1 |
| base1 | Sonnet | verifier | 6 | 459 | 529 | 46,176 | 252 | 0 |
| base2 | Sonnet | main | 15 | 0 | 7,247 | 123,736 | 23,595 | 0 |
| base2 | Sonnet | verifier | 7 | 577 | 661 | 52,116 | 378 | 0 |
| base3 (to turn 4) | Sonnet | main | 16 | 772 | 7,042 | 109,361 | 14,916 | 7 |
| base3 | Sonnet | verifier | 7 | 591 | 675 | 51,886 | 378 | 0 |

Sonnet totals, gated: held 5,236 against every-tag 26,234 (80% less); result bytes 515,786, about 172 KB per run, against 319,882 for the two complete baseline runs, about 160 KB per run; wrong proofs 12 against 8 (1 in the two complete baseline runs). The third baseline run stopped before its last two turns because a tool guard refused the user's line, so its main row covers turns 1 to 4.

Proof coverage: cur1's main agent sent proofs only on its first `start_procedure` call; cur3's main agent skipped them on 4 calls; base2's main agent sent none; base3's main agent sent them on its first 4 calls only. Seven of cur2's ten wrong proofs repeat one tag with a field name as its word. The Opus main agent sent proofs on every call after the first, none wrong. Verifier subagents sent proofs on every call after `resume_session`, none wrong.

Three runs per build differ in the verifier's questions and in extra playbacks, so a 7% difference in result bytes is within run-to-run variation.
