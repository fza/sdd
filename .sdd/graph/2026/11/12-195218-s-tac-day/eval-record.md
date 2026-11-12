# Codex capture evals of the serve token-cost build

Each batch ran one scenario three times with a real Codex agent through the eval-codex driver: record one directive with `verify.mode: deep` and `verify.runner: codex`, the user giving the reason and scope when asked, then a one-word wording change after the verifier's verdict.

| Batch | Build | Mean result chars | Largest | sdd calls | User turns | Completed |
|---|---|---|---|---|---|---|
| 1 | parts 1 and 2 | 360,745 | 33,937 | 40 | 5.3 | 3/3 |
| 2 | + headless verifier fixes | 164,620 | 29,280 | 15.7 | 4.0 | 3/3 |
| 3a | + 18,000 bound and guidance fixes | 165,299 | 22,054 | 20.3 | 3.3 | 3/3 |
| 3b | 3a + the four source trims | 196,312 | 22,841 | 32.3 | 5.0 | 3/3 |

Batch 3b ran longer turns than 3a in two runs (one reached the late edit, one restarted its capture), so its means do not compare with 3a's.

## Defects found and fixed

- Batch 1: the headless codex verifier never ran. Its sdd tools were not approved (`MCP tool call requires approval, but approval policy is never`), a round that ended without delivering left the capture waiting with no limit, and the drafter, handed the child handle, judged its own draft. Every wait re-served about 5 KB of framing.
- Batch 2: Codex in code mode prints a tool result's JSON under a 10,000-token output budget, measured at 0.517 tokens per text character, so results above about 19,000 characters lost their middle. The drafter was told pre-flight's model had judged when it had not, withdrawn findings never reached the user, 2 of 80 proofs named the word before the tag, and no agent asked for a missing reason.
- Batch 3: an agent proving nothing never received the capture's later instruction lanes, so the rule to ask for a missing reason reached 1 of 3 runs per batch; the completion serve carried the whole session state (up to 24,139 characters); the late edit started a fresh verifier because headless runners never resumed; a stalled headless round was never surfaced; one agent confirmed playback in the user's name; guide findings dismissed on proceed, findings accepted without an edit, and findings on facts only the user knows were not put to the user.

## Not shown

The paired comparison of batches 3a and 3b, which was to gate the four source trims, could not separate the trims from the batch 3 defects; it was not repeated after their fixes. No agent in any batch asked for a missing reason, even where the rule reached it; 5 of 6 in batch 3 marked their own inference at playback instead.
