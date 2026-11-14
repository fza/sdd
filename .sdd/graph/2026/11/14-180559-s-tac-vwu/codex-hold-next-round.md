# Hold-next live round with Codex main agents

Six deep-verifier captures of one gap observation in a scratch repository, the same user words in every run. Main agents: `gpt-5.3-codex` through Opper. Verifiers: Codex subagent threads (base2: headless Claude, after its agent answered that it had no subagent tool).

- current: fza-fork at 8e7e2c0e (hold-next line, its four follow-up fixes)
- baseline: d98b01fc, the fza-fork commit before the first hold-next merge (every-tag proof rule)

Measured per agent context with the held-proof measurement script: `held` = characters of the proofs the agent sent; `every` = characters of proving every tag served so far in that context; `result` = bytes of the results sdd returned; `repeat` = bytes of paragraphs served again in full; `wrong` = proofs whose word does not follow that tag in the served text, or whose tag was never served.

| run | context | calls | held | every | result | repeat | wrong |
|---|---|---|---|---|---|---|---|
| cur1 | main | 19 | 0 | 9,262 | 115,706 | 28,573 | 0 |
| cur1 | verifier | 5 | 279 | 383 | 43,224 | 908 | 0 |
| cur2 | main | 28 | 1,460 | 16,426 | 166,510 | 56,941 | 1 |
| cur2 | verifier | 9 | 472 | 1,327 | 66,104 | 11,906 | 0 |
| cur3 | main | 16 | 321 | 5,954 | 94,208 | 24,688 | 0 |
| cur3 | verifier | 4 | 180 | 352 | 41,837 | 454 | 0 |
| base1 | main | 21 | 0 | 11,884 | 120,332 | 28,499 | 0 |
| base1 | verifier | 7 | 541 | 625 | 42,658 | 252 | 0 |
| base2 | main | 28 | 0 | 24,808 | 194,536 | 63,764 | 0 |
| base2 | verifier (headless Claude) | 4 | 298 | 340 | 44,469 | n/a | 0 |
| base3 | main | 18 | 0 | 9,200 | 113,064 | 28,525 | 0 |
| base3 | verifier | 5 | 307 | 363 | 42,630 | 252 | 0 |

Totals: current held 2,712 against every-tag 33,704 (92.0% less); result bytes current 527,589, baseline 557,689 (5.4% less); main agents only 376,424 against 427,932 (12% less). Wrong proofs: current 1, baseline 0 (the baseline main agents sent no proofs).

Main-agent proof coverage on the current build: cur1 sent an empty `held` list on all 18 calls after the first; cur2 proved 99 of 412 tags its hold-next lines named, cur3 22 of 183. Verifier subagents proved 19 of 19, 31 of 49 and 12 of 15.
