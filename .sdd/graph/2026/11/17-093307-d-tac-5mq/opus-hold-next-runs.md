# Hold-next on Opus: three captures per build

Setup: Opus main agent with a deep verifier subagent, one capture card per run. Gated build: `fza-next` at 3ea6f35f (hold-next line served to Opus). Base build: d98b01fc, the merge before hold-next (every-tag proof rule). Three runs per build.

| Measure | base (d98b01fc) | gated (3ea6f35f) | change |
| --- | --- | --- | --- |
| Held characters, main agent plus verifier, per run | 4,137 / 4,631 / 4,450 | 4,584 / 4,892 / 4,513 | |
| Held characters, total | 13,218 | 13,989 | +5.8% |
| Result bytes, total | 513,289 | 519,696 | +1.2% |
| Wrong proofs | 0 | 1 | |

Criterion 4 of the hold-next decision asked for at least 70% fewer held characters with result bytes no higher and no more wrong proofs: not met.

Observations:

- Every call after the first carried held proofs in all six runs: Opus complies under both rules.
- Both builds sent about 42 to 48% fewer held characters than the every-tag counterfactual.
- In gated run 1 the hold-next lists grew to 5, 10, 18 and then 20 tags (the cap): sdd named nearly every tag a later step may repeat, so the named list was no shorter than what Opus sent under the every-tag rule. The prediction was too broad; agent compliance was not the cause.
- The one wrong proof came on the summary check step of gated run 1.

Run deviations: a guard false positive made four drivers drop quotes and a final period from the edit line; the deep verifier runs only after the first "Yes, write it", so the card's post-verifier edit changed nothing in any run.
