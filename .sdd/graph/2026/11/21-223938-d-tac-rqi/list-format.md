# Format of a graph check and of the verifier's start

A graph check sends no entry in full. It sends lines, marks and counts, and the instruction:

> Open every entry you would reference and every entry that could contradict the draft. Then open
> those that look like bearing on the topic.

The agent opens entries by ID, several in one call. At the verifier's start only the entries the
draft references come in full; everything else is lines with marks.

## Marks

| Mark | Meaning |
|---|---|
| `!` | likely bearing: the line owes an answer, a verdict or a reference |
| `.87` | the judge's score |
| `?` | not judged |
| `~ref` | one reference step from an entry the draft references |
| `{open}` | status |
| `→ refined by <id> (newer)` | a newer entry refines this one |

Not on a line: which search found the entry. That information orders the candidates before
judging and protects an entry found by two or more searches from the known-far floor; shown to
the agent it would act as a second ranking against the entries topic listings exist for.

## Groups

- likely bearing: every line with title and first summary sentence; each owes an answer.
- in doubt: at most 100 lines, the first 30 with a summary sentence, the rest with the title; then
  at most 300 title lines; then a count.
- set aside: counts per reason, no IDs.
- header: candidates found, how many judged, how many shown and set aside.

## Scenarios (IDs shortened, titles invented)

Narrow draft, judge complete:

```
# graph check   35 candidates · judged 35 of 35 · 18 shown · 17 set aside

## likely bearing (3) — each owes a verdict or a reference
! .91 ~ref  d-tac-a1b {open}  Session log write retries
      A write that meets another writer catches up once, then fails.
! .84       s-tac-c2d {open}  Ranking record races the capture
      Two background records around one write leave the capture stuck.
! .72 ~ref  d-cpt-e3f {active} → refined by d-tac-g4h (newer)
      Event-sourced session model
      Every state change of a session is an appended event.

## in doubt (15)
  .55       d-tac-g4h {open}  Catch-up bound for session writes
      One catch-up folds everything that landed before it.
  … 14 more lines with a summary sentence

## set aside (17)
  12 rated no higher than entries far from the draft · 5 below the learned line
```

Broad draft: 249 candidates, 5 likely bearing, 100 in doubt (30 with a summary sentence, 70 with
the title), 7 by title, 137 set aside.

Time ran out with part judged: the header says `judged 62 of 249 (time limit 1m) · rest follows`;
judged lines come first; lines not judged yet carry `?` and stand in the merged order of the
searches; a later page shows them with their score.

Judge failed: the header says `judged 0 of 93: judge timed out (limit 1m)`; one group
`not judged`, in the merged order of the searches; the first 5 carry `!` and owe an answer, so
the duty does not vanish with the judge; the first 50 lines carry a summary sentence.

The verifier's start:

```
# reading for the verifier   draft references 4 entries: in full below
[4 entries in full]

## marked for you (6) — say for each whether you opened it
! .90 closes a target                        d-tac-q9r {open}   Earlier limit on list lines
! .86 drafter: dismissed, topic-only         s-tac-s1t {open}   Summary lines per check
! .81 top-ranked, not read by the drafter    d-tac-u2v {active} …

## in doubt (48)
  .58 drafter: read   …
  .44                 …

## set aside (71)
  52 rated no higher than entries far from the draft · 19 below the learned line
```
