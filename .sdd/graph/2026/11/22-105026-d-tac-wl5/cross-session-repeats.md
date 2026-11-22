# Cross-session repeats in sdd serves

## Data

- Transcripts: real Claude Code main-agent conversations with the sdd MCP server, from 2026-11-15 on, each holding at least two `start_session` calls. Subagent and delegate transcripts are left out.
- 1880 transcripts named `start_session`. Excluded: 1582 subagent transcripts, 32 delegate transcripts, 216 with fewer than two `start_session` calls, 32 older than the cutoff. 18 conversations remain, C01 (newest) to C18. Every qualifying conversation is counted.
- Sessions are the distinct session handles in `start_session` results. C15 made two `start_session` calls but got one handle, so it has no later session. That leaves 39 later sessions (session 2 and up) across 17 conversations.

## Method

- **Pieces.** Every sdd tool result is parsed as JSON, and nested serves are walked (`base_junction`, `open_instances`, `gather.serve`). Each piece's text is hashed after removing the tag markers (`⟦xxxx⟧` and one following space) and every session handle of that conversation.
  - `framing` is split into blocks at paragraphs starting with `## ` or `(held:`.
  - `instructions` is split into lanes at the paragraphs that open a capture `assemble` lane (`**Type-system overview.**`, `**Ground before you draft.**`, `**Lifecycle**`, ...) and at pointer lines. The type-system lane runs from `**Type-system overview.**` to the next lane opening. Other instructions have no served lane boundary and stay one segment.
  - `report_schema`: its `properties` block is one piece, with one sub-piece per property. The rest of the schema is a second piece.
  - `entries`, `results`, `serve` and `coverage` are kind "entries" (graph content). Every other field is "other".
- **Two hash levels.** `lane` hashes a whole block, lane or schema-properties block (strict). `para` hashes each paragraph inside it (lenient). Piece bytes are the JSON-escaped bytes the model sees.
- **Carried candidate.** A piece whose hash the same conversation received earlier under another session handle. It counts as **in context** if at least one earlier receipt has no compaction between it and the piece's own receipt.
- **Window.** The calls of the new session from `start_session` up to and including the first serve of the capture step `playback`, at most 15 sdd calls. The `end` column says what closed it: playback, cap, or sess-end (the session made fewer calls and no playback). **Life** covers all calls of the session.
- **API calls.** Distinct assistant messages of the main chain. Prompt size P = input tokens + cache creation tokens + cache read tokens. Calls with P=0 are skipped.
- **Bytes per token, derived.** From calls where exactly one sdd result of at least 4000 bytes lies between two consecutive API calls of the same model: result bytes / (P_next - P_prev - output_tokens_prev). Median **2.556 bytes/token** (n=1025, quartiles 2.46 / 2.56 / 2.67). Token figures below use 2.556.
- **Cost (assumed prices).** Each in-context carried piece of T tokens costs one cache write at 1.25×T plus a cache read at 0.1×T on every later API call until the next compaction or the end of the conversation, in fresh-input-token equivalents. No cache expiry or miss is modelled.
- **Compaction.** Marked by a compaction boundary line followed by a compaction summary. All 15 compactions found are automatic, in 7 of the 18 conversations (C01 2, C07 2, C13 1, C14 4, C16 4, C17 1, C18 1), each at about 968k tokens of context. No manual compaction appears. Two prompt-size drops without a marker (C07, C14) are model switches (a tokenizer change); distances across a switch are left out and counted separately.
- **Distance.** P at the API call that issued the new `start_session`, minus P at the call that first saw the earlier receipt. Computed for the first in-context receipt and for the nearest one. Medians are weighted by bytes.

## Summary

- **Sessions per conversation:** median 3, range 1 to 11. `resume_session` calls: C01 1, C14 3, C16 5, C18 1, all others 0.
- **First serve of a new session** (the `start_session` result): median 13,375 bytes, about 5.2k tokens (range 12,854 to 15,976).
- **Window:** median 92,940 bytes, about 36.4k tokens (range 14,663 to 167,879).
- **Carried-candidate share in the window, para level:** 62% of all bytes, 53% still in context. Per session: median 65% (43 to 86); in context, median 62% (0 to 86).

  | kind | carried | in context |
  |---|---|---|
  | framing | 90% | 77% |
  | type-system lane | 100% | 83% |
  | other instruction lanes | 71% | 60% |
  | report schema | 95% | 82% |
  | entries | 5% | 3% |
  | other | 41% | 35% |

  At lane level (strict): 52% carried, 44% in context; framing 88/76, type-system 86/70, other instructions 47/37, schema 87/76.
- **Cost:** in-context carried bytes in the window, median 50,080 per session (about 19.6k tokens). Median cost about 111k fresh-input-token equivalents per later session, 7.56M across the 39 sessions. API calls after a later session's first serve: median 126 (7 to 1687); up to the next compaction or end: median 73 (6 to 387).
- **Distance** from the nearest earlier receipt to the new session's start, byte-weighted:

  | p10 | p25 | median | p75 | p90 |
  |---|---|---|---|---|
  | 8.4k | 22k | 48.8k | 90k | 147k tokens |

  Half the carried bytes are within 50k tokens. The first receipt lies at a median of 258k tokens.
- **Compaction in between:** 303k window bytes (15% of carried) had every earlier receipt compacted away. In 4 later sessions (C13 s7, C14 s2, C14 s3, C18 s2) all earlier receipts were compacted away. 80k bytes spanned a model switch.

## What the data cannot show

- **Attention.** Whether the agent still attends to or recalls a piece served 50k to 500k tokens earlier. "In context" means only that no compaction removed it.
- **Cost.** Real cache behaviour (expiry, misses) makes the cost an estimate.
- **Version drift.** A hash miss can be a small edit, such as a count that changed. Para level softens this but can match short generic paragraphs by chance. Lane level is the strict bound.
- **Instruction lanes outside capture `assemble`.** They carry no served boundary and count as one segment each.
- **Delegates and subagents.** Their sessions are not counted.
- **Safety.** Whether a repeat could be left out safely.

## Conversations

| conv | sessions | resume_session | sdd calls | API calls | compactions | API index of each start_session |
|---|---|---|---|---|---|---|
| C01 | 2 | 1 | 52 | 982 | 2 | 330,855 |
| C02 | 2 | 0 | 6 | 189 | 0 | 30,115 |
| C03 | 2 | 0 | 31 | 117 | 0 | 16,64 |
| C04 | 4 | 0 | 52 | 280 | 0 | 32,142,203,211 |
| C05 | 4 | 0 | 79 | 301 | 0 | 43,107,187,270 |
| C06 | 3 | 0 | 86 | 421 | 0 | 17,244,342 |
| C07 | 4 | 0 | 268 | 864 | 2 | 10,374,380,667 |
| C08 | 2 | 0 | 32 | 173 | 0 | 47,145 |
| C09 | 2 | 0 | 4 | 24 | 0 | 3,13 |
| C10 | 4 | 0 | 46 | 105 | 0 | 17,74,81,89 |
| C11 | 3 | 0 | 62 | 202 | 0 | 24,126,146 |
| C12 | 3 | 0 | 116 | 571 | 0 | 27,183,488 |
| C13 | 11 | 0 | 228 | 867 | 1 | 47,233,324,508,557,586,643,670,735,785,831 |
| C14 | 3 | 3 | 223 | 2420 | 4 | 591,732,2353 |
| C15 | 1 | 0 | 85 | 229 | 0 | 31 |
| C16 | 2 | 5 | 517 | 1883 | 4 | 7,1328 |
| C17 | 2 | 0 | 160 | 776 | 1 | 422,768 |
| C18 | 3 | 1 | 155 | 872 | 1 | 33,640,700 |

## Later sessions, window, para level (bytes carried / bytes served, per kind)

| conv | sess | calls | end | bytes | tokens | framing | typesys | instr | schema | entries | other | carried bytes | carried % | in-context carried bytes | in-context % | in-context carried cost (fresh-input tok eq.) |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| C01 | 2 | 14 | playback | 93503 | 36577 | 11063/16206 | 7208/7208 | 15972/21917 | 26497/26497 | 88/11278 | 3225/9678 | 64053 | 69 | 42512 | 45 | 25093 |
| C02 | 2 | 3 | sess-end | 25963 | 10156 | 3646/6164 | 0/0 | 7932/7932 | 514/514 | 16/10598 | 374/374 | 12482 | 48 | 12482 | 48 | 40719 |
| C03 | 2 | 15 | cap | 105303 | 41193 | 10494/11694 | 7208/7208 | 13623/29038 | 29230/30131 | 425/20401 | 2351/6098 | 63331 | 60 | 63331 | 60 | 142370 |
| C04 | 2 | 12 | playback | 96185 | 37627 | 14043/14043 | 0/0 | 14480/28673 | 23143/23143 | 228/21259 | 3022/8057 | 54916 | 57 | 54916 | 57 | 303894 |
| C04 | 3 | 3 | sess-end | 27295 | 10678 | 6462/6798 | 0/0 | 7707/7707 | 514/514 | 0/0 | 370/11916 | 15053 | 55 | 15053 | 55 | 51352 |
| C04 | 4 | 2 | sess-end | 14663 | 5736 | 4483/5606 | 0/0 | 6490/7856 | 514/514 | 0/0 | 370/370 | 11857 | 81 | 11857 | 81 | 36861 |
| C05 | 2 | 13 | playback | 113378 | 44352 | 10379/12173 | 7785/7785 | 19010/31796 | 29692/29692 | 68/27995 | 2476/3805 | 69410 | 61 | 69410 | 61 | 539146 |
| C05 | 3 | 9 | playback | 94446 | 36946 | 10181/12494 | 7208/7208 | 13118/18407 | 20491/21237 | 237/33098 | 1885/1957 | 53120 | 56 | 53120 | 56 | 231725 |
| C05 | 4 | 6 | playback | 65862 | 25765 | 10094/10695 | 7208/7208 | 11702/13637 | 16014/16014 | 1160/17145 | 1049/1341 | 47227 | 72 | 47227 | 72 | 72830 |
| C06 | 2 | 13 | playback | 106634 | 41714 | 13986/15352 | 7495/7495 | 17493/29854 | 26011/27157 | 236/21146 | 3734/4809 | 68955 | 65 | 68955 | 65 | 460128 |
| C06 | 3 | 15 | playback | 127291 | 49795 | 14154/15534 | 7208/7208 | 18936/31574 | 34175/34175 | 443/33618 | 3382/5003 | 78298 | 62 | 78298 | 62 | 236054 |
| C07 | 2 | 5 | sess-end | 28829 | 11278 | 10110/10110 | 0/0 | 7707/7707 | 514/514 | 8/6845 | 370/3220 | 18709 | 65 | 18709 | 65 | 46359 |
| C07 | 3 | 13 | playback | 126109 | 49333 | 20220/21415 | 7208/7208 | 15778/24555 | 16319/19038 | 296/27939 | 2112/24901 | 61933 | 49 | 61933 | 49 | 86488 |
| C07 | 4 | 11 | playback | 94411 | 36933 | 15333/16540 | 7208/7208 | 14932/19627 | 20497/21311 | 240/25620 | 2262/3782 | 60472 | 64 | 58715 | 62 | 179011 |
| C08 | 2 | 6 | playback | 70522 | 27587 | 10026/11222 | 7208/7208 | 14444/18968 | 15602/17238 | 609/14001 | 1835/1835 | 49724 | 71 | 49724 | 71 | 70882 |
| C09 | 2 | 3 | sess-end | 19303 | 7551 | 5190/5190 | 0/0 | 9714/9714 | 398/398 | 0/2341 | 278/1291 | 15580 | 81 | 15580 | 81 | 13102 |
| C10 | 2 | 3 | sess-end | 28522 | 11158 | 9074/9074 | 0/0 | 7567/7567 | 514/514 | 0/0 | 370/10993 | 17525 | 61 | 17525 | 61 | 28068 |
| C10 | 3 | 3 | playback | 31076 | 12157 | 4936/6306 | 0/0 | 9303/10810 | 10821/13023 | 0/0 | 846/1158 | 25906 | 83 | 25906 | 83 | 34206 |
| C10 | 4 | 7 | playback | 46213 | 18078 | 14058/14058 | 0/0 | 11289/12965 | 12803/13825 | 8/2338 | 1631/2623 | 39789 | 86 | 39789 | 86 | 36247 |
| C11 | 2 | 7 | playback | 78602 | 30748 | 15180/15783 | 7498/7498 | 16444/20132 | 14389/15412 | 758/17459 | 1692/2090 | 55961 | 71 | 55961 | 71 | 184245 |
| C11 | 3 | 15 | cap | 120810 | 47260 | 9222/10334 | 7208/7208 | 15766/22832 | 19939/19939 | 109/51073 | 2704/8428 | 54948 | 45 | 54948 | 45 | 112745 |
| C12 | 2 | 7 | playback | 71266 | 27879 | 4970/4970 | 7498/7498 | 20850/25579 | 14389/16017 | 237/13857 | 2136/3012 | 50080 | 70 | 50080 | 70 | 777485 |
| C12 | 3 | 8 | playback | 76881 | 30075 | 4946/4946 | 7498/7498 | 20910/25378 | 14389/16017 | 3579/20414 | 2183/2262 | 53505 | 70 | 53505 | 70 | 190503 |
| C13 | 2 | 13 | playback | 97239 | 38039 | 4717/4717 | 7208/7208 | 23884/31695 | 19939/21167 | 72/27893 | 2999/3697 | 58819 | 60 | 58819 | 60 | 875306 |
| C13 | 3 | 12 | playback | 96112 | 37598 | 4717/4717 | 7498/7498 | 23388/32305 | 19939/20866 | 233/26432 | 2848/3546 | 58623 | 61 | 58623 | 61 | 665452 |
| C13 | 4 | 15 | cap | 113899 | 44556 | 19182/20379 | 14691/14691 | 19654/23086 | 23611/28418 | 112/21668 | 3057/4781 | 80307 | 71 | 80307 | 71 | 310431 |
| C13 | 5 | 7 | playback | 64765 | 25335 | 10207/11649 | 7208/7208 | 11566/14390 | 15602/16625 | 1259/12363 | 1642/2435 | 47484 | 73 | 47484 | 73 | 108128 |
| C13 | 6 | 5 | playback | 49633 | 19416 | 9703/11944 | 0/0 | 8462/15529 | 16806/18972 | 0/0 | 1822/3183 | 36793 | 74 | 36793 | 74 | 43199 |
| C13 | 7 | 8 | playback | 92940 | 36357 | 14736/15927 | 7208/7208 | 17220/24206 | 20907/21653 | 247/21676 | 2153/2189 | 62471 | 67 | 0 | 0 | 0 |
| C13 | 8 | 15 | cap | 126444 | 49464 | 20058/20058 | 7208/7208 | 19703/28473 | 29488/30353 | 9572/32976 | 3355/6650 | 89384 | 71 | 78164 | 62 | 609395 |
| C13 | 9 | 13 | playback | 123100 | 48155 | 13005/14026 | 7208/7208 | 16225/25701 | 30102/30102 | 589/41504 | 2476/4409 | 69605 | 57 | 69430 | 56 | 356901 |
| C13 | 10 | 7 | playback | 79606 | 31141 | 14898/16094 | 7208/7208 | 13609/18210 | 20907/21653 | 1567/13978 | 2189/2466 | 60378 | 76 | 60378 | 76 | 213986 |
| C13 | 11 | 12 | playback | 102271 | 40007 | 15069/15672 | 7208/7208 | 16267/19983 | 26761/26761 | 374/27885 | 3012/4285 | 68691 | 67 | 68203 | 67 | 111209 |
| C14 | 2 | 15 | cap | 167879 | 65673 | 22714/24362 | 15073/15073 | 20639/32602 | 18593/19038 | 3412/67697 | 1711/8229 | 82142 | 49 | 0 | 0 | 0 |
| C14 | 3 | 15 | cap | 147032 | 57517 | 12946/18916 | 7208/7208 | 18389/24787 | 23438/23857 | 378/64024 | 1377/8228 | 63736 | 43 | 0 | 0 | 0 |
| C16 | 2 | 7 | playback | 84654 | 33116 | 10908/12467 | 7208/7208 | 8275/19300 | 14617/15590 | 14351/28155 | 1080/1963 | 56439 | 67 | 56257 | 66 | 150183 |
| C17 | 2 | 3 | sess-end | 25334 | 9910 | 10184/10184 | 0/0 | 7593/7593 | 514/514 | 8/6280 | 374/374 | 18673 | 74 | 17282 | 68 | 12081 |
| C18 | 2 | 13 | playback | 108319 | 42373 | 6090/9674 | 7185/7185 | 22625/30085 | 19543/20470 | 60/35069 | 2871/4776 | 58374 | 54 | 0 | 0 | 0 |
| C18 | 3 | 8 | playback | 53413 | 20895 | 4822/4822 | 6742/7208 | 6586/11088 | 11425/12085 | 915/17162 | 786/1179 | 31276 | 59 | 31276 | 59 | 208316 |
| all | 39 | | | 3195707 | 1250128 | 426206/472315 | 218707/219173 | 565252/793258 | 659561/690958 | 41894/823187 | 74409/181393 | 1986029 | 62 | 1682552 | 53 | 7564099 |

Per-session carried % median 65 (range 43 to 86); in context median 62 (range 0 to 86).

## Totals at other scopes

| scope, hash level | bytes served | carried | in context | framing | typesys | instr | schema | entries | other | cost (fresh-input tok eq.) |
|---|---|---|---|---|---|---|---|---|---|---|
| window, lane | 3,195,707 | 52% | 44% | 88% / 76% | 86% / 70% | 47% / 37% | 87% / 76% | 0% / 0% | 41% / 35% | 6,404,358 |
| life, para | 8,819,897 | 61% | 34% | 80% / 42% | 100% / 53% | 66% / 40% | 93% / 51% | 10% / 3% | 33% / 18% | 12,283,619 |
| life, lane | 8,819,897 | 49% | 27% | 77% / 41% | 88% / 43% | 38% / 23% | 85% / 46% | 0% / 0% | 33% / 18% | 10,070,976 |

Per kind: carried / in context.

## Distance of carried candidates per later session (window, para level)

| conv | sess | first serve bytes | tokens | API calls after | API calls to next compaction or end | carried bytes compacted away | carried bytes across model switch | weighted median dist first receipt (tok) | weighted median dist nearest receipt (tok) | min..max dist nearest |
|---|---|---|---|---|---|---|---|---|---|---|
| C01 | 2 | 14094 | 5513 | 126 | 6 | 21541 | 0 | 405570 | 22418 | 11531..418460 |
| C02 | 2 | 14256 | 5577 | 73 | 73 | 0 | 0 | 127838 | 127838 | 117468..127838 |
| C03 | 2 | 13434 | 5255 | 52 | 52 | 0 | 0 | 100861 | 61321 | 50373..105330 |
| C04 | 2 | 13058 | 5108 | 137 | 137 | 0 | 0 | 161436 | 70886 | 61208..171694 |
| C04 | 3 | 13065 | 5111 | 76 | 76 | 0 | 0 | 277142 | 12031 | 4067..100414 |
| C04 | 4 | 13456 | 5264 | 68 | 68 | 0 | 0 | 291343 | 9449 | 3013..9449 |
| C05 | 2 | 13438 | 5257 | 193 | 193 | 0 | 0 | 129155 | 28084 | 12479..136546 |
| C05 | 3 | 13462 | 5266 | 113 | 113 | 0 | 0 | 254645 | 71575 | 62965..207078 |
| C05 | 4 | 13480 | 5273 | 30 | 30 | 0 | 0 | 428766 | 18637 | 5940..413631 |
| C06 | 2 | 13039 | 5101 | 176 | 176 | 0 | 0 | 321218 | 26311 | 3977..417300 |
| C06 | 3 | 13054 | 5107 | 78 | 78 | 0 | 0 | 480685 | 55943 | 42430..387257 |
| C07 | 2 | 13512 | 5286 | 489 | 53 | 0 | 18709 | - | - | - |
| C07 | 3 | 13512 | 5286 | 483 | 47 | 0 | 61773 | 8044 | 3890 | 3890..3890 |
| C07 | 4 | 13568 | 5308 | 196 | 70 | 1757 | 0 | 651226 | 131102 | 115744..483252 |
| C08 | 2 | 13059 | 5109 | 27 | 27 | 0 | 0 | 169520 | 95069 | 75223..174507 |
| C09 | 2 | 15768 | 6168 | 10 | 10 | 0 | 0 | 4838 | 4838 | 4838..4838 |
| C10 | 2 | 12854 | 5028 | 30 | 30 | 0 | 0 | 103299 | 7570 | 6900..103299 |
| C10 | 3 | 12854 | 5028 | 23 | 23 | 0 | 0 | 111848 | 10274 | 3419..58320 |
| C10 | 4 | 13005 | 5087 | 15 | 15 | 0 | 0 | 133370 | 2023 | 944..63313 |
| C11 | 2 | 13375 | 5232 | 75 | 75 | 0 | 0 | 116716 | 42600 | 29273..147269 |
| C11 | 3 | 13359 | 5226 | 55 | 55 | 0 | 0 | 166813 | 21145 | 8365..166813 |
| C12 | 2 | 13313 | 5208 | 387 | 387 | 0 | 0 | 272198 | 45654 | 18310..284333 |
| C12 | 3 | 13289 | 5199 | 82 | 82 | 0 | 0 | 578570 | 156003 | 135125..550994 |
| C13 | 2 | 13034 | 5099 | 633 | 373 | 0 | 0 | 251166 | 76408 | 20129..257721 |
| C13 | 3 | 13034 | 5099 | 542 | 282 | 0 | 0 | 394087 | 66390 | 48837..190176 |
| C13 | 4 | 13025 | 5095 | 358 | 98 | 0 | 0 | 605166 | 90634 | 72471..401255 |
| C13 | 5 | 13335 | 5217 | 309 | 49 | 0 | 0 | 723538 | 13581 | 4736..498861 |
| C13 | 6 | 13743 | 5376 | 280 | 20 | 0 | 0 | 774821 | 26225 | 16054..46166 |
| C13 | 7 | 13345 | 5220 | 223 | 223 | 62471 | 0 | - | - | - |
| C13 | 8 | 13386 | 5236 | 196 | 196 | 11220 | 0 | 79255 | 8354 | 660..89025 |
| C13 | 9 | 13376 | 5233 | 131 | 131 | 175 | 0 | 184691 | 36627 | 26722..140054 |
| C13 | 10 | 13399 | 5242 | 81 | 81 | 0 | 0 | 270109 | 19308 | 9287..249494 |
| C13 | 11 | 13374 | 5232 | 35 | 35 | 488 | 0 | 353233 | 48513 | 40002..327353 |
| C14 | 2 | 15762 | 6166 | 1687 | 372 | 82142 | 0 | - | - | - |
| C14 | 3 | 15336 | 5999 | 66 | 66 | 63736 | 0 | - | - | - |
| C16 | 2 | 15976 | 6250 | 554 | 59 | 182 | 0 | 527637 | 63752 | 5714..201793 |
| C17 | 2 | 13433 | 5255 | 7 | 7 | 1391 | 0 | 285348 | 54200 | 1979..85300 |
| C18 | 2 | 13039 | 5101 | 231 | 231 | 58374 | 0 | - | - | - |
| C18 | 3 | 13139 | 5140 | 171 | 171 | 0 | 0 | 98557 | 50995 | 26838..114333 |

All sessions, byte-weighted: nearest receipt median 48,837 tokens, first receipt 257,721; nearest-receipt p10 8,354, p25 22,418, p75 90,473, p90 146,720. Bytes with every earlier receipt compacted away: 303,477; across a model switch: 80,482.
