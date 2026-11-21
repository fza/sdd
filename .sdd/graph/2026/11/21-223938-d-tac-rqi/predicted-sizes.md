# Predicted sizes once the decided changes are built

Estimates. They combine the sizes measured for three real drafts (a narrow topic, a broad topic, a
draft with few references) with the shares measured for each rule on five graphs. No judged graph
check could be measured: the judge scored nothing on the measuring machine. Ranges, not points.

## Inputs

- Median sizes: entry in full 2,200 characters; line with title 180; line with title and summary
  sentence 320; title line 61.
- Known-far floor: sets aside 38 to 47% of about 65 candidates and 47 to 63% of large lists; in
  its careful form 24 to 40% and 38 to 54%.
- Learned threshold with a proof: on top of the floor, roughly another tenth of the candidates
  (the two rules overlap; the overlap is not measured).
- Entries that surely bear: 3 to 6 per check, served in full.
- Entries in doubt: at most 100 lines, then 300 titles, then a count.
- A capture calls the graph check about three times today, and each call prints the whole list.
  After the change a call that opens entries prints only those entries.

## The drafter's graph check, per capture

| Draft | Candidates | Today: one call | Today: capture (3 calls) | After, careful floor | After, with proof |
|---|---|---|---|---|---|
| narrow topic | 35 | 7 KB | 21 KB | about 11 KB | about 10 KB |
| few references | 93 | 19 KB | 58 KB | about 21 KB | about 18 KB |
| broad topic | 249 | 51 KB | 154 KB | about 36 KB | about 28 KB |

"After" holds 3 to 5 entries in full (7 to 11 KB) that the agent opens by itself today, at about
2 KB each plus a reprint of the list. A narrow check alone gets larger per call, since entries
now arrive in full; the capture as a whole gets smaller.

## The verifier's reading

| Draft | Today | of that rules | of that summary lines | After | Change |
|---|---|---|---|---|---|
| narrow topic | 74 KB | 35 KB | 10 KB (32 lines) | about 70 KB | about -5% |
| few references | 83 KB | 36 KB | 32 KB (92 lines) | about 67 KB | about -20% |
| broad topic | 155 KB | 37 KB | 91 KB (246 lines) | about 97 KB | about -35% |

The saving comes from the summary lines: set-asides and the limit of 100. The rules part, 35 KB
of every reading, is untouched by any decision.

## Everything sdd sends in one long dialogue

Basis: a recorded dialogue of 140 calls and 1,157,000 characters.

| Part | Today | After | Rule |
|---|---|---|---|
| step text | 326 k | about 200 k | identical repeats short when the text question is answered |
| report forms | 226 k | about 85 k | same |
| entries from graph checks, reads, searches | 196 k | about 115 k | limits, set-asides, opens without the list |
| framing | 150 k | about 27 k | sent on change only |
| list of unproved texts | 136 k | about 8 k | replaced by one question line per response |
| text at a move's end | 51 k | about 18 k | short landing |
| checker findings and playback | 55 k | about 23 k | in full once; the change only |
| rest | 17 k | 17 k | |
| total | 1,157 k | about 490 k | about 55 to 60% less |

The step-text and report-form rows assume the agent answers the text question in 9 of 10 calls
(measured 92 to 99% with the text in view). A wrong or "lost" answer sends the text again, so a
weaker agent saves less, never loses text.

## Not in these numbers

- Waiting time: the model call that wrote the searches goes (one call on the main model per
  check); the judge gets up to one minute.
- Background judge calls for replays and scans: about 0.75 to 2.40 US dollars per graph and
  judge for a full proof.
- The verifier's rules part and its verdict briefs: no decision covers them.

## With no entry sent in full

The graph check sends lines and marks only; the agent opens what it needs. Only the entries the
draft references come in full at the verifier's start.

| Draft | Graph check today, capture | Lines only, before opens | With 3 to 4 opens |
|---|---|---|---|
| narrow topic | 21 KB | about 8 KB | about 14 KB |
| few references | 58 KB | about 14 KB | about 22 KB |
| broad topic | 154 KB | about 24 KB | about 33 KB |

| Draft | Verifier's reading today | Lines only, before its own opens |
|---|---|---|
| narrow topic | 74 KB | about 55 KB |
| few references | 83 KB | about 59 KB |
| broad topic | 155 KB | about 83 KB |

Assumed: the 30 best lines carry a summary sentence, the next 70 the title. An agent that opens
the same three or four entries a check would have sent in full ends near the earlier estimate;
the saving shows where it needs fewer. The total for one long dialogue stays near 490 k.
