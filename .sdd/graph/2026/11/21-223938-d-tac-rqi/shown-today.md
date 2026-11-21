# What sdd shows an agent: gather and the deep verifier's reading

Measured 2026-11-21 on `fza-next`, main checkout, `bin/sdd` dev build. Graph: 1,873 entries loaded (base procedures included), 1,044 in the live grounding population. Sizes are bytes of the rendered text unless "served chars" is named (`model.ServedChars`, the JSON-escaped cost the page bounds count).

Judge state while measuring (`sdd grounding status`): `chat:claude-haiku-5-5`, calibrated at the top stage 0.98, sets aside below 0.078, serves in full from 0.349, far floor on. Its stored batch size on this machine is 1, one `claude -p` call takes 22 s median and 41 s at the 90th percentile today, and the pass deadline is 45 s.

## Part 1: gather (the drafter's graph check)

### Header

`internal/presenters/gather.go:28-44`:

```
# gather

reads:
- semantic "<query>": 20 entries
- keyword "<term>": 7 entries          (one line per term)
- listing topic(<label>): 18 entries   (one line per draft topic)
- new since the reading began: N entries
- opened <id>, <id>: N entries
corpus: 1044 entries live, mark dcc219551308
prefilter: <off|ok|partial|failed>: <note>
```

A graph below 100 live entries (`DefaultGroundingBriefAllBelow`, `internal/model/grounding.go:15`, `pkg/application/gather.go:170`) has one read instead, `every live brief, no search`.

### Groups, in the order they print

| # | Group | Rule that puts an entry there | Form | Line format and markers | Cap | Owed at the gate |
|---|---|---|---|---|---|---|
| 1 | `# relevant`: entries in full | Opened by `ids` (`gather.go:202-204`, `289`); stored verdict relevant, only with a subject entry (`gather.go:214`, `grounding_relevance.go:84`); score at or above the judge's relevant threshold, valid thresholds only (`model/prefilter.go:437`) | YAML envelope between `---` lines (id, type, kind, layer, title, confidence, intent, participants, topics, refs with kind and desc, closes, supersedes, decided-by, approved-by, reason, attachment names, preflight, grounding, verification, status, time; no summary), then `# body` and the body with headings demoted one level. No attachment content, no upstream or downstream chain (`presenters/show.go:68-86`, `126-164`; `gather.go:300` asks depth 0) | Over MCP one proof tag `⟦xxxx⟧ ` stands before one content word of the body (`model/anchor.go:21-23`, `176-181`; `workflow_pieces.go:159-168`). An entry the call proves it holds prints instead as `<id> — <title> (held as tag xxxx; next proof xxxx:<the word right after "w">)` (`presenters/repeats.go:120-128`). CLI: no tag, no pointer | 12 (`query.GatherFullCap`, `internal/query/gather.go:7`; `gather.go:293-298`) | A ref or a verdict for each (`grounding_gate.go:105-114`, `workflow_grounding.go:155-159`) |
| 2 | Overflow lines | Full entries past the cap | Sentence `+N opened entries past the full-serve cap of 12, open them with gather ids [<ids>]:` then one line each in the to-judge format, without `found by` (`presenters/gather.go:62-72`) | As group 3 | none | Nothing; counted as shown |
| 3 | `# to judge (N)`: lines | Every candidate not placed elsewhere: valid thresholds and score between set-aside and relevant; every unscored candidate; blind audit samples (`model/prefilter.go:439-441`, `calibration.go:366`). Candidates are each live entry a read found, once (`grounding.go:142`); searches list 20 hits each (`gather.go:53`, `371`), topic listings are whole (`gather.go:182-193`) | One line: title only | `- <id> (<kind> <layer> <type>) {status: <kind> [<by>]} — <title> <topic, topic> · found by semantic, keyword(<term>), topic(<label>)` (`presenters/gather.go:135-159`). Status braces absent for a done entry. No score printed, order is falling score, unscored after scored. No "not judged" marker; an audit sample is unmarked by design and inserted at a seeded position. Pointer form replaces everything before ` · found by` when the entry is held in full | None with valid thresholds. Without them: 100 scored (`GatherJudgeRanked`, `model/prefilter.go:398-401`), 200 for a weak judge, plus every unscored one (`prefilter.go:458-479`) | Nothing: unanswered lines become implicit dismissals at publish (`d-prc-cap` line 495) |
| 4 | Way-back sentence | Printed once when any pointer printed | `An entry marked held is not served again while you prove you hold it; to have it in full again, call show or gather with its id and its tag left out of held.` (`repeats.go:112-116`) | | | |
| 5 | `# listed by title (N)` | Only without valid thresholds: scored candidates ranked past group 3 (`prefilter.go:472-473`) | Fixed sentence (`presenters/gather.go:91`), then `- <id> — <title>` | No kind, status, topics or `found by` | 300 (`GatherTitleListed`) | Nothing; not counted as shown |
| 6 | Counted | Past the title ceiling | `+N more ranked lower, not listed` (`presenters/gather.go:95-97`) | A count, no IDs | none | Nothing |
| 7 | `# set aside` | Per reason (`grounding.go:129-136`): answered in this capture; actor and role records; not live; judged not relevant before; below the calibrated threshold less the 3 % audit sample (`calibration.go:17`, `32`); below the far floor (`far_floor.go:16-19`, `84-86`) | `- <reason>: <n>: <id>, <id>, ...` (`presenters/gather.go:100-107`) | Full ID list, no title | none | Nothing |

Paging (MCP only, `pkg/mcpapp/result_bound.go:16-49`, `119-125`): a text above 15,000 served chars is split into chunks of at most 14,500 at paragraph or line breaks; each page ends in `(page i of n of this gather result — call gather again with the same arguments and page i+1 ...)`. Nothing repeats per page; the header is on page 1 only. Full bodies count as read only after the last page served (`result_bound.go:74-89`).

Beside the gather result the capture's `widenReport` carries a seed block naming every read with every ID it surfaced (`internal/model/grounding_gate.go:205-223`).

## Part 1: the deep verifier's reading at its start

### Header and what frames it

- Page serve: lane `rules` (3,021 bytes, `internal/baseprocedures/entries/20261109-010000-d-prc-vfy.md` lines 83-105; the capture procedure dispatches it, `d-prc-cap` line 278), then lane `page`: `Page i of n. Report more for the next page. Findings are refused until every page is served.` and the page text (`workflow_deep.go:1025-1043`).
- Report schema: `findings`, a list of `deep-finding` (`internal/engine/schema.go:555-595`, about 1.5 KB of JSON by reading the code, not measured). The code reserves about 7,000 chars for session head, rules lane and schema on the first serve (`verifier_deep.go:36-40`).
- Page bounds: first page 8,500 served chars (`DeepFirstPageChars`), later pages 12,000 (`DeepPageChars`, `verifier_deep.go:34`, `40`). A lead or block larger than a page runs on with `(continued from the page before)` or `(<id>, continued)` (`verifier_deep.go:786-836`, `884-920`).
- Lead, page 1 (`workflow_deep.go:582-601`): `## Draft` with the draft document in a fence (one `field: <JSON>` line per reported field, then the body), `## Ref fit` (one block per ref), `## Already raised by pre-flight`, `## Changed since the last verdict` on a later exchange, `## Ranking` with `Ranking source: judge <name>, N entries.` or `mixed, N by judge ..., N by similarity to the draft, N in listed order.` (`preflight_light.go:523-553`).
- `## Pre-flight's rules`: the rubric pre-flight's model would have judged by, served when the verifier judges in its place (`workflow_deep.go:761-768`, `llmops/preflight.go:149`). It quotes the active contracts as entry blocks.

### Groups, in reading order

Input: `Listed` is every ID any credited gather read surfaced, in read order (`workflow_capture.go:383-400`); scores are those the gather stored for the same judge and draft text (`workflow_ranking.go:67`), completed by one more ranking pass of 45 s, then by embedding similarity for what stays unscored (`workflow_ranking.go:250-294`).

| # | Group | Rule | Form | Format and markers | Cap | Owed |
|---|---|---|---|---|---|---|
| 1 | Declared targets | Every ref, supersede, close, approve of the draft (`verifier_lanes.go:117-119`) | Entry in full: `---`, header, body (`preflight_light.go:243-246`) | Header: `ID: <id> \| kind: <kind> \| Status: <status>`, `Topics: ...`, one `Ref: <kind> <id> (<desc>)` per ref, `Summary: <whole summary>`, `Decided by`/`Approved by`/`Reason`/`Agent-reasoned via` lines where they apply, `Role in the reading: declared target` (`preflight_light.go:227-236`, `268-347`) | none | Nothing per entry |
| 2 | Read in full by the drafter | Every entry a credited gather served in full (`verifier_lanes.go:120-122`) | As 1 | Role `read in full`, or `set aside (code: <code>)` when the drafter gave a verdict | none | Nothing |
| 3 | Lifecycle neighbours | Listed entries that close or supersede a target or that a target closes or supersedes (`verifier_lanes.go:123-128`, `227-246`) | As 1 | Role `lifecycle neighbour of a target`; replaced by `implicitly dismissed` when gather showed the entry and no verdict answers it (`workflow_deep.go:689-693`) | none | Nothing |
| 4 | Top-ranked | The first 5 ranked entries that are scored and not `done` (`DeepTopRanked`, `verifier_lanes.go:21`, `200`); 10 for a weak judge. Unscored entries qualify only when 5 or fewer are ranked | As 1 | Role `top-ranked`, replaced as in 3 | 5 or 10 | Nothing |
| 5 | Given a verdict | Listed entries with a drafter verdict (`verifier_lanes.go:129-133`) | "Brief" block: `---`, the full header of 1 (topics, ref lines, whole summary), then the title (`preflight_light.go:247-257`). A closed entry whose closer is in the reading: first line and role only | Role `set aside (code: <code>)` | none | Nothing |
| 6 | `## Listed, not served (N)`: summary lines | One ref from a target, first 50 by rank (`DeepHopListed`, `verifier_lanes.go:154-167`); every ranked entry past the top when thresholds are valid, else the next 100 (`verifier_lanes.go:202`) | Fixed paragraph, then `<id>  <title>: <first summary sentence>` (`preflight_light.go:560-606`) | Group headings when any implicit or hidden line exists: `Shown to the capture and left unanswered (implicitly dismissed):`, `Below the prefilter's threshold:`, `Other listed entries:` | 50 for ref neighbours; none for ranked entries with valid thresholds, 100 (200) without | Nothing |
| 7 | Title lines | Without valid thresholds: ranked entries past the summary tier; with them: entries scored below the set-aside threshold, less the blind 3 % audit sample (`verifier_lanes.go:204-213`) | `<id>  <title>` | Under `Below the prefilter's threshold:` when hidden | 300 (`DeepTitleListed`) | Nothing |
| 8 | Counted | Ref neighbours past 50, title lines past 300 | `N more listed, open with search or show` | A count | none | Nothing |

Rank (`verifier_lanes.go:135-153`): `done` entries last, then scored before unscored (`rankLast`), falling score, then falling similarity, then listed order.

The verifier owes no answer per entry. Findings are refused until every page is served (`workflow_deep.go:1156-1163`); an empty findings list is valid.

## Part 2a: gather, three real runs

CLI, `--draft-file <entry body> --kind directive --topic <each topic>`, no session, run in parallel. The draft's own entry is live and is one of the candidates in each run.
| Draft | Refs | Topics (listing size) | Reads | Candidates | In full | Lines to judge | Title list | Counted | Set aside | Bytes: header / lines / total | Served chars | Pages | Wall |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| `20261110-021124-d-tac-non`, narrow | 3 | cli/sync (18) | semantic 20, keywords 7+0+1+0+4+8, listing 18 | 35 | 0 | 35 | 0 | 0 | 0 | 636 / 6,367 / 7,004 | 7,398 | 1 | 56.1 s |
| `20261120-192504-d-tac-s5l`, broad | 5 | implementation/engine (144), implementation/mcp (56), reliability/pre-flight (62) | semantic 20, keywords 9+0+0+0+0+0, listings 144+56+62 | 249 | 0 | 249 | 0 | 0 | 0 | 825 / 50,553 / 51,379 | 54,129 | 4 | 58.8 s |
| `20261110-083100-d-tac-f48`, few refs | 1 | cli/config (35), collaboration/concurrent-work (39) | semantic 20, keywords 8+4+1+4+7+7, listings 35+39 | 93 | 0 | 93 | 0 | 0 | 0 | 693 / 18,724 / 19,418 | 20,451 | 2 | 55.7 s |

`prefilter:` note, the same shape in all three: `failed: judge chat:claude-haiku-5-5 scored 0 of 249 pairs; lines to judge run by falling score, 249 unanswered and left to judge (context deadline exceeded; claude -p: context deadline exceeded)`. 45 s of each run is the judge's deadline, 9 to 13 s the index refresh.

To-judge line: median 178 to 197 bytes, 90th percentile 231 to 255. `found by` tags are 1,387, 10,936 and 4,032 bytes, 21 to 22 % of the lines. 214 of the 249 lines of the broad draft carry one tag, a topic listing.

Not measured: the split a working judge gives (full, set-aside lists). By the code a scored candidate below 0.078 would move to an ID list (27 bytes each) and one at or above 0.349 to full (median 2,161 bytes, cap 12); every other stays a line, uncapped.

Real gather calls of today's four captures in this repository (session logs): 9 calls, 24 to 174 lines shown per call, 1 to 7 entries in full, union of reads 27 to 179. One call got fresh judge scores (16); eight got none. One capture called gather three times and was shown 162, 163 and 174 lines.

## Part 2b: the verifier's reading

A throwaway program mirrors `deepReading` over the exported pieces (`PlaceVerifierReading`, `DeepReadingBlock`, `DeepListedBlock`, `DeepRankingNote`, `PreflightRubric`, `DeepReadingPages`). Input per draft: targets from the entry, `Listed` from the gather run above without the draft's own entry, no scores (the judge returned none), thresholds valid as the status reports, nothing read in full by the drafter.
| Draft | Pages | Bytes | Lead | Pre-flight's rules | Targets in full | Lifecycle in full | Top-ranked in full | Verdict briefs | Summary lines | Title lines | Counted |
|---|---|---|---|---|---|---|---|---|---|---|---|
| non, no verdicts | 8 | 73,862 | 3,355 | 34,953 | 3 / 21,886 | 1 / 3,274 | 0 | 0 | 32 / 10,368 | 0 | 0 |
| s5l, no verdicts | 15 | 155,355 | 6,729 | 37,044 | 5 / 20,556 | 0 | 0 | 0 | 246 / 90,599 | 0 | 0 |
| f48, no verdicts | 9 | 82,820 | 2,516 | 34,953 | 1 / 9,860 | 1 / 3,263 | 0 | 0 | 92 / 32,114 | 0 | 0 |
| non, a verdict on every line | 10 | 94,629 | 3,249 | 34,953 | 3 / 21,886 | 1 (in the briefs' role) | 0 | 33 / 34,519 | 0 | 0 | 0 |
| s5l, a verdict on every line | 36 | 385,458 | 6,622 | 37,044 | 5 / 20,556 | 0 | 0 | 246 / 321,306 | 0 | 0 | 0 |
| f48, a verdict on every line | 16 | 161,473 | 2,410 | 34,953 | 1 / 9,860 | 1 (in the briefs' role) | 0 | 93 / 114,280 | 0 | 0 | 0 |

The second scenario is an upper bound, not observed behaviour. Ref neighbours: 5, 9 and 2 of the summary lines. Not measured for these drafts: the top five in full and the hidden title group, which need judge scores.

Real readings `prepareDeep` stored today (session logs of this repository, six runs in four captures):

| Session, run | Listed | Pages | Bytes | Lead | Pre-flight's rules | Full blocks | Listed block: lines / bytes | Ranking source |
|---|---|---|---|---|---|---|---|---|
| 204033 | 47 + 14 full | 12 | 111,989 | 14,031 | 35,365 | 14 / 48,584 | 47 / 13,932 (3 hidden title lines) | 13 judge, 6 similarity, 33 listed order |
| 204027 first | 161 | 14 | 134,761 | 14,583 | 35,365 | 6 / 24,367 | 161 / 60,113 | 17 similarity, 144 listed order |
| 204027 second | 175 | 16 | 159,405 | 18,987 | 35,365 | 8 / 39,835 | 175 / 64,849 | 29 similarity, 146 listed order |
| 204023 | 47 | 12 | 111,123 | 16,771 | 35,365 | 9 / 42,174 | 47 / 16,736 | 9 similarity, 38 listed order |
| 204209 first | 24 | 7 | 69,342 | 15,342 | 35,365 | 3 / 10,434 | 24 / 8,179 | 1 similarity, 23 listed order |
| 204209 second | 30 | 9 | 88,386 | 18,584 | 35,365 | 4 / 24,318 | 30 / 10,061 | 1 similarity, 29 listed order |

The revision readings a live verifier got after a changed draft: 5 pages / 40,366 bytes and 4 pages / 35,274 bytes.

## Part 2c: sizes over the whole graph

Bytes. All 1,873 entries; the live 1,044 differ by a few percent.

| Form | Median | 90th percentile | Mean | Max |
|---|---|---|---|---|
| Entry body alone | 1,534 | 3,815 | 1,990 | 39,343 |
| Gather: entry in full (envelope, `# body`, body) | 2,169 | 4,870 | 2,683 | 62,639 |
| Verifier: entry in full (`---`, header with summary, body) | 2,586 | 5,337 | 3,079 | 40,098 |
| Verifier: "brief" block of a verdict entry | 1,024 | 1,748 | 1,138 | 5,527 |
| Gather: line to judge, without `found by` | 143 | 199 | 149 | 348 |
| Gather: line to judge with `found by`, the three runs | 178 to 197 | 231 to 255 | | 339 |
| Verifier: title plus first summary sentence | 320 | 440 | 328 | 978 |
| Gather: title list line | 61 | 79 | 65 | 302 |
| Verifier: title line | 56 | 74 | 60 | 297 |

## Surprises

1. A calibrated judge lifts the tier caps in both places. Gather's 100 and 300 hold only "while its judge holds no recall stage" (`model/prefilter.go:458`); with valid thresholds every candidate between the two thresholds is a line. The verifier lists every ranked entry past the top with its summary sentence once thresholds are valid (`verifier_lanes.go:202`); the 300 ceiling then covers only hidden entries. This judge is calibrated, so neither list has a cap here.
2. A judge that answers nothing leaves every candidate as a line: 249 lines, 51 KB, 4 pages for the broad draft. All 377 judge calls of the three runs timed out. With batch size 1 and 22 to 41 s per call, 16 calls at once score at most a few dozen pairs in 45 s.
3. The note still says `lines to judge run by falling score` when 0 pairs were scored.
4. Every gather call inside a capture runs every read again and prints every line again, also a call that only opens one entry by `ids`. Pointers replace only entries held in full.
5. Topic listings are whole and match by prefix: one broad topic put 144 lines into the result. They are 214 of the 249 lines.
6. A line to judge carries no summary sentence, yet repeats what the ID says (`tactical signal` beside `s-tac`) and a `found by` tail that is a fifth of the list.
7. No marker tells a scored line from an unscored one, and no score is printed.
8. IDs repeat without titles: each set-aside group prints its whole ID list, and the `widenReport` seed names every read with every ID (a 144-entry listing is 3.9 KB there).
9. The overflow sentence calls entries `opened` that the score served in full (`presenters/gather.go:68`).
10. The verifier's "brief" block is not brief: full header with every ref line and the whole summary, 1,024 bytes median, three times a summary line, uncapped. A verdict on a line costs the verifier's reading about 1.0 to 1.3 KB; leaving the line unanswered costs about 330 bytes.
11. `Pre-flight's rules` is 35 KB in every reading here, three to four pages, of which about 10 KB are active contracts quoted as entry blocks. In three of the six real readings it is larger than the entries served in full.
12. The role line `top-ranked` or `lifecycle neighbour of a target` is replaced by `implicitly dismissed` for any entry gather showed (`workflow_deep.go:691`). In the real readings nearly every listed line sits under the one heading `Shown to the capture and left unanswered`, so the grouping tells nothing.
13. The far floor exists only in gather. `PlaceVerifierReading` hides by threshold alone, so a far-floor set-aside is an ordinary ranked entry for the verifier.
14. An unscored entry is never served in full as top-ranked unless five or fewer are ranked (`verifier_lanes.go:200`). When the judge fails, the verifier gets no full entry beyond targets, drafter reads and lifecycle neighbours.
15. The lead alone passes the first page: real leads are 14 to 19 KB against a first-page bound of 8,500.
16. The two "in full" forms differ: gather's has the YAML envelope and no summary, the verifier's has the summary and no envelope.

## Caveats

- The judge is run through `claude -p`; the 377 timed-out calls record 0 tokens in `.sdd/stats/llm.jsonl`, so their cost is unknown and may exceed the 1 USD allowance. The three query-writer calls took 2,438 tokens on `glm-5.3-flash`.
- The verifier numbers for the three drafts are a simulation over exported functions, not a run of `deepReading`. The six real readings above are its check: same structure, 300 to 375 bytes per listed line in both.
- The report schema and the session head of the verifier's first serve were not measured.
