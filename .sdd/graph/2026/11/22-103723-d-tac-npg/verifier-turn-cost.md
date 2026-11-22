# Deep-verifier runs: token cost per turn

Source: the transcripts of 15 deep-verifier runs on this repository (Claude Code `sdd_delegate` subagents, model `claude-opus-5-5`, dated 2026-11-20 to 2026-11-22), the newest complete runs that call `next`, with the per-turn `usage` the host records. Runs are numbered R01 (newest) to R15. Per-turn tables are left out.


A turn is one API response (one message id). `in` = fresh input, `cw` = cache creation, `cr` = cache read, `out` = output, all in tokens. A tool result is counted at the turn that called it and is first read by the following turn (`recv`). A rules page is a result holding the heading `## Pre-flight's rules`.


## All runs

| run | date | turns | `next` calls | results | in | cw | cr | out | cr share | final prompt | rules pages | rules bytes | recv first | turns after first | turns after last | carried rules (byte-turns) | carried other (byte-turns) |
|---|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| R01 | 2026-11-22 | 45 | 33 | 53 | 92 | 217313 | 5335688 | 26117 | 96.1 % | 217315 | 3 | 42597 | 4 | 41 | 39 | 1704077 | 8756898 |
| R02 | 2026-11-21 | 38 | 33 | 41 | 76 | 185355 | 4023576 | 15373 | 95.6 % | 198500 | 3 | 43418 | 6 | 32 | 30 | 1347111 | 6876406 |
| R03 | 2026-11-21 | 21 | 16 | 35 | 42 | 127308 | 1695662 | 12874 | 93.0 % | 140453 | 3 | 43040 | 5 | 16 | 14 | 646299 | 2420071 |
| R04 | 2026-11-21 | 30 | 20 | 44 | 60 | 153944 | 2923884 | 17919 | 95.0 % | 167089 | 3 | 40000 | 5 | 25 | 23 | 963742 | 4661157 |
| R05 | 2026-11-21 | 34 | 21 | 42 | 68 | 150022 | 3072754 | 16875 | 95.3 % | 163167 | 3 | 40592 | 4 | 30 | 28 | 1178863 | 4889814 |
| R06 | 2026-11-21 | 18 | 15 | 28 | 36 | 105487 | 1279494 | 14287 | 92.4 % | 118632 | 3 | 38396 | 5 | 13 | 11 | 463578 | 1721505 |
| R07 | 2026-11-21 | 23 | 12 | 30 | 46 | 113513 | 1517676 | 13253 | 93.0 % | 113515 | 3 | 42467 | 3 | 20 | 18 | 807342 | 1637888 |
| R08 | 2026-11-21 | 48 | 45 | 50 | 96 | 232561 | 5790629 | 22730 | 96.1 % | 232563 | 3 | 35619 | 7 | 41 | 37 | 1406643 | 10834398 |
| R09 | 2026-11-21 | 39 | 29 | 42 | 78 | 181301 | 4074957 | 20976 | 95.7 % | 194446 | 4 | 47712 | 4 | 35 | 32 | 1611762 | 6751223 |
| R10 | 2026-11-21 | 38 | 27 | 39 | 76 | 151636 | 3860442 | 17825 | 96.2 % | 164781 | 4 | 46730 | 3 | 35 | 32 | 1578541 | 6374071 |
| R11 | 2026-11-21 | 81 | 70 | 83 | 162 | 383074 | 16532931 | 42330 | 97.7 % | 383076 | 3 | 34711 | 9 | 72 | 69 | 2457213 | 33847394 |
| R12 | 2026-11-21 | 50 | 42 | 50 | 100 | 219154 | 6099749 | 17633 | 96.5 % | 219156 | 3 | 34428 | 9 | 41 | 38 | 1382146 | 11419072 |
| R13 | 2026-11-21 | 27 | 22 | 30 | 56 | 127291 | 2069241 | 18999 | 94.2 % | 140436 | 3 | 38859 | 4 | 23 | 21 | 859073 | 2917652 |
| R14 | 2026-11-21 | 68 | 50 | 70 | 138 | 292887 | 10768226 | 38266 | 97.4 % | 306032 | 4 | 47906 | 4 | 64 | 61 | 3007569 | 19702593 |
| R15 | 2026-11-21 | 63 | 42 | 74 | 128 | 373126 | 8229512 | 32148 | 95.7 % | 252015 | 4 | 47695 | 4 | 59 | 56 | 2756135 | 13855808 |

### Medians and ranges (15 runs)

| measure | median (min to max) |
|---|---|
| assistant turns | 38 (18 to 81) |
| `next` calls | 29 (12 to 70) |
| fresh input tokens | 76 (36 to 162) |
| cache-creation tokens | 181301 (105487 to 383074) |
| cache-read tokens | 4023576 (1279494 to 16532931) |
| output tokens | 17919 (12874 to 42330) |
| prompt size at the last turn (tokens) | 194446 (113515 to 383076) |
| cache-read share of input-side tokens, per run | 95.7 (92.4 to 97.7) % |
| cache-read share, pooled over runs | 96.2 % |
| input-side cost per run (fresh-input-token equivalents) | 634127 (259844 to 2132298) |
| cache writes on the 1h TTL (tokens, sum over runs) | 0 |
| turns that re-wrote the prefix after a cache lapse, per run | 0 (0 to 1) |
| findings reports (`SubagentHandback` calls) per transcript | 1 (1 to 2) |
| transcripts holding more than one report (run resumed for another round; rules pages not served again) | 4: R01, R13, R14, R15 |
| assistant turns, single-report transcripts only (11) | 38 (18 to 81) |
| input-side cost, single-report transcripts only (FTE) | 575665 (259844 to 2132298) |
| bytes of a `next` result that is not a rules page, median per run | 12530 (10504 to 13727) |

### Rules pages (15 of 15 runs hold them)

| measure | median (min to max) |
|---|---|
| rules pages per run | 3 (3 to 4) |
| rules bytes per run | 42467 (34428 to 47906) |
| rules tokens per run (bytes / 2.87) | 14789 (11990 to 16684) |
| turn that first receives a rules page | 4 (3 to 9) |
| turn that receives the last rules page | 7 (5 to 12) |
| turns after the first receipt | 35 (13 to 72) |
| turns after the last receipt | 32 (11 to 69) |
| carried rules / carried other (byte-turns) | 0.207 (0.073 to 0.493) |
| rules bytes / all result bytes | 0.117 (0.040 to 0.205) |
| first receipt as share of the rules cost | 27.0 (15.0 to 50.9) % |
| rules cost as share of the run's input-side cost | 10.5 (4.7 to 15.9) % |
| (a) net saving as share of the run's input-side cost | -0.0 (-0.8 to 2.6) % |
| carried cost with cache lapses priced as re-writes (FTE) | 48134 (16144 to 115086) |

### Tokenizer ratio, derived

Least squares over 608 turn-to-turn steps of the measured runs (0 steps with a shrinking prompt dropped): growth of the prompt (in + cw + cr) = 93 + sdd page bytes / 2.67 + other result bytes / 2.47 + 0.94 x previous output tokens; R2 = 0.884. Unweighted ratio bytes / (growth - previous output): 2.60. Steps that received rules pages only: 42; bytes per token there, median (range): 2.87 (2.65 to 3.02). The rules estimates below use 2.87 bytes per token, the median of the rules-only steps. Results are JSON-escaped envelopes, which lowers the ratio against plain prose.

### Cost of the rules pages, estimated

Assumed price ratio, the public one for the model family: cache write 1.25 x and cache read 0.10 x fresh input (5-minute TTL). Unit: fresh-input-token equivalents (FTE). First receipt = 1.25 x rules tokens. Carried = 0.10 x rules tokens x turns after receipt. (a) rules served last, right before the findings report: the rules cost 1.25 x tokens plus the carry between the rules pages themselves; every step after a rules page moves one turn earlier per rules page before it, and its growth is carried that much longer (`displaced`), so the run's net saving is carried - carry between the rules pages - displaced. (b) rules absent: the whole rules cost goes; the turns that fetched them go too, with their re-read of the prefix (`prefix re-read`) and their output.

| run | rules tokens | first receipt | carried | total | share of run input cost | cache written at receiving turns (tokens) | (a) rules cost | (a) displaced | (a) net saving | (b) saving, rules only | (b) turns gone | (b) prefix re-read saved | (b) output tokens of those turns |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| R01 | 14835 | 18543 | 59345 | 77889 | 9.7 % | 16536 | 20034 | 50822 | 7033 | 77889 | 3 | 9605 | 1248 |
| R02 | 15121 | 18901 | 46914 | 65815 | 10.4 % | 16638 | 20453 | 42372 | 2990 | 65815 | 3 | 12372 | 1074 |
| R03 | 14989 | 18736 | 22508 | 41244 | 12.5 % | 27887 | 20259 | 20305 | 680 | 41244 | 3 | 14391 | 1828 |
| R04 | 13930 | 17413 | 33563 | 50976 | 10.5 % | 15279 | 18936 | 32130 | -90 | 50976 | 3 | 13567 | 1434 |
| R05 | 14136 | 17670 | 41055 | 58725 | 11.9 % | 15571 | 19143 | 35472 | 4110 | 58725 | 3 | 8948 | 1009 |
| R06 | 13372 | 16715 | 16144 | 32859 | 12.6 % | 22750 | 18150 | 15473 | -764 | 32859 | 3 | 13814 | 3523 |
| R07 | 14789 | 18487 | 28116 | 46603 | 15.9 % | 24256 | 19982 | 19047 | 7574 | 46603 | 3 | 8700 | 2355 |
| R08 | 12405 | 15506 | 48987 | 64493 | 7.4 % | 13725 | 17051 | 52640 | -5198 | 64493 | 3 | 13142 | 941 |
| R09 | 16616 | 20770 | 56131 | 76901 | 12.1 % | 18425 | 23729 | 58438 | -5267 | 76901 | 4 | 12275 | 1349 |
| R10 | 16274 | 20342 | 54974 | 75316 | 13.1 % | 18193 | 23239 | 48338 | 3739 | 75316 | 4 | 10600 | 1557 |
| R11 | 12088 | 15110 | 85574 | 100684 | 4.7 % | 13622 | 16712 | 93252 | -9279 | 100684 | 3 | 17707 | 1524 |
| R12 | 11990 | 14987 | 48134 | 63121 | 7.1 % | 13220 | 16536 | 46635 | -50 | 63121 | 3 | 15251 | 1141 |
| R13 | 13533 | 16916 | 29918 | 46834 | 12.8 % | 14963 | 18415 | 28703 | -284 | 46834 | 3 | 9093 | 1131 |
| R14 | 16684 | 20854 | 104740 | 125595 | 8.7 % | 18260 | 23825 | 102571 | -801 | 125595 | 4 | 12808 | 1089 |
| R15 | 16610 | 20763 | 95984 | 116747 | 9.1 % | 18375 | 23730 | 82199 | 10817 | 116747 | 4 | 11517 | 1531 |
| median | 14789 | 18487 | 48134 | 64493 | 10.5 % | 16638 | 19982 | 46635 | -50 | 64493 | 3 | 12372 | 1349 |
| min | 11990 | 14987 | 16144 | 32859 | 4.7 % | 13220 | 16536 | 15473 | -9279 | 32859 | 3 | 8700 | 941 |
| max | 16684 | 20854 | 104740 | 125595 | 15.9 % | 27887 | 23825 | 102571 | 10817 | 125595 | 4 | 17707 | 3523 |

# Turn anatomy

Same 15 runs as above (R01 newest). Price assumption as above: cache write 1.25 x, cache read 0.10 x fresh input; unit FTE.

## Turn classes

Markers. A `next` call turned a page when its result's JSON field `step` is `read` (452 results, 2030 to 17214 bytes, median 13257); it waited or polled when `step` is anything else (20 results, median 6832 bytes); it reported when the call's input holds `report.fields` (`findings` or `responses`). A turn with several tool calls takes the first class that fits, in the order report, page, wait. Page turns that called other tools in the same turn: 16. Carried cost of a turn = its `cache_read_input_tokens` x 0.10.

Turns per class:

| run | turns | page | wait | report | next-other | resume | reads | handback | none |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| R01 | 45 | 31 | 0 | 2 | 0 | 1 | 9 | 2 | 0 |
| R02 | 38 | 32 | 0 | 1 | 0 | 1 | 3 | 1 | 0 |
| R03 | 21 | 15 | 0 | 1 | 0 | 1 | 3 | 1 | 0 |
| R04 | 30 | 19 | 0 | 1 | 0 | 1 | 8 | 1 | 0 |
| R05 | 34 | 20 | 0 | 1 | 0 | 1 | 11 | 1 | 0 |
| R06 | 18 | 14 | 0 | 1 | 0 | 1 | 1 | 1 | 0 |
| R07 | 23 | 11 | 0 | 1 | 0 | 1 | 9 | 1 | 0 |
| R08 | 48 | 44 | 0 | 1 | 0 | 1 | 1 | 1 | 0 |
| R09 | 39 | 28 | 0 | 1 | 0 | 1 | 8 | 1 | 0 |
| R10 | 38 | 25 | 0 | 1 | 1 | 1 | 9 | 1 | 0 |
| R11 | 81 | 69 | 0 | 1 | 0 | 1 | 9 | 1 | 0 |
| R12 | 50 | 41 | 0 | 1 | 0 | 1 | 6 | 1 | 0 |
| R13 | 27 | 18 | 1 | 3 | 0 | 1 | 2 | 2 | 0 |
| R14 | 68 | 46 | 0 | 3 | 1 | 1 | 15 | 2 | 0 |
| R15 | 63 | 39 | 0 | 2 | 1 | 1 | 18 | 2 | 0 |

Carried cost per class (FTE):

| run | page | wait | report | next-other | resume | reads | handback | none | all turns |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| R01 | 342404 | 0 | 35776 | 0 | 0 | 118721 | 36668 | 0 | 533569 |
| R02 | 309155 | 0 | 18967 | 0 | 1314 | 53532 | 19389 | 0 | 402358 |
| R03 | 105338 | 0 | 12929 | 0 | 1314 | 36404 | 13581 | 0 | 169566 |
| R04 | 155357 | 0 | 16136 | 0 | 1314 | 103333 | 16249 | 0 | 292388 |
| R05 | 129351 | 0 | 15248 | 0 | 1314 | 145542 | 15820 | 0 | 307275 |
| R06 | 93151 | 0 | 11217 | 0 | 1314 | 10877 | 11390 | 0 | 127949 |
| R07 | 70172 | 0 | 10779 | 0 | 0 | 59971 | 10845 | 0 | 151768 |
| R08 | 511854 | 0 | 22398 | 0 | 0 | 22165 | 22646 | 0 | 579063 |
| R09 | 235335 | 0 | 18401 | 0 | 1314 | 133584 | 18861 | 0 | 407496 |
| R10 | 200281 | 0 | 15842 | 15709 | 1314 | 136786 | 16112 | 0 | 386044 |
| R11 | 1253972 | 0 | 37109 | 0 | 0 | 325015 | 37197 | 0 | 1653293 |
| R12 | 441986 | 0 | 21240 | 0 | 0 | 125426 | 21324 | 0 | 609975 |
| R13 | 107615 | 13288 | 36922 | 0 | 1314 | 21242 | 26543 | 0 | 206924 |
| R14 | 690245 | 0 | 66038 | 17851 | 1314 | 252434 | 48940 | 0 | 1076823 |
| R15 | 503271 | 0 | 38428 | 13696 | 1314 | 227276 | 38966 | 0 | 822951 |

Medians over runs (min to max):

| class | turns per run | carried cost per run (FTE) | share of the run's carried cost | output tokens per run | output tokens per turn, all runs pooled |
|---|---|---|---|---|---|
| `next` that turned a page | 28 (11 to 69) | 235335 (70172 to 1253972) | 62.1 (42.1 to 88.4) % | 9672 (6019 to 34031) | 372 (150 to 1840) |
| `next` that waited or polled | 0 (0 to 1) | 0 (0 to 13288) | 0.0 (0.0 to 6.4) % | 0 (0 to 164) | 164 (164 to 164) |
| `next` that reported findings or a revision | 1 (1 to 3) | 18967 (10779 to 66038) | 5.0 (2.2 to 17.8) % | 2940 (1593 to 9557) | 2768 (366 to 7191) |
| `next`, error or unparsed result | 0 (0 to 1) | 0 (0 to 17851) | 0.0 (0.0 to 4.1) % | 0 (0 to 8666) | 4262 (2670 to 8666) |
| `resume_session` | 1 (1 to 1) | 1314 (0 to 1314) | 0.3 (0.0 to 1.0) % | 85 (81 to 90) | 85 (81 to 90) |
| `show` and other read tools | 8 (1 to 18) | 118721 (10877 to 325015) | 22.3 (3.8 to 47.4) % | 4436 (1341 to 9242) | 377 (78 to 2998) |
| `SubagentHandback` (report to the caller) | 1 (1 to 2) | 19389 (10845 to 48940) | 4.8 (2.2 to 12.8) % | 60 (60 to 157) | 60 (60 to 97) |
| no tool call | 0 (0 to 0) | 0 (0 to 0) | 0.0 (0.0 to 0.0) % | 0 (0 to 0) | n/a |
| all | 38 (18 to 81) | 402358 (127949 to 1653293) | 100 % | 17919 (12874 to 42330) | 367 (60 to 8666) |

Output tokens by class, all runs pooled: page 56.2 %; wait 0.1 %; report 17.0 %; next-other 4.8 %; resume 0.4 %; reads 21.3 %; handback 0.4 %; none 0.0 %.

`next` turns with an error or unparsed result:

| run | turn | call input bytes | result bytes | output tokens | class of the next turn |
|---|---:|---:|---:|---:|---|
| R10 | 36 | 4071 | 68 | 2670 | report |
| R14 | 42 | 9868 | 68 | 8666 | report |
| R15 | 36 | 6373 | 68 | 4262 | report |

## Page results: page text against envelope

452 page results. Bytes are those of the JSON-encoded field with its key. The page text sits in `instructions`; every other field is envelope.

| field | present in | bytes median (min to max) | share of all page-result bytes |
|---|---:|---|---:|
| `instructions` | 452 | 11368 (577 to 14764) | 82.19 % |
| `report_schema` | 452 | 775 (724 to 2033) | 10.31 % |
| `framing` | 326 | 642 (133 to 973) | 3.42 % |
| `pending_chooser` | 452 | 145 (145 to 145) | 1.23 % |
| `goal` | 452 | 73 (73 to 73) | 0.62 % |
| `session` | 452 | 63 (63 to 63) | 0.53 % |
| `unproved` | 75 | 304 (292 to 332) | 0.43 % |
| `project` | 452 | 39 (39 to 39) | 0.33 % |
| `execution` | 452 | 29 (29 to 29) | 0.25 % |
| `procedure` | 452 | 21 (21 to 21) | 0.18 % |
| `status` | 452 | 19 (19 to 19) | 0.16 % |
| `instance` | 452 | 17 (17 to 17) | 0.14 % |
| `step` | 452 | 14 (14 to 14) | 0.12 % |
| `proof_notice` | 16 | 265 (263 to 357) | 0.08 % |
| (JSON braces, rest) | 452 | 1 (1 to 1) | 0.01 % |
| whole result | 452 | 13257 (2030 to 17214) | 100 % |

| measure | value |
|---|---|
| `instructions` bytes per page result | 11352 (561 to 14748) |
| envelope bytes per page result (result minus `instructions`) | 1978 (1161 to 3747) |
| envelope share, pooled | 17.9 % |
| envelope share per result | 17.3 (7.3 to 79.4) % |
| bytes before the rules heading inside `instructions`, rules pages (49) | 95 (95 to 3126) |
| characters two consecutive pages' `instructions` share at the start (437 pairs) | 0 (0 to 10) |
| characters two consecutive pages' `instructions` share at the end | 242 (0 to 439) |
| header and trailer inside `instructions`, estimate from the two rows above (bytes per page) | 337 |
| envelope share, pooled, with that header and trailer counted as envelope | 20.8 % |
| `report_schema` + `framing` + `pending_chooser`, repeated on every page: bytes per run | 48448 (19393 to 112165) |
| page calls per run | 28 (11 to 69) |
| page-result bytes per run | 326573 (136841 to 796883) |

## Context growth

Prompt size = in + cw + cr of the turn, in tokens. The first user message is 686 (686 to 686) bytes, so turn 1 is system prompt, tool definitions and injected context almost entirely.

| run | turn 1 | of it read from cache | after the last page of the first reading | at the first findings report (turn) | at the last turn | reports |
|---|---:|---:|---:|---:|---:|---:|
| R01 | 15600 | 0 | 122210 | 151678 (30) | 217315 | 2 |
| R02 | 15594 | 13143 | 176996 | 193895 (37) | 198500 | 1 |
| R03 | 15599 | 13143 | 122644 | 135815 (20) | 140453 | 1 |
| R04 | 15596 | 13143 | 139441 | 162495 (29) | 167089 | 1 |
| R05 | 15594 | 13143 | 122058 | 158199 (33) | 163167 | 1 |
| R06 | 15592 | 13143 | 112170 | 113904 (17) | 118632 | 1 |
| R07 | 15591 | 0 | 103483 | 108450 (22) | 113515 | 1 |
| R08 | 15595 | 0 | 223982 | 226461 (47) | 232563 | 1 |
| R09 | 15596 | 13143 | 154638 | 188612 (38) | 194446 | 1 |
| R10 | 15595 | 13143 | 148224 | 161126 (37) | 164781 | 1 |
| R11 | 15594 | 0 | 350661 | 371975 (80) | 383076 | 1 |
| R12 | 15595 | 0 | 207938 | 213238 (49) | 219156 | 1 |
| R13 | 15600 | 13143 | 107897 | 121203 (22) | 140436 | 3 |
| R14 | 15594 | 13143 | 149963 | 188621 (43) | 306032 | 3 |
| R15 | 15595 | 13143 | 106197 | 141984 (37) | 252015 | 2 |
| median | 15595 | 13143 | 139441 | 161126 (37) | 194446 | 1 |

Turn 1 prompt: 15595 (15591 to 15600) tokens. Carried over all turns of a run (turn 1 prompt x 0.10 x following turns): 57702 (26506 to 124752) FTE, 9.3 (5.9 to 11.7) % of the run's input-side cost.

## What-if: each reading in k calls

Model: a turn costs 0.10 x the prompt it re-reads + 1.25 x what the turn before added (its output and its results). Today, modelled this way, against the measured cost: ratio 1.000 (0.880 to 1.000). What-if: within each reading (the turns up to a findings report; a resumed transcript has more than one), the page turns become k turns at the place of the first one; together they add the same tokens as the measured page turns (page results and the output of those turns, split evenly); every other turn stays as measured, in order. This assumes the verifier needs no more thinking turns, and no more output, when pages are larger. It also ignores any limit on the size of one tool result.

| k | turns left | input-side cost (FTE) | saving against today (FTE) | saving (%) | largest reading: tokens per call |
|---:|---|---|---|---|---|
| today | 38 (18 to 81) | 634061 (259814 to 2132154) | - | - | - |
| 1 | 12 (5 to 26) | 357986 (159945 to 847348) | 246081 (47736 to 1284805) | 38.8 (16.3 to 61.0) | 105487 (61977 to 328719) |
| 2 | 13 (6 to 28) | 365383 (166856 to 865978) | 237065 (42060 to 1266175) | 37.4 (14.3 to 59.5) | 52744 (30988 to 164360) |
| 3 | 14 (7 to 30) | 372780 (173766 to 884609) | 228049 (36384 to 1247545) | 35.7 (12.4 to 58.5) | 35162 (20659 to 109573) |
| 4 | 15 (8 to 32) | 380177 (180676 to 903239) | 219033 (30708 to 1228915) | 32.5 (10.5 to 57.6) | 26372 (15494 to 82180) |

Per run:

| run | readings | page turns | today (FTE, modelled) | k=1 | k=2 | k=3 | k=4 | saving k=1 | saving k=4 |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| R01 | 2 | 31 | 805221 | 465319 | 491356 | 517393 | 543431 | 42.2 % | 32.5 % |
| R02 | 1 | 32 | 634061 | 311543 | 321573 | 331603 | 341633 | 50.9 % | 46.1 % |
| R03 | 1 | 15 | 328707 | 216084 | 223513 | 230942 | 238371 | 34.3 % | 27.5 % |
| R04 | 1 | 19 | 484826 | 333294 | 341169 | 349044 | 356919 | 31.3 % | 26.4 % |
| R05 | 1 | 20 | 494811 | 357986 | 365383 | 372780 | 380177 | 27.7 % | 23.2 % |
| R06 | 1 | 14 | 259814 | 159945 | 166856 | 173766 | 180676 | 38.4 % | 30.5 % |
| R07 | 1 | 11 | 293665 | 245930 | 251605 | 257281 | 262957 | 16.3 % | 10.5 % |
| R08 | 1 | 44 | 869776 | 339639 | 352004 | 364369 | 376734 | 61.0 % | 56.7 % |
| R09 | 1 | 28 | 634132 | 388051 | 397067 | 406083 | 415098 | 38.8 % | 34.5 % |
| R10 | 1 | 25 | 575599 | 365015 | 373713 | 382411 | 391110 | 36.6 % | 32.1 % |
| R11 | 1 | 69 | 2132154 | 847348 | 865978 | 884609 | 903239 | 60.3 % | 57.6 % |
| R12 | 1 | 41 | 883929 | 425646 | 437245 | 448844 | 460443 | 51.8 % | 47.9 % |
| R13 | 1 | 18 | 366045 | 252079 | 258746 | 265413 | 272081 | 31.1 % | 25.7 % |
| R14 | 2 | 46 | 1442947 | 753701 | 787105 | 820510 | 853915 | 47.8 % | 40.8 % |
| R15 | 2 | 39 | 1134979 | 617564 | 643886 | 670207 | 696529 | 45.6 % | 38.6 % |

## Time and cache lapses

Gap = time from one turn's first transcript line to the next turn's. A lapse is a turn whose cache read is under half the prompt of the turn before. Lapse cost = what the turn cost minus what it would have cost with the prefix read from cache.

| measure | value |
|---|---|
| gaps measured | 608 |
| gap in seconds: median, 90th percentile, maximum | 6.1, 12.2, 342 |
| gaps over 5 minutes, all runs | 1 |
| turns with a cache lapse, all runs | 1 |
| runs with a lapse | 1 of 15 |
| lapse cost, all runs (FTE) | 154392 |
| lapse cost as share of the input-side cost of all runs | 1.34 % |
| wall-clock per transcript, first to last turn (minutes) | 4.9 (2.5 to 13.2) |
| wall-clock to the first report to the caller (minutes) | 4.2 (2.5 to 10.2) |

Gaps over 5 minutes, lapses, and every gap after a report to the caller (the run sat idle until it was resumed):

| run | turn | gap (s) | class of the turn before | cache lapsed | lapse cost (FTE) | prompt before (tokens) |
|---|---:|---:|---|---|---:|---:|
| R01 | 32 | 195 | handback | no | 0 | 157138 |
| R13 | 25 | 61 | handback | no | 0 | 132879 |
| R14 | 46 | 297 | handback | no | 0 | 199918 |
| R15 | 39 | 342 | handback | yes | 154392 | 147401 |

## What-if: each reading paged at a cap per result

Same 15 runs and cost model as the k-call what-if. Within each reading the page turns become as many calls as the reading's page text needs at the cap, at the place of the first page turn; together they add the same tokens as the measured page turns, split evenly; every other turn stays as measured, in order. Page text = the bytes of the JSON-encoded `instructions` field of each page result. Two rules for the room a page leaves its text: (env) the cap minus the run's median measured envelope per page result (result bytes minus `instructions`); (code) the bounds sdd uses today, scaled: a page holds the cap minus 6,000, the first page the cap minus 9,500. The calls per reading never exceed the measured page turns. This assumes the verifier needs no more thinking turns, and no more output, when pages are larger.

Median envelope per page result, per run: 1927 (1739 to 2231) bytes. Page text per reading: 197414 (113741 to 640609) bytes. Measured page turns per reading: 20 (11 to 69).

| cap (chars) | room rule | calls per reading | turns per run | input-side cost (FTE) | saving against today (FTE) | saving (%) | largest call (tokens) |
|---:|---|---|---|---|---|---|---|
| today | - | 20 (11 to 69) | 38 (18 to 81) | 634061 (259814 to 2132154) | - | - | - |
| 18000 | env | 13 (8 to 41) | 28 (13 to 53) | 504188 (215228 to 1592554) | 101826 (8005 to 539599) | 16.2 (2.7 to 25.4) | 7662 (7205 to 10746) |
| 18000 | code | 18 (10 to 54) | 34 (16 to 66) | 572328 (235959 to 1834746) | 35124 (-3347 to 297407) | 7.5 (-1.1 to 13.9) | 5710 (5325 to 7439) |
| 45000 | env | 5 (3 to 15) | 18 (8 to 34) | 417205 (180676 to 1108170) | 191985 (36384 to 1023983) | 30.5 (12.4 to 48.2) | 20133 (17497 to 24178) |
| 45000 | code | 6 (4 to 17) | 18 (8 to 35) | 417205 (180676 to 1145431) | 191985 (30708 to 986723) | 30.5 (10.5 to 46.7) | 17581 (15494 to 24178) |

Saving of 45,000 against 18,000 under the same room rule (env), which cancels the packing error the 18,000 row shows against today: 17.3 (9.9 to 30.5) %.

Saving of 45,000 against 18,000 under the same room rule (code), which cancels the packing error the 18,000 row shows against today: 25.0 (11.5 to 39.0) %.

Per run:

| run | page turns | page text (bytes) | calls 18,000 env | calls 18,000 code | calls 45,000 env | calls 45,000 code | saving 45,000 env | saving 45,000 code |
|---|---:|---:|---|---|---|---|---:|---:|
| R01 | 31 | 311247 | 13+8 | 17+11 | 5+3 | 6+4 | 33.9 % | 30.7 % |
| R02 | 32 | 319201 | 20 | 27 | 8 | 9 | 39.8 % | 38.2 % |
| R03 | 15 | 141585 | 9 | 13 | 4 | 4 | 27.5 % | 27.5 % |
| R04 | 19 | 194172 | 13 | 17 | 5 | 6 | 24.8 % | 23.1 % |
| R05 | 20 | 204543 | 13 | 18 | 5 | 6 | 21.7 % | 20.2 % |
| R06 | 14 | 138091 | 9 | 12 | 4 | 4 | 30.5 % | 30.5 % |
| R07 | 11 | 113741 | 8 | 10 | 3 | 4 | 12.4 % | 10.5 % |
| R08 | 44 | 408641 | 26 | 35 | 10 | 11 | 48.2 % | 46.7 % |
| R09 | 28 | 268401 | 17 | 23 | 7 | 7 | 30.3 % | 30.3 % |
| R10 | 25 | 264463 | 17 | 23 | 7 | 7 | 27.5 % | 27.5 % |
| R11 | 69 | 640609 | 41 | 54 | 15 | 17 | 48.0 % | 46.3 % |
| R12 | 41 | 375722 | 24 | 32 | 9 | 10 | 41.3 % | 40.0 % |
| R13 | 18 | 165878 | 11 | 15 | 4 | 5 | 25.7 % | 23.8 % |
| R14 | 46 | 454934 | 17+12 | 23+16 | 7+5 | 7+5 | 37.3 % | 37.3 % |
| R15 | 39 | 368788 | 11+13 | 15+18 | 4+5 | 5+6 | 36.9 % | 34.6 % |
