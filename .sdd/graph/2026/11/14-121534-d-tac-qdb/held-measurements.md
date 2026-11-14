# Held-list sizing: measurements

All figures are characters (UTF-8 bytes for the live runs). A token is roughly 2.5 characters for held strings and 4 for prose.

## 1. Live runs over `sdd serve`

A script drives one capture over stdio MCP to its guide-review step (12 calls), then abandons the session; no entry is written. The agent policy keeps its newest N tags.

| Held policy | Held bytes per run | Result bytes per run | Mean total |
|---|---|---|---|
| every tag | 5,107 / 9,339 / 4,936 | 76,329 / 72,566 / 67,431 | 78,570 |
| newest 20 | 3,335 / 6,969 / 4,001 | 69,140 / 75,501 / 75,507 | 78,151 |
| newest 10 | 2,546 / 2,260 / 1,789 | 76,093 / 92,571 / 84,892 | 86,717 |
| none | 0 | 90,775 / 93,729 / 97,943 / 91,562 | 93,502 |

Run-to-run noise in results reaches about 17 KB, because gather writes different queries each run.

## 2. Transcript replay

Claude Code transcripts since 2026-11-12 from seven projects (anonymized, A to G) and one live capture (H): 58 agent contexts, 2,430 sdd calls carrying held proofs. A tag first seen in a result is a full serve; the same tag seen again is a pointer serve. A piece's size is estimated as its result's length divided by the new tags in that result.

### 2.1 How far back a pointer serve reaches

Rank of the re-served piece's tag among the tags held, newest first.

| Project | Pointer serves | Median | p90 | p95 | p99 | max |
|---|---|---|---|---|---|---|
| A | 1,295 | 15 | 39 | 47 | 57 | 70 |
| B | 592 | 14 | 29 | 34 | 40 | 43 |
| C | 338 | 7 | 21 | 25 | 36 | 51 |
| D | 196 | 7 | 20 | 25 | 32 | 35 |
| E | 344 | 7 | 14 | 16 | 27 | 37 |
| F | 241 | 10 | 38 | 41 | 45 | 49 |
| G | 1 | 0 | 0 | 0 | 0 | 0 |
| H (live capture) | 1,541 | 106 | 182 | 192 | 208 | 225 |
| all | 4,548 | 18 | 152 | 174 | 200 | 225 |

Held list items per call: median 19, p90 63, max 236.

### 2.2 Simulated policies, totals over all calls

| Policy | Held chars | Re-served in full (est.) | Missed pointer serves | Hint chars | Total |
|---|---|---|---|---|---|
| every tag | 1,179,288 | 0 | 0 of 4,548 | 0 | 1.18 M |
| newest 10 | 327,280 | 6,001,679 | 1,878 | 0 | 6.33 M |
| newest 20 | 547,845 | 4,440,105 | 1,253 | 0 | 4.99 M |
| newest 30 | 696,338 | 2,569,414 | 723 | 0 | 3.27 M |
| newest 40 | 783,972 | 1,371,275 | 379 | 0 | 2.16 M |
| server names 10 | 63,240 | 1,544,103 | 317 | 62,370 | 1.67 M |
| server names 20 | 67,490 | 125,862 | 30 | 64,092 | 0.26 M |

The server-named policies assume exact foresight of the next call's pointer serves; a real prediction misses more.

## 3. Tag challenge break-even

The same transcripts, read for the host's token usage at each sdd call (1,186 transcript files, 5,116 sdd calls). Serving an entry again in full costs its tokens once as a prompt-cache write (1.25 times the input price) and again on every later request of that context as a cache read (0.1 times). A tag challenge costs one extra request, which reads the whole cached context once (0.1 times its size), plus about 300 input and 60 output tokens. The break-even entry size at a call is therefore (0.1 × context + 675) / (1.25 + 0.1 × requests left), in tokens.

| At the sdd call | p10 | median | p90 |
|---|---|---|---|
| Context tokens | 115,816 | 254,603 | 421,318 |
| Requests left before the context ends or compacts | 9 | 65 | 174 |

sdd results measure 2.2 characters per token (median; p25 1.46, p75 2.46).

| Break-even | p10 | p25 | median | p75 | p90 |
|---|---|---|---|---|---|
| Tokens | 851 | 1,602 | 3,232 | 7,049 | 16,206 |
| Characters | 1,873 | 3,527 | 7,113 | 15,513 | 35,666 |

| Entry size | Calls where the challenge is cheaper | Graph entry files at least this size |
|---|---|---|
| 1,000 chars | 1.3% | |
| 2,000 chars | 11.2% | 73% |
| 4,000 chars | 29.6% | 29% |
| 8,000 chars | 54.4% | 7% |
| 16,000 chars | 75.6% | 1% |

Graph entry sizes are the files under this project's graph folder (2,083 files; median 2,861, p90 7,014, p99 19,377 characters). The model counts tokens only, not the latency of the extra request.
