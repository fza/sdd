# Relevance judge: do reference links and time rank the needed entries higher?

Computed 2026-11-21 from the stored answers of the judge measurement on five graphs: sdd (this repository's graph) and projects A to D, four software development projects. No judge call, no model call, no network. The other graphs are named by letter only; this file holds numbers, no entry IDs.

## Method

1. Scores: the stored ones, unchanged. Small size: arm A0, 62 to 67 candidates per draft. Large size: arm A0, all batches of a draft pooled into one list of 764 candidates or of every earlier entry (184 to 764). Experiment B: the full-text replay, 88 to 100 candidates. 50 drafts with 179 needed entries at each size; 200 replayed entries with 676 needed entries in B. Needed = the references the stored sets name for the draft.
2. Links: read from the frontmatter of the candidates (`refs` of every kind, `supersedes`, `closes`), either direction, both ends older than the draft. The draft's own references are never read and the draft is never a candidate.
3. Variants. L1: score + w × the highest score among linked candidates, w = 0.25 and 0.5. L2 pair: a candidate takes the highest score among itself and the candidates joined to it by one `refines`, `supersedes` or `closes` link; L2 chain: the same over whole chains of such links. L3: L1 over links whose two ends share a topic; no embeddings were used, the shared topic stands in for "near in meaning". L4: L1 with w = 0.25 plus 0.1 × the highest score among candidates exactly two links away (the entry in between may be any entry older than the draft). T: among equal scores the newer entry first.
4. Ties: every figure is the exact expected value over a random order of equal scores (no sampling). Under T the order is fixed. Under L2 tied candidates joined by a refinement link take their places newest first.
5. Measures: share of needed entries in the top 10, 20, 30, 50; mean and median rank of needed entries; needed entries whose rank moves up, stays, moves down against the baseline; 95% interval of the change by bootstrap over lists (1000 resamples).
6. Hub controls. H1: candidates ranked by their number of links before the draft alone; H1b: by their number of linked candidates alone. H3: each candidate lifted by w × the best of as many random candidates as it has linked ones (30 draws averaged), so the lift keeps the link counts and drops who is linked to whom.
7. Harm: arm A1 scores against the floor (median of the three planted far entries of the batch), once with the stored floor and once with the planted entries lifted like any candidate; and the expected number of non-needed entries per list whose chance to be in the top 30 rises.
8. "Best of L1 to L4" is the variant with the lowest mean rank of needed entries on experiment B in total: L1 with w = 0.25 (L4 lies 0.03 behind and is shown too). It is chosen on the data it is reported on.

Checks: the rank formulas against a simulation, the link signals against a recount; both agree.

## Answer

No variant ranks the needed entries reliably higher. The judge's own score already puts them near the top, and a linked candidate with a higher score is as common for a non-needed entry as for a needed one.

| | Small size | Large size | Experiment B |
|---|---|---|---|
| Baseline: top 10 / 20 / 30 / 50 | 0.962 / 0.996 / 1.000 / 1.000 | 0.848 / 0.925 / 0.953 / 0.974 | 0.875 / 0.944 / 0.965 / 0.980 |
| Baseline: mean / median rank | 3.46 / 2.50 | 9.66 / 3.50 | 6.41 / 3.00 |
| L1 w=0.25: top 10 / 20 / 30 / 50 | 0.967 / 0.994 / 1.000 / 1.000 | 0.838 / 0.930 / 0.947 / 0.973 | 0.872 / 0.951 / 0.970 / 0.987 |
| L1 w=0.25: mean rank, change and 95% interval | 3.44, -0.02 (-0.11 to +0.07) | 7.41, -2.25 (-6.32 to +0.30) | 6.10, -0.31 (-0.70 to +0.06) |
| L1 w=0.25: up / stay / down | 44 / 106 / 29 | 68 / 80 / 31 | 145 / 416 / 115 |
| T alone: mean rank, up / stay / down | 3.49, 39 / 98 / 42 | 9.37, 62 / 64 / 53 | 6.38, 157 / 352 / 167 |
| T + L1 w=0.25: mean rank | 3.46 | 7.34 | 6.08 |
| H3 w=0.25 (link counts only): mean rank | 3.46 | 9.31 | 6.32 |

- L1 and L4: the mean rank falls, the top 10 moves by +0.005 (small), -0.010 (large), -0.003 (B), the top 20 to 50 gain at most 0.008 in B. Every interval in total includes zero; of the 15 graph-by-data cells one excludes it for L1 w=0.25 (project A, large size: mean rank 7.99 to 5.22). The whole fall of the mean rank at the large size comes from the 8 of 179 needed entries outside the top 30: they rise by 54 ranks on average from a mean rank of 107, and none reaches the top 30 with w = 0.25. In B the 24 needed entries outside the top 30 carry the fall as well; 7 of them reach the top 30 and 3.1 others drop out of it.
- On sdd's own experiment B lists, the only ones built by embedding search, L1 with w = 0.25 is slightly worse than the baseline (mean rank 5.75 to 5.90, top 10 0.919 to 0.903).
- L2 harms. Pair: top 10 in B 0.875 to 0.806, mean rank +1.39 (+0.80 to +1.96); chain: 0.638. A refinement link does not say which end the draft needs: of the refinement pairs in B with a needed end, both ends are needed in 133, only the newer in 521, only the older in 545.
- L3 is no better than L1 (B: mean rank 6.52 and 6.80).
- T changes nothing measurable: as many needed entries move down as up.
- Hub control: link count alone ranks poorly (H1: top 10 in B 0.181, H1b 0.391) but above chance, since a needed entry has 7.4 linked candidates in B and a non-needed one 3.2. H3 reproduces 0.09 of the 0.31 gain in mean rank in B and 0.35 of the 2.25 at the large size; for needed entries with four or more linked candidates it reproduces 0.49 of 0.61 in B. Needed entries without a linked candidate lose: +4.2 ranks in B (20 entries), +6.9 at the large size (7 entries).

Opportunity (total): a needed entry has at least one linked candidate in 82.7% (small), 96.1% (large), 97.0% (B) of cases, and one scoring higher than itself in 40.8%, 50.3%, 48.8%. For non-needed candidates the same shares are 49.1%, 81.3%, 80.0% and 29.9%, 56.0%, 56.8%. Of the 86 needed entries outside the top 10 in B, 68 have a higher-scoring linked candidate; L1 brings 5 (w = 0.25) or 10 (w = 0.5) of them in and pushes others out.

Harm:

- Floor: no needed entry sits at or below the floor under any variant, at either size, with one exception: L2 chain at the large size with the planted entries lifted too, 44 of 179 (project B 25, project D 19), where a planted entry takes the score of a chain it belongs to and raises the floor.
- A lifted score must not feed the floor rule: against the stored floor the share surely out at the large size falls from 58.6% to 25.8% (L1 w=0.25) and 13.3% (L4); with the floor lifted likewise to 44.7% and 41.5%.
- Non-needed entries lifted into the top 30 per list, small / large / B: L1 w=0.25 2.5 / 3.6 / 3.8; L1 w=0.5 3.1 / 5.7 / 5.0; L4 3.5 / 4.1 / 4.4; L2 pair 1.6 / 8.1 / 4.2; T 1.9 / 1.6 / 1.9 (ties re-ordered). Needed entries they push out of the top 30 in B: 3.1 of 676 (L1 w=0.25), 7.2 (L1 w=0.5), 16.0 (L2 pair).

## Limits

- Padded lists. Linked pairs make up 1.99% of all candidate pairs at the small size (0.28% to 3.91% per graph), 0.58% at the large size (0.12% to 3.40%), 3.41% in B (1.66% to 7.00%). At both A sizes most non-needed candidates are random earlier entries with few links: 49% of them have a linked candidate at the small size against 80% in B. A real, prefiltered list holds the neighbours of the needed entries, which a lift raises as well, so the padded sizes flatter the link variants. They show no gain even so.
- The small size cannot show a gain: the baseline is at the ceiling (top 30 = 1.000, 7 of 179 needed entries outside the top 10).
- Experiment B is the nearer test and decides the reading, but only sdd's B lists come from embedding search (40 lists, 120 needed entries). Projects A to D filled their B lists by shared topics, then entries within two links of the draft's references, then random entries (project C mostly random). The second tier picks non-needed candidates by their links to needed ones, which a real list would not do in that way.
- Needed = referenced. A non-needed candidate lifted into the top 30 may be relevant and unreferenced; these figures count it as harm.
- The B drafts often name their references in the text, which lifts the baseline. The stored ablation runs (citing sentences removed) were not re-ranked here; the link signal may be worth more for such drafts.
- The weights are the given ones, not tuned; the best variant is picked on the reported data, so its gain is an upper reading. A second run of the judge alone moves more than any link variant gains: on the small lists scored twice the mean rank of needed entries goes from 3.46 to 3.30 and the top 10 from 0.962 to 0.974, with 61 needed entries up, 69 unchanged, 49 down.
- L3 uses shared topics, not embeddings. In graphs with few topics (project D: 13) nearly every link passes that filter.

## Totals over the five graphs

### Small size: 50 lists, 179 needed entries

| Variant | Top 10 | Top 20 | Top 30 | Top 50 | Mean rank | Median rank | Up / stay / down | Change of mean rank, 95% | Non-needed into top 30 per list |
|---|---|---|---|---|---|---|---|---|---|
| baseline | 0.962 | 0.996 | 1.000 | 1.000 | 3.46 | 2.50 |  |  |  |
| L1 w=0.25 | 0.967 | 0.994 | 1.000 | 1.000 | 3.44 | 3.00 | 44 / 106 / 29 | -0.02 (-0.11 to +0.07) | 2.45 |
| L1 w=0.5 | 0.961 | 0.994 | 1.000 | 1.000 | 3.51 | 3.00 | 47 / 98 / 34 | +0.05 (-0.08 to +0.19) | 3.05 |
| L2 pair | 0.947 | 0.996 | 1.000 | 1.000 | 3.94 | 3.00 | 21 / 80 / 78 | +0.48 (+0.23 to +0.73) | 1.57 |
| L2 chain | 0.877 | 0.975 | 0.994 | 1.000 | 5.12 | 3.00 | 19 / 79 / 81 | +1.66 (+0.83 to +2.65) | 2.11 |
| L3 w=0.25 | 0.955 | 1.000 | 1.000 | 1.000 | 3.57 | 3.00 | 41 / 101 / 37 | +0.11 (-0.02 to +0.25) | 2.30 |
| L3 w=0.5 | 0.950 | 0.994 | 1.000 | 1.000 | 3.72 | 3.00 | 46 / 90 / 43 | +0.26 (+0.04 to +0.49) | 3.11 |
| L4 | 0.967 | 0.994 | 1.000 | 1.000 | 3.42 | 3.00 | 59 / 76 / 44 | -0.04 (-0.13 to +0.06) | 3.47 |
| T | 0.955 | 0.994 | 1.000 | 1.000 | 3.49 | 3.00 | 39 / 98 / 42 | +0.03 (-0.01 to +0.06) | 1.85 |
| T + L1 w=0.25 | 0.967 | 0.994 | 1.000 | 1.000 | 3.46 | 3.00 | 56 / 76 / 47 | +0.00 (-0.08 to +0.10) | 2.90 |
| T + L4 | 0.967 | 0.994 | 1.000 | 1.000 | 3.43 | 3.00 | 58 / 72 / 49 | -0.03 (-0.12 to +0.06) | 3.55 |
| H1 link count | 0.295 | 0.527 | 0.684 | 0.901 | 23.11 | 19.00 | 19 / 4 / 156 | +19.65 (+16.81 to +22.50) | 13.65 |
| H1b in-list links | 0.521 | 0.714 | 0.810 | 0.946 | 15.99 | 10.00 | 33 / 5 / 141 | +12.53 (+10.05 to +15.28) | 11.02 |
| H3 w=0.25 | 0.964 | 0.998 | 1.000 | 1.000 | 3.46 | 2.63 | 82 / 17 / 80 | +0.00 (-0.06 to +0.05) | 3.28 |
| H3 w=0.5 | 0.955 | 0.995 | 1.000 | 1.000 | 3.65 | 2.67 | 70 / 12 / 97 | +0.19 (+0.10 to +0.28) | 4.28 |

### Large size: 50 lists, 179 needed entries

| Variant | Top 10 | Top 20 | Top 30 | Top 50 | Mean rank | Median rank | Up / stay / down | Change of mean rank, 95% | Non-needed into top 30 per list |
|---|---|---|---|---|---|---|---|---|---|
| baseline | 0.848 | 0.925 | 0.953 | 0.974 | 9.66 | 3.50 |  |  |  |
| L1 w=0.25 | 0.838 | 0.930 | 0.947 | 0.973 | 7.41 | 3.50 | 68 / 80 / 31 | -2.25 (-6.32 to +0.30) | 3.63 |
| L1 w=0.5 | 0.838 | 0.908 | 0.955 | 0.976 | 7.57 | 3.50 | 72 / 71 / 36 | -2.09 (-7.17 to +1.14) | 5.73 |
| L2 pair | 0.754 | 0.899 | 0.936 | 0.965 | 12.13 | 4.33 | 35 / 33 / 111 | +2.47 (+0.05 to +5.12) | 8.05 |
| L2 chain | 0.558 | 0.640 | 0.679 | 0.742 | 55.86 | 7.00 | 23 / 31 / 125 | +46.20 (+25.95 to +69.49) | 15.76 |
| L3 w=0.25 | 0.847 | 0.925 | 0.944 | 0.967 | 9.37 | 3.50 | 63 / 75 / 41 | -0.29 (-1.66 to +0.84) | 3.64 |
| L3 w=0.5 | 0.855 | 0.911 | 0.939 | 0.961 | 10.30 | 3.50 | 68 / 66 / 45 | +0.64 (-1.16 to +2.42) | 6.03 |
| L4 | 0.838 | 0.927 | 0.947 | 0.974 | 7.16 | 3.50 | 78 / 62 / 39 | -2.50 (-6.76 to +0.26) | 4.08 |
| T | 0.849 | 0.939 | 0.955 | 0.972 | 9.37 | 3.00 | 62 / 64 / 53 | -0.29 (-0.74 to +0.06) | 1.64 |
| T + L1 w=0.25 | 0.838 | 0.933 | 0.944 | 0.972 | 7.34 | 3.00 | 82 / 48 / 49 | -2.32 (-6.34 to +0.28) | 3.71 |
| T + L4 | 0.838 | 0.927 | 0.944 | 0.972 | 7.15 | 4.00 | 80 / 51 / 48 | -2.51 (-6.72 to +0.25) | 4.14 |
| H1 link count | 0.061 | 0.116 | 0.164 | 0.232 | 190.87 | 136.00 | 12 / 0 / 167 | +181.21 (+145.51 to +220.84) | 26.52 |
| H1b in-list links | 0.064 | 0.128 | 0.194 | 0.287 | 171.58 | 115.00 | 14 / 0 / 165 | +161.92 (+129.18 to +198.80) | 26.20 |
| H3 w=0.25 | 0.841 | 0.928 | 0.957 | 0.973 | 9.31 | 3.70 | 77 / 3 / 99 | -0.35 (-1.42 to +0.29) | 4.12 |
| H3 w=0.5 | 0.810 | 0.915 | 0.948 | 0.968 | 10.32 | 4.28 | 56 / 1 / 122 | +0.66 (-0.99 to +1.83) | 9.13 |

### Experiment B: 200 lists, 676 needed entries

| Variant | Top 10 | Top 20 | Top 30 | Top 50 | Mean rank | Median rank | Up / stay / down | Change of mean rank, 95% | Non-needed into top 30 per list |
|---|---|---|---|---|---|---|---|---|---|
| baseline | 0.875 | 0.944 | 0.965 | 0.980 | 6.41 | 3.00 |  |  |  |
| L1 w=0.25 | 0.872 | 0.951 | 0.970 | 0.987 | 6.10 | 3.00 | 145 / 416 / 115 | -0.31 (-0.70 to +0.06) | 3.82 |
| L1 w=0.5 | 0.869 | 0.950 | 0.969 | 0.985 | 6.20 | 3.00 | 164 / 378 / 134 | -0.21 (-0.73 to +0.26) | 5.02 |
| L2 pair | 0.806 | 0.918 | 0.948 | 0.984 | 7.80 | 4.00 | 93 / 243 / 340 | +1.39 (+0.80 to +1.96) | 4.22 |
| L2 chain | 0.638 | 0.757 | 0.817 | 0.924 | 14.86 | 5.50 | 78 / 215 / 383 | +8.45 (+6.57 to +10.44) | 7.87 |
| L3 w=0.25 | 0.876 | 0.944 | 0.962 | 0.980 | 6.52 | 3.00 | 147 / 383 / 146 | +0.11 (-0.14 to +0.35) | 3.30 |
| L3 w=0.5 | 0.865 | 0.939 | 0.959 | 0.978 | 6.80 | 3.00 | 164 / 339 / 173 | +0.39 (+0.09 to +0.70) | 4.46 |
| L4 | 0.871 | 0.948 | 0.972 | 0.987 | 6.13 | 3.00 | 194 / 337 / 145 | -0.28 (-0.73 to +0.18) | 4.41 |
| T | 0.873 | 0.947 | 0.965 | 0.979 | 6.38 | 3.00 | 157 / 352 / 167 | -0.03 (-0.22 to +0.14) | 1.91 |
| T + L1 w=0.25 | 0.873 | 0.950 | 0.970 | 0.987 | 6.08 | 3.00 | 193 / 307 / 176 | -0.33 (-0.72 to +0.03) | 3.91 |
| T + L4 | 0.870 | 0.948 | 0.972 | 0.987 | 6.14 | 3.00 | 204 / 292 / 180 | -0.27 (-0.72 to +0.19) | 4.44 |
| H1 link count | 0.181 | 0.314 | 0.441 | 0.684 | 38.53 | 34.25 | 78 / 3 / 595 | +32.12 (+29.53 to +34.51) | 18.49 |
| H1b in-list links | 0.391 | 0.565 | 0.685 | 0.848 | 24.36 | 16.75 | 127 / 12 / 537 | +17.95 (+15.66 to +20.13) | 15.14 |
| H3 w=0.25 | 0.877 | 0.943 | 0.966 | 0.984 | 6.32 | 3.17 | 289 / 39 / 348 | -0.09 (-0.39 to +0.19) | 5.20 |
| H3 w=0.5 | 0.860 | 0.941 | 0.962 | 0.983 | 6.77 | 3.72 | 210 / 11 / 455 | +0.36 (-0.03 to +0.73) | 7.53 |

### Change of recall against the baseline, with 95% interval (bootstrap over lists)

| Data | Variant | Top 10 | Top 20 | Top 30 | Top 50 |
|---|---|---|---|---|---|
| small size | L1 w=0.25 | +0.005 (-0.015 to +0.024) | -0.001 (-0.004 to +0.000) | +0.000 (+0.000 to +0.000) | +0.000 (+0.000 to +0.000) |
| small size | L1 w=0.5 | -0.001 (-0.020 to +0.019) | -0.001 (-0.004 to +0.000) | +0.000 (+0.000 to +0.000) | +0.000 (+0.000 to +0.000) |
| small size | L4 | +0.005 (-0.007 to +0.018) | -0.001 (-0.004 to +0.000) | +0.000 (+0.000 to +0.000) | +0.000 (+0.000 to +0.000) |
| small size | T | -0.006 (-0.018 to +0.003) | -0.001 (-0.004 to +0.000) | +0.000 (+0.000 to +0.000) | +0.000 (+0.000 to +0.000) |
| small size | T + L1 w=0.25 | +0.005 (-0.015 to +0.024) | -0.001 (-0.004 to +0.000) | +0.000 (+0.000 to +0.000) | +0.000 (+0.000 to +0.000) |
| large size | L1 w=0.25 | -0.010 (-0.040 to +0.017) | +0.005 (-0.010 to +0.021) | -0.006 (-0.022 to +0.006) | -0.001 (-0.015 to +0.014) |
| large size | L1 w=0.5 | -0.010 (-0.053 to +0.024) | -0.018 (-0.049 to +0.009) | +0.002 (-0.020 to +0.023) | +0.002 (-0.028 to +0.037) |
| large size | L4 | -0.010 (-0.040 to +0.017) | +0.002 (-0.016 to +0.019) | -0.006 (-0.022 to +0.006) | +0.000 (-0.012 to +0.014) |
| large size | T | +0.001 (-0.010 to +0.013) | +0.013 (+0.002 to +0.027) | +0.002 (-0.001 to +0.006) | -0.002 (-0.005 to +0.000) |
| large size | T + L1 w=0.25 | -0.010 (-0.040 to +0.017) | +0.008 (-0.004 to +0.022) | -0.009 (-0.024 to +0.001) | -0.002 (-0.018 to +0.014) |
| experiment B | L1 w=0.25 | -0.003 (-0.014 to +0.007) | +0.008 (-0.001 to +0.018) | +0.006 (-0.003 to +0.015) | +0.006 (-0.002 to +0.015) |
| experiment B | L1 w=0.5 | -0.006 (-0.022 to +0.007) | +0.007 (-0.005 to +0.018) | +0.004 (-0.009 to +0.017) | +0.004 (-0.004 to +0.013) |
| experiment B | L4 | -0.004 (-0.016 to +0.006) | +0.004 (-0.005 to +0.014) | +0.007 (-0.002 to +0.017) | +0.006 (-0.003 to +0.016) |
| experiment B | T | -0.002 (-0.007 to +0.002) | +0.003 (+0.001 to +0.007) | -0.000 (-0.002 to +0.001) | -0.001 (-0.006 to +0.003) |
| experiment B | T + L1 w=0.25 | -0.002 (-0.014 to +0.009) | +0.006 (-0.004 to +0.017) | +0.006 (-0.003 to +0.015) | +0.006 (-0.002 to +0.015) |

## Opportunity

Needed entries:

| Data | Graph | Needed | With a linked candidate | With a linked candidate scoring higher | of these: a higher needed one / a higher non-needed one | Same topic and linked: any / higher | Two links away: any / higher | Refinement link: any / higher | Outside top 10: count, with a higher linked candidate |
|---|---|---|---|---|---|---|---|---|---|
| small size | sdd | 39 | 61.5% | 41.0% | 30.8% / 12.8% | 46.2% / 23.1% | 82.1% / 41.0% | 20.5% / 5.1% | 4, 2 |
| small size | project A | 36 | 72.2% | 30.6% | 27.8% / 2.8% | 72.2% / 30.6% | 88.9% / 38.9% | 38.9% / 13.9% | 0, 0 |
| small size | project B | 33 | 81.8% | 36.4% | 24.2% / 21.2% | 75.8% / 33.3% | 97.0% / 51.5% | 57.6% / 24.2% | 1, 1 |
| small size | project C | 37 | 100.0% | 45.9% | 40.5% / 5.4% | 86.5% / 40.5% | 100.0% / 29.7% | 24.3% / 10.8% | 1, 1 |
| small size | project D | 34 | 100.0% | 50.0% | 44.1% / 29.4% | 97.1% / 44.1% | 100.0% / 32.4% | 76.5% / 32.4% | 1, 1 |
| small size | total | 179 | 82.7% | 40.8% | 33.5% / 14.0% | 74.9% / 34.1% | 93.3% / 38.5% | 42.5% / 16.8% | 7, 5 |
| large size | sdd | 39 | 92.3% | 46.2% | 25.6% / 28.2% | 74.4% / 28.2% | 94.9% / 43.6% | 43.6% / 5.1% | 12, 8 |
| large size | project A | 36 | 88.9% | 33.3% | 27.8% / 8.3% | 86.1% / 33.3% | 100.0% / 38.9% | 61.1% / 19.4% | 4, 3 |
| large size | project B | 33 | 100.0% | 48.5% | 24.2% / 33.3% | 100.0% / 48.5% | 100.0% / 54.5% | 100.0% / 39.4% | 6, 3 |
| large size | project C | 37 | 100.0% | 59.5% | 40.5% / 35.1% | 100.0% / 48.6% | 100.0% / 45.9% | 62.2% / 24.3% | 2, 2 |
| large size | project D | 34 | 100.0% | 64.7% | 52.9% / 41.2% | 97.1% / 55.9% | 100.0% / 38.2% | 85.3% / 35.3% | 3, 3 |
| large size | total | 179 | 96.1% | 50.3% | 34.1% / 29.1% | 91.1% / 42.5% | 98.9% / 44.1% | 69.3% / 24.0% | 27, 19 |
| experiment B | sdd | 120 | 91.7% | 45.0% | 33.3% / 25.8% | 73.3% / 33.3% | 95.0% / 31.7% | 32.5% / 3.3% | 10, 7 |
| experiment B | project A | 128 | 93.0% | 46.9% | 36.7% / 23.4% | 91.4% / 44.5% | 93.8% / 35.9% | 69.5% / 27.3% | 17, 10 |
| experiment B | project B | 139 | 100.0% | 56.8% | 43.2% / 37.4% | 100.0% / 45.3% | 99.3% / 57.6% | 93.5% / 34.5% | 35, 32 |
| experiment B | project C | 137 | 100.0% | 41.6% | 38.7% / 11.7% | 90.5% / 29.2% | 100.0% / 35.8% | 9.5% / 0.7% | 6, 5 |
| experiment B | project D | 152 | 99.3% | 52.6% | 48.0% / 17.8% | 96.1% / 46.7% | 100.0% / 46.7% | 82.9% / 35.5% | 18, 14 |
| experiment B | total | 676 | 97.0% | 48.8% | 40.4% / 23.1% | 90.8% / 40.1% | 97.8% / 42.0% | 58.7% / 21.0% | 86, 68 |

The same shares for the non-needed candidates, which a lift raises too:

| Data | Graph | Non-needed | With a linked candidate | With a linked candidate scoring higher | Same topic and linked: any / higher | Two links away: any / higher | Refinement link: any / higher |
|---|---|---|---|---|---|---|---|
| small size | sdd | 600 | 37.5% | 18.5% | 34.2% / 17.0% | 65.3% / 45.3% | 15.5% / 5.8% |
| small size | project A | 600 | 9.0% | 4.5% | 8.7% / 4.2% | 40.2% / 23.5% | 4.8% / 2.3% |
| small size | project B | 600 | 51.3% | 35.7% | 45.0% / 28.3% | 80.3% / 70.3% | 31.0% / 16.7% |
| small size | project C | 600 | 66.3% | 40.0% | 53.3% / 30.8% | 92.0% / 73.2% | 6.3% / 2.8% |
| small size | project D | 600 | 81.2% | 50.8% | 73.7% / 44.2% | 95.2% / 86.8% | 50.3% / 25.7% |
| small size | total | 3000 | 49.1% | 29.9% | 43.0% / 24.9% | 74.6% / 59.8% | 21.6% / 10.7% |
| large size | sdd | 7601 | 86.0% | 53.5% | 52.5% / 31.1% | 95.7% / 83.4% | 40.7% / 16.9% |
| large size | project A | 7604 | 49.0% | 24.1% | 46.2% / 22.5% | 85.1% / 62.8% | 26.4% / 11.0% |
| large size | project B | 5990 | 97.9% | 77.9% | 97.2% / 73.4% | 98.3% / 93.8% | 86.5% / 57.3% |
| large size | project C | 3874 | 99.7% | 75.6% | 97.2% / 58.8% | 98.7% / 94.7% | 41.6% / 15.0% |
| large size | project D | 2220 | 99.1% | 80.6% | 96.6% / 72.8% | 99.2% / 93.6% | 82.6% / 50.4% |
| large size | total | 27289 | 81.3% | 56.0% | 70.5% / 45.3% | 94.0% / 82.4% | 50.3% / 26.6% |
| experiment B | sdd | 3880 | 64.6% | 40.9% | 34.9% / 21.3% | 84.0% / 66.4% | 16.1% / 7.0% |
| experiment B | project A | 3872 | 61.9% | 36.9% | 61.8% / 36.8% | 81.1% / 62.2% | 41.6% / 18.9% |
| experiment B | project B | 3861 | 91.4% | 72.6% | 87.4% / 62.9% | 96.8% / 92.0% | 70.8% / 41.8% |
| experiment B | project C | 3863 | 85.6% | 52.7% | 69.3% / 29.6% | 95.9% / 81.2% | 3.3% / 1.3% |
| experiment B | project D | 3815 | 97.2% | 81.4% | 91.7% / 72.6% | 99.3% / 96.3% | 64.9% / 40.0% |
| experiment B | total | 19291 | 80.0% | 56.8% | 68.9% / 44.5% | 91.4% / 79.6% | 39.3% / 21.8% |

## Link density of the lists

| Data | Graph | Lists | Candidates per list | Pairs linked | Needed-needed pairs linked | Linked candidates per needed / per non-needed | Links before the draft per needed / per non-needed | Refinement pairs: both needed / only newer / only older / neither |
|---|---|---|---|---|---|---|---|---|
| small size | sdd | 10 | 64 | 1.04% | 20.0% | 1.46 / 0.60 | 8.2 / 5.6 | 3 / 3 / 0 / 51 |
| small size | project A | 10 | 64 | 0.28% | 29.8% | 1.25 / 0.11 | 11.2 / 5.2 | 6 / 1 / 4 / 13 |
| small size | project B | 10 | 63 | 2.05% | 35.4% | 4.06 / 1.12 | 12.0 / 6.4 | 6 / 8 / 17 / 112 |
| small size | project C | 10 | 64 | 2.69% | 39.0% | 3.59 / 1.57 | 6.9 / 6.2 | 4 / 2 / 3 / 18 |
| small size | project D | 10 | 63 | 3.91% | 61.7% | 4.97 / 2.30 | 11.9 / 7.6 | 13 / 25 / 27 / 252 |
| small size | total | 50 | 64 | 1.99% | 35.6% | 3.01 / 1.14 | 9.9 / 6.2 | 32 / 39 / 51 / 446 |
| large size | sdd | 10 | 764 | 0.33% | 20.0% | 4.36 / 2.47 | 8.2 / 5.5 | 3 / 9 / 5 / 1942 |
| large size | project A | 10 | 764 | 0.12% | 29.8% | 2.78 / 0.87 | 11.2 / 5.5 | 6 / 8 / 15 / 1302 |
| large size | project B | 10 | 602 | 0.88% | 35.4% | 11.70 / 5.91 | 12.0 / 6.2 | 6 / 39 / 67 / 7320 |
| large size | project C | 10 | 391 | 1.63% | 39.0% | 6.89 / 6.46 | 6.9 / 6.5 | 4 / 18 / 4 / 1022 |
| large size | project D | 10 | 225 | 3.40% | 61.7% | 11.91 / 7.62 | 11.9 / 7.7 | 13 / 77 / 67 / 2971 |
| large size | total | 50 | 549 | 0.58% | 35.6% | 7.35 / 3.76 | 9.9 / 6.0 | 32 / 151 / 158 / 14557 |
| experiment B | sdd | 40 | 100 | 1.66% | 37.4% | 3.81 / 1.58 | 6.8 / 5.8 | 4 / 27 / 18 / 348 |
| experiment B | project A | 40 | 100 | 1.66% | 37.5% | 4.57 / 1.54 | 7.5 / 5.0 | 31 / 90 / 74 / 1218 |
| experiment B | project B | 40 | 100 | 4.10% | 42.5% | 11.91 / 3.78 | 13.5 / 7.7 | 27 / 137 / 228 / 2617 |
| experiment B | project C | 40 | 100 | 2.67% | 39.8% | 6.39 / 2.51 | 7.2 / 5.5 | 1 / 3 / 13 / 62 |
| experiment B | project D | 40 | 99 | 7.00% | 47.6% | 9.39 / 6.78 | 10.2 / 8.6 | 70 / 264 / 212 / 3274 |
| experiment B | total | 200 | 100 | 3.41% | 41.7% | 7.40 / 3.23 | 9.2 / 6.5 | 133 / 521 / 545 / 7519 |

## Hub control

Change of the mean rank of needed entries (negative = higher up), split by how many candidates of the list the needed entry is linked to. H3 lifts each candidate by the best of as many random candidates as it has links.

| Data | Linked candidates of the needed entry | Needed | Baseline mean rank | L1 w=0.25 | H3 w=0.25 | L1 w=0.5 | H3 w=0.5 | L4 | L2 pair | T |
|---|---|---|---|---|---|---|---|---|---|---|
| small size | 0 | 31 | 3.21 | +1.02 | +0.44 | +1.47 | +0.80 | +1.02 | +0.31 | +0.11 |
| small size | 1 | 37 | 3.35 | -0.41 | +0.02 | -0.32 | +0.17 | -0.51 | +0.19 | +0.05 |
| small size | 2-3 | 48 | 2.88 | -0.18 | +0.08 | -0.20 | +0.25 | -0.23 | +0.62 | +0.04 |
| small size | 4+ | 63 | 4.10 | -0.20 | -0.30 | -0.25 | -0.14 | -0.14 | +0.64 | -0.05 |
| large size | 0 | 7 | 3.00 | +6.93 | +1.71 | +9.93 | +3.65 | +7.00 | +0.50 | -0.14 |
| large size | 1 | 17 | 4.88 | +1.47 | +1.00 | +3.71 | +2.52 | +0.91 | +1.25 | -0.41 |
| large size | 2-3 | 30 | 11.05 | -3.62 | +0.81 | -3.85 | +3.22 | -4.23 | -3.45 | -0.68 |
| large size | 4+ | 125 | 10.34 | -2.94 | -0.93 | -3.12 | -0.37 | -3.08 | +4.17 | -0.18 |
| experiment B | 0 | 20 | 19.02 | +4.17 | +3.94 | +5.72 | +6.20 | +2.70 | +0.88 | -2.38 |
| experiment B | 1 | 44 | 7.06 | -0.53 | +0.89 | -0.52 | +1.72 | +0.03 | +1.00 | -0.74 |
| experiment B | 2-3 | 127 | 4.46 | +0.22 | +0.50 | +0.47 | +1.27 | +0.24 | +0.77 | -0.36 |
| experiment B | 4+ | 485 | 6.34 | -0.61 | -0.49 | -0.60 | -0.24 | -0.57 | +1.62 | +0.22 |

Needed entries outside a cut at baseline, and how many each variant brings inside it:

| Data | Cut | Needed outside it at baseline | With a higher linked candidate | Brought inside by: L1 w=0.25 | L1 w=0.5 | L2 pair | L3 w=0.25 | L4 | T | H3 w=0.25 | H3 w=0.5 | Needed pushed out of top 30 by L1 w=0.25 / w=0.5 |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| small size | top 10 | 7 | 5 | 2 | 2 | 0 | 1 | 1 | 0 | 0 | 1 |  |
| large size | top 10 | 27 | 19 | 3 | 4 | 4 | 5 | 3 | 1 | 2 | 3 |  |
| large size | top 30 | 8 | 6 | 0 | 2 | 2 | 0 | 0 | 0 | 1 | 2 | 1.8 / 1.8 |
| experiment B | top 10 | 86 | 68 | 5 | 10 | 16 | 8 | 5 | 3 | 6 | 10 |  |
| experiment B | top 30 | 24 | 18 | 7 | 9 | 5 | 3 | 8 | 0 | 3 | 5 | 3.1 / 7.2 |

## Harm check against the far floor (arm A1 scores)

Needed entries at or below the floor. Cells of a variant: lifted score against the stored floor / against the floor of the three planted entries lifted the same way.

| Size | Graph | Needed | Baseline | L1 w=0.25 | L1 w=0.5 | L2 pair | L2 chain | L3 w=0.25 | L3 w=0.5 | L4 |
|---|---|---|---|---|---|---|---|---|---|---|
| small size | sdd | 39 | 0 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 |
| small size | project A | 36 | 0 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 |
| small size | project B | 33 | 0 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 |
| small size | project C | 37 | 0 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 |
| small size | project D | 34 | 0 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 |
| small size | total | 179 | 0 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 |
| large size | sdd | 39 | 0 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 |
| large size | project A | 36 | 0 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 |
| large size | project B | 33 | 0 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 25 | 0 / 0 | 0 / 0 | 0 / 0 |
| large size | project C | 37 | 0 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 0 |
| large size | project D | 34 | 0 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 19 | 0 / 0 | 0 / 0 | 0 / 0 |
| large size | total | 179 | 0 | 0 / 0 | 0 / 0 | 0 / 0 | 0 / 44 | 0 / 0 | 0 / 0 | 0 / 0 |

Share of candidates at or below the floor (surely out), same two readings:

| Size | Graph | Candidates | Baseline | L1 w=0.25 | L1 w=0.5 | L2 pair | L2 chain | L3 w=0.25 | L3 w=0.5 | L4 |
|---|---|---|---|---|---|---|---|---|---|---|
| small size | sdd | 639 | 46.5% | 42.1% / 42.4% | 41.3% / 41.8% | 45.7% / 45.7% | 45.7% / 45.7% | 43.3% / 43.3% | 42.6% / 42.6% | 32.7% / 35.8% |
| small size | project A | 636 | 37.9% | 36.3% / 36.3% | 36.3% / 36.3% | 37.6% / 37.6% | 37.6% / 37.6% | 36.3% / 36.3% | 36.3% / 36.3% | 28.9% / 35.2% |
| small size | project B | 633 | 40.0% | 28.8% / 29.4% | 27.2% / 29.4% | 36.7% / 36.7% | 36.3% / 39.0% | 31.0% / 31.6% | 29.4% / 31.6% | 20.5% / 23.1% |
| small size | project C | 637 | 39.6% | 20.3% / 26.8% | 20.3% / 29.0% | 39.2% / 39.2% | 39.1% / 39.1% | 26.5% / 30.3% | 26.5% / 33.0% | 4.4% / 27.8% |
| small size | project D | 634 | 42.6% | 17.7% / 27.0% | 15.3% / 27.6% | 35.5% / 35.5% | 32.5% / 37.1% | 20.3% / 26.2% | 18.3% / 27.6% | 5.5% / 17.4% |
| small size | total | 3179 | 41.3% | 29.0% / 32.4% | 28.1% / 32.8% | 38.9% / 38.9% | 38.3% / 39.7% | 31.5% / 33.6% | 30.6% / 34.2% | 18.4% / 27.9% |
| large size | sdd | 7640 | 62.9% | 30.7% / 52.3% | 24.7% / 52.9% | 55.9% / 59.3% | 54.5% / 59.6% | 44.3% / 56.2% | 41.2% / 57.3% | 17.2% / 47.3% |
| large size | project A | 7640 | 60.6% | 39.9% / 45.9% | 37.3% / 47.2% | 55.0% / 56.8% | 53.9% / 58.0% | 41.2% / 46.2% | 38.7% / 47.4% | 18.8% / 45.1% |
| large size | project B | 6023 | 56.6% | 22.5% / 42.3% | 15.6% / 41.9% | 34.3% / 50.0% | 11.1% / 78.0% | 23.9% / 43.8% | 17.0% / 43.5% | 12.3% / 39.3% |
| large size | project C | 3911 | 56.1% | 5.2% / 39.8% | 3.1% / 41.2% | 49.1% / 54.0% | 47.3% / 53.6% | 7.5% / 42.3% | 5.1% / 44.0% | 2.6% / 36.8% |
| large size | project D | 2254 | 46.6% | 5.7% / 30.3% | 3.4% / 30.9% | 27.3% / 37.4% | 15.1% / 75.2% | 8.5% / 32.2% | 5.4% / 32.8% | 2.8% / 23.8% |
| large size | total | 27468 | 58.6% | 25.8% / 44.7% | 21.4% / 45.4% | 47.6% / 54.0% | 40.5% / 63.6% | 30.8% / 46.8% | 27.1% / 47.6% | 13.3% / 41.5% |

## Per graph

### sdd, small size: 10 lists, 39 needed entries

| Variant | Top 10 | Top 20 | Top 30 | Top 50 | Mean rank | Median rank | Up / stay / down | Change of mean rank, 95% | Non-needed into top 30 per list |
|---|---|---|---|---|---|---|---|---|---|
| baseline | 0.902 | 1.000 | 1.000 | 1.000 | 4.55 | 3.00 |  |  |  |
| L1 w=0.25 | 0.872 | 1.000 | 1.000 | 1.000 | 4.51 | 3.00 | 12 / 20 / 7 | -0.04 (-0.29 to +0.28) | 2.08 |
| L1 w=0.5 | 0.846 | 1.000 | 1.000 | 1.000 | 4.65 | 3.00 | 12 / 19 / 8 | +0.10 (-0.26 to +0.62) | 2.66 |
| L2 pair | 0.887 | 1.000 | 1.000 | 1.000 | 4.77 | 3.00 | 3 / 24 / 12 | +0.22 (-0.10 to +0.50) | 0.63 |
| L2 chain | 0.880 | 1.000 | 1.000 | 1.000 | 4.87 | 3.00 | 3 / 24 / 12 | +0.32 (-0.09 to +0.75) | 0.63 |
| L3 w=0.25 | 0.846 | 1.000 | 1.000 | 1.000 | 4.94 | 3.00 | 6 / 23 / 10 | +0.39 (+0.06 to +0.71) | 1.90 |
| L3 w=0.5 | 0.821 | 1.000 | 1.000 | 1.000 | 5.24 | 3.00 | 8 / 20 / 11 | +0.69 (+0.08 to +1.31) | 2.53 |
| L4 | 0.897 | 1.000 | 1.000 | 1.000 | 4.50 | 3.00 | 13 / 15 / 11 | -0.05 (-0.32 to +0.28) | 2.77 |
| T | 0.872 | 1.000 | 1.000 | 1.000 | 4.64 | 3.00 | 5 / 26 / 8 | +0.09 (-0.03 to +0.20) | 1.56 |
| T + L1 w=0.25 | 0.872 | 1.000 | 1.000 | 1.000 | 4.56 | 3.00 | 13 / 16 / 10 | +0.01 (-0.25 to +0.37) | 2.23 |
| T + L4 | 0.897 | 1.000 | 1.000 | 1.000 | 4.51 | 3.00 | 13 / 13 / 13 | -0.04 (-0.31 to +0.31) | 2.89 |
| H1 link count | 0.299 | 0.465 | 0.535 | 0.769 | 28.31 | 26.50 | 8 / 1 / 30 | +23.76 (+16.21 to +33.90) | 14.99 |
| H1b in-list links | 0.417 | 0.574 | 0.688 | 0.871 | 21.79 | 17.00 | 10 / 0 / 29 | +17.24 (+11.01 to +25.84) | 9.82 |
| H3 w=0.25 | 0.896 | 1.000 | 1.000 | 1.000 | 4.59 | 2.78 | 16 / 5 / 18 | +0.04 (-0.09 to +0.15) | 2.46 |
| H3 w=0.5 | 0.864 | 0.993 | 1.000 | 1.000 | 4.82 | 2.72 | 16 / 5 / 18 | +0.27 (+0.02 to +0.53) | 3.21 |

### sdd, large size: 10 lists, 39 needed entries

| Variant | Top 10 | Top 20 | Top 30 | Top 50 | Mean rank | Median rank | Up / stay / down | Change of mean rank, 95% | Non-needed into top 30 per list |
|---|---|---|---|---|---|---|---|---|---|
| baseline | 0.699 | 0.846 | 0.899 | 0.930 | 19.56 | 4.50 |  |  |  |
| L1 w=0.25 | 0.667 | 0.846 | 0.872 | 0.923 | 13.01 | 4.00 | 16 / 14 / 9 | -6.55 (-20.73 to +3.19) | 2.81 |
| L1 w=0.5 | 0.641 | 0.782 | 0.872 | 0.949 | 12.86 | 5.00 | 13 / 14 / 12 | -6.70 (-25.06 to +5.79) | 5.46 |
| L2 pair | 0.677 | 0.788 | 0.874 | 0.916 | 23.27 | 5.00 | 5 / 8 / 26 | +3.71 (+0.20 to +7.56) | 6.43 |
| L2 chain | 0.674 | 0.770 | 0.856 | 0.914 | 24.26 | 5.00 | 3 / 9 / 27 | +4.70 (+0.37 to +9.62) | 8.86 |
| L3 w=0.25 | 0.733 | 0.821 | 0.872 | 0.901 | 21.29 | 4.50 | 12 / 13 / 14 | +1.73 (+0.08 to +2.98) | 3.28 |
| L3 w=0.5 | 0.718 | 0.795 | 0.821 | 0.897 | 24.01 | 5.50 | 12 / 11 / 16 | +4.45 (+1.03 to +7.20) | 7.23 |
| L4 | 0.667 | 0.846 | 0.872 | 0.923 | 12.21 | 5.00 | 20 / 8 / 11 | -7.35 (-22.15 to +3.19) | 3.36 |
| T | 0.667 | 0.846 | 0.897 | 0.923 | 19.64 | 4.00 | 14 / 11 / 14 | +0.08 (-0.21 to +0.35) | 1.35 |
| T + L1 w=0.25 | 0.667 | 0.846 | 0.872 | 0.923 | 12.95 | 5.00 | 20 / 7 / 12 | -6.61 (-20.71 to +3.05) | 2.70 |
| T + L4 | 0.667 | 0.846 | 0.872 | 0.923 | 12.26 | 5.00 | 20 / 7 / 12 | -7.30 (-21.98 to +3.18) | 3.36 |
| H1 link count | 0.077 | 0.149 | 0.205 | 0.207 | 322.60 | 262.00 | 4 / 0 / 35 | +303.04 (+217.26 to +427.94) | 27.33 |
| H1b in-list links | 0.077 | 0.167 | 0.187 | 0.257 | 271.65 | 225.50 | 5 / 0 / 34 | +252.09 (+157.95 to +369.05) | 27.11 |
| H3 w=0.25 | 0.688 | 0.843 | 0.897 | 0.928 | 17.97 | 4.23 | 13 / 0 / 26 | -1.59 (-5.21 to +0.90) | 2.06 |
| H3 w=0.5 | 0.652 | 0.819 | 0.876 | 0.925 | 19.42 | 4.88 | 8 / 0 / 31 | -0.14 (-6.16 to +4.51) | 7.37 |

### sdd, experiment B: 40 lists, 120 needed entries

| Variant | Top 10 | Top 20 | Top 30 | Top 50 | Mean rank | Median rank | Up / stay / down | Change of mean rank, 95% | Non-needed into top 30 per list |
|---|---|---|---|---|---|---|---|---|---|
| baseline | 0.919 | 0.962 | 0.974 | 0.983 | 5.75 | 3.00 |  |  |  |
| L1 w=0.25 | 0.903 | 0.952 | 0.975 | 0.983 | 5.90 | 3.00 | 29 / 65 / 26 | +0.15 (-0.20 to +0.53) | 3.15 |
| L1 w=0.5 | 0.878 | 0.952 | 0.963 | 0.983 | 6.12 | 3.00 | 34 / 58 / 28 | +0.37 (-0.11 to +0.91) | 4.30 |
| L2 pair | 0.895 | 0.962 | 0.973 | 0.981 | 6.31 | 3.50 | 13 / 47 / 60 | +0.56 (+0.35 to +0.79) | 1.24 |
| L2 chain | 0.883 | 0.962 | 0.972 | 0.981 | 6.55 | 3.63 | 14 / 46 / 60 | +0.80 (+0.49 to +1.14) | 1.45 |
| L3 w=0.25 | 0.925 | 0.950 | 0.975 | 0.983 | 5.83 | 3.00 | 35 / 59 / 26 | +0.08 (-0.32 to +0.45) | 2.25 |
| L3 w=0.5 | 0.892 | 0.958 | 0.969 | 0.983 | 6.18 | 3.00 | 38 / 53 / 29 | +0.43 (-0.18 to +1.00) | 3.06 |
| L4 | 0.906 | 0.950 | 0.975 | 0.992 | 5.52 | 3.00 | 44 / 45 / 31 | -0.23 (-1.28 to +0.59) | 4.02 |
| T | 0.917 | 0.967 | 0.975 | 0.983 | 5.54 | 3.00 | 41 / 54 / 25 | -0.21 (-0.48 to -0.02) | 1.60 |
| T + L1 w=0.25 | 0.908 | 0.950 | 0.975 | 0.983 | 5.81 | 3.00 | 38 / 46 / 36 | +0.06 (-0.29 to +0.46) | 3.25 |
| T + L4 | 0.908 | 0.950 | 0.975 | 0.992 | 5.52 | 3.00 | 42 / 43 / 35 | -0.23 (-1.31 to +0.61) | 4.06 |
| H1 link count | 0.153 | 0.276 | 0.379 | 0.594 | 44.50 | 40.25 | 9 / 1 / 110 | +38.75 (+32.64 to +44.97) | 18.95 |
| H1b in-list links | 0.401 | 0.621 | 0.751 | 0.855 | 23.19 | 13.75 | 23 / 1 / 96 | +17.44 (+12.69 to +23.00) | 13.06 |
| H3 w=0.25 | 0.904 | 0.951 | 0.973 | 0.985 | 6.08 | 3.33 | 43 / 2 / 75 | +0.33 (+0.11 to +0.53) | 4.10 |
| H3 w=0.5 | 0.877 | 0.951 | 0.959 | 0.985 | 6.78 | 3.96 | 31 / 1 / 88 | +1.03 (+0.60 to +1.47) | 6.20 |

### project A, small size: 10 lists, 36 needed entries

| Variant | Top 10 | Top 20 | Top 30 | Top 50 | Mean rank | Median rank | Up / stay / down | Change of mean rank, 95% | Non-needed into top 30 per list |
|---|---|---|---|---|---|---|---|---|---|
| baseline | 1.000 | 1.000 | 1.000 | 1.000 | 2.74 | 2.25 |  |  |  |
| L1 w=0.25 | 1.000 | 1.000 | 1.000 | 1.000 | 2.72 | 3.00 | 14 / 15 / 7 | -0.02 (-0.09 to +0.07) | 1.06 |
| L1 w=0.5 | 1.000 | 1.000 | 1.000 | 1.000 | 2.74 | 2.50 | 15 / 13 / 8 | +0.00 (-0.11 to +0.12) | 1.53 |
| L2 pair | 1.000 | 1.000 | 1.000 | 1.000 | 2.88 | 2.58 | 5 / 17 / 14 | +0.14 (-0.02 to +0.40) | 0.59 |
| L2 chain | 1.000 | 1.000 | 1.000 | 1.000 | 2.88 | 2.58 | 5 / 17 / 14 | +0.14 (-0.02 to +0.40) | 0.59 |
| L3 w=0.25 | 1.000 | 1.000 | 1.000 | 1.000 | 2.72 | 3.00 | 14 / 15 / 7 | -0.02 (-0.09 to +0.07) | 1.05 |
| L3 w=0.5 | 1.000 | 1.000 | 1.000 | 1.000 | 2.74 | 2.50 | 15 / 13 / 8 | +0.00 (-0.11 to +0.12) | 1.52 |
| L4 | 1.000 | 1.000 | 1.000 | 1.000 | 2.72 | 3.00 | 15 / 14 / 7 | -0.02 (-0.09 to +0.07) | 3.16 |
| T | 1.000 | 1.000 | 1.000 | 1.000 | 2.72 | 2.50 | 8 / 21 / 7 | -0.02 (-0.04 to +0.00) | 2.20 |
| T + L1 w=0.25 | 1.000 | 1.000 | 1.000 | 1.000 | 2.72 | 3.00 | 13 / 16 / 7 | -0.02 (-0.09 to +0.07) | 2.75 |
| T + L4 | 1.000 | 1.000 | 1.000 | 1.000 | 2.72 | 3.00 | 15 / 14 / 7 | -0.02 (-0.09 to +0.07) | 3.35 |
| H1 link count | 0.444 | 0.628 | 0.828 | 0.972 | 16.99 | 13.75 | 4 / 2 / 30 | +14.25 (+9.69 to +18.20) | 12.88 |
| H1b in-list links | 0.675 | 0.784 | 0.834 | 0.933 | 13.29 | 4.50 | 11 / 1 / 24 | +10.55 (+7.07 to +15.98) | 12.83 |
| H3 w=0.25 | 1.000 | 1.000 | 1.000 | 1.000 | 2.72 | 2.88 | 17 / 7 / 12 | -0.02 (-0.04 to +0.01) | 0.95 |
| H3 w=0.5 | 1.000 | 1.000 | 1.000 | 1.000 | 2.74 | 2.85 | 18 / 2 / 16 | +0.00 (-0.04 to +0.04) | 1.56 |

### project A, large size: 10 lists, 36 needed entries

| Variant | Top 10 | Top 20 | Top 30 | Top 50 | Mean rank | Median rank | Up / stay / down | Change of mean rank, 95% | Non-needed into top 30 per list |
|---|---|---|---|---|---|---|---|---|---|
| baseline | 0.884 | 0.939 | 0.972 | 0.972 | 7.99 | 3.00 |  |  |  |
| L1 w=0.25 | 0.917 | 0.944 | 0.972 | 1.000 | 5.22 | 3.00 | 21 / 7 / 8 | -2.77 (-7.26 to -0.23) | 4.25 |
| L1 w=0.5 | 0.944 | 0.944 | 1.000 | 1.000 | 5.06 | 3.00 | 23 / 5 / 8 | -2.93 (-7.94 to -0.02) | 7.63 |
| L2 pair | 0.906 | 0.952 | 0.994 | 1.000 | 5.25 | 3.58 | 6 / 11 / 19 | -2.74 (-9.34 to +0.83) | 5.48 |
| L2 chain | 0.794 | 0.869 | 0.906 | 1.000 | 8.65 | 3.67 | 7 / 11 / 18 | +0.66 (-7.86 to +9.93) | 9.68 |
| L3 w=0.25 | 0.917 | 0.944 | 0.972 | 1.000 | 5.28 | 3.00 | 20 / 7 / 9 | -2.71 (-7.26 to -0.22) | 4.01 |
| L3 w=0.5 | 0.944 | 0.944 | 1.000 | 1.000 | 5.11 | 3.00 | 22 / 5 / 9 | -2.88 (-7.88 to +0.00) | 7.05 |
| L4 | 0.917 | 0.944 | 0.972 | 1.000 | 4.83 | 3.00 | 22 / 6 / 8 | -3.16 (-8.44 to -0.29) | 5.23 |
| T | 0.889 | 0.972 | 0.972 | 0.972 | 7.36 | 3.00 | 15 / 12 / 9 | -0.63 (-1.73 to +0.11) | 2.02 |
| T + L1 w=0.25 | 0.917 | 0.944 | 0.972 | 1.000 | 5.11 | 3.00 | 22 / 5 / 9 | -2.88 (-7.48 to -0.27) | 4.43 |
| T + L4 | 0.917 | 0.944 | 0.972 | 1.000 | 4.81 | 3.00 | 22 / 6 / 8 | -3.18 (-8.45 to -0.32) | 5.29 |
| H1 link count | 0.083 | 0.130 | 0.155 | 0.222 | 197.01 | 166.00 | 2 / 0 / 34 | +189.02 (+127.34 to +238.27) | 27.89 |
| H1b in-list links | 0.094 | 0.189 | 0.311 | 0.432 | 159.50 | 78.50 | 3 / 0 / 33 | +151.51 (+107.65 to +196.55) | 26.83 |
| H3 w=0.25 | 0.893 | 0.942 | 0.972 | 0.972 | 8.00 | 3.08 | 23 / 1 / 12 | +0.01 (-0.42 to +0.69) | 2.55 |
| H3 w=0.5 | 0.892 | 0.934 | 0.972 | 0.972 | 8.53 | 3.32 | 19 / 0 / 17 | +0.54 (-0.25 to +1.96) | 6.69 |

### project A, experiment B: 40 lists, 128 needed entries

| Variant | Top 10 | Top 20 | Top 30 | Top 50 | Mean rank | Median rank | Up / stay / down | Change of mean rank, 95% | Non-needed into top 30 per list |
|---|---|---|---|---|---|---|---|---|---|
| baseline | 0.866 | 0.930 | 0.945 | 0.963 | 7.47 | 3.00 |  |  |  |
| L1 w=0.25 | 0.848 | 0.951 | 0.953 | 0.977 | 6.94 | 3.00 | 32 / 73 / 23 | -0.53 (-1.95 to +0.54) | 3.26 |
| L1 w=0.5 | 0.863 | 0.945 | 0.961 | 0.977 | 7.00 | 3.00 | 38 / 65 / 25 | -0.47 (-2.06 to +0.73) | 4.65 |
| L2 pair | 0.809 | 0.945 | 0.953 | 0.970 | 7.78 | 3.42 | 27 / 26 / 75 | +0.31 (-0.98 to +1.36) | 3.63 |
| L2 chain | 0.674 | 0.820 | 0.889 | 0.966 | 12.06 | 5.00 | 21 / 23 / 84 | +4.59 (+1.92 to +7.35) | 6.77 |
| L3 w=0.25 | 0.844 | 0.943 | 0.945 | 0.969 | 7.55 | 3.00 | 32 / 70 / 26 | +0.08 (-0.93 to +0.91) | 3.25 |
| L3 w=0.5 | 0.850 | 0.938 | 0.953 | 0.969 | 7.70 | 3.00 | 39 / 61 / 28 | +0.23 (-0.97 to +1.24) | 4.62 |
| L4 | 0.844 | 0.945 | 0.961 | 0.977 | 7.13 | 3.00 | 37 / 57 / 34 | -0.34 (-1.86 to +0.88) | 4.03 |
| T | 0.867 | 0.938 | 0.945 | 0.969 | 7.06 | 3.00 | 34 / 64 / 30 | -0.41 (-1.15 to +0.13) | 2.13 |
| T + L1 w=0.25 | 0.852 | 0.945 | 0.953 | 0.977 | 6.82 | 3.00 | 43 / 53 / 32 | -0.65 (-2.03 to +0.35) | 3.38 |
| T + L4 | 0.844 | 0.945 | 0.961 | 0.977 | 7.12 | 3.00 | 41 / 47 / 40 | -0.35 (-1.87 to +0.86) | 4.06 |
| H1 link count | 0.240 | 0.398 | 0.499 | 0.708 | 35.19 | 31.00 | 18 / 0 / 110 | +27.72 (+21.99 to +33.00) | 17.37 |
| H1b in-list links | 0.465 | 0.594 | 0.700 | 0.856 | 23.14 | 15.25 | 25 / 3 / 100 | +15.67 (+12.21 to +18.98) | 14.22 |
| H3 w=0.25 | 0.863 | 0.932 | 0.948 | 0.967 | 7.62 | 3.20 | 50 / 8 / 70 | +0.15 (-0.52 to +0.81) | 4.16 |
| H3 w=0.5 | 0.848 | 0.926 | 0.947 | 0.963 | 8.11 | 3.68 | 39 / 1 / 88 | +0.64 (-0.22 to +1.47) | 6.17 |

### project B, small size: 10 lists, 33 needed entries

| Variant | Top 10 | Top 20 | Top 30 | Top 50 | Mean rank | Median rank | Up / stay / down | Change of mean rank, 95% | Non-needed into top 30 per list |
|---|---|---|---|---|---|---|---|---|---|
| baseline | 0.980 | 1.000 | 1.000 | 1.000 | 3.29 | 3.00 |  |  |  |
| L1 w=0.25 | 1.000 | 1.000 | 1.000 | 1.000 | 3.32 | 3.00 | 6 / 20 / 7 | +0.03 (-0.11 to +0.18) | 2.49 |
| L1 w=0.5 | 1.000 | 1.000 | 1.000 | 1.000 | 3.38 | 3.00 | 5 / 19 / 9 | +0.09 (-0.07 to +0.27) | 3.12 |
| L2 pair | 0.970 | 1.000 | 1.000 | 1.000 | 4.07 | 3.00 | 6 / 9 / 18 | +0.78 (+0.23 to +1.33) | 2.06 |
| L2 chain | 0.841 | 0.980 | 1.000 | 1.000 | 5.65 | 4.00 | 6 / 8 / 19 | +2.36 (+0.81 to +3.76) | 2.50 |
| L3 w=0.25 | 0.970 | 1.000 | 1.000 | 1.000 | 3.52 | 3.00 | 4 / 21 / 8 | +0.23 (-0.03 to +0.58) | 2.56 |
| L3 w=0.5 | 0.970 | 0.970 | 1.000 | 1.000 | 3.74 | 3.00 | 4 / 19 / 10 | +0.45 (+0.01 to +1.16) | 3.45 |
| L4 | 0.970 | 1.000 | 1.000 | 1.000 | 3.27 | 3.00 | 7 / 17 / 9 | -0.02 (-0.28 to +0.17) | 3.73 |
| T | 0.970 | 1.000 | 1.000 | 1.000 | 3.30 | 3.00 | 6 / 21 / 6 | +0.01 (-0.05 to +0.11) | 2.13 |
| T + L1 w=0.25 | 1.000 | 1.000 | 1.000 | 1.000 | 3.33 | 3.00 | 7 / 17 / 9 | +0.04 (-0.09 to +0.18) | 2.66 |
| T + L4 | 0.970 | 1.000 | 1.000 | 1.000 | 3.27 | 3.00 | 8 / 15 / 10 | -0.02 (-0.28 to +0.17) | 3.85 |
| H1 link count | 0.263 | 0.478 | 0.763 | 0.946 | 22.00 | 21.00 | 2 / 1 / 30 | +18.71 (+13.80 to +23.40) | 12.47 |
| H1b in-list links | 0.558 | 0.750 | 0.811 | 0.933 | 15.59 | 10.50 | 3 / 2 / 28 | +12.30 (+6.12 to +18.28) | 11.08 |
| H3 w=0.25 | 0.993 | 1.000 | 1.000 | 1.000 | 3.36 | 2.90 | 13 / 3 / 17 | +0.07 (-0.07 to +0.20) | 2.94 |
| H3 w=0.5 | 0.989 | 1.000 | 1.000 | 1.000 | 3.53 | 2.72 | 9 / 2 / 22 | +0.24 (-0.02 to +0.46) | 4.14 |

### project B, large size: 10 lists, 33 needed entries

| Variant | Top 10 | Top 20 | Top 30 | Top 50 | Mean rank | Median rank | Up / stay / down | Change of mean rank, 95% | Non-needed into top 30 per list |
|---|---|---|---|---|---|---|---|---|---|
| baseline | 0.819 | 0.909 | 0.931 | 1.000 | 7.30 | 3.50 |  |  |  |
| L1 w=0.25 | 0.788 | 0.894 | 0.924 | 0.975 | 8.18 | 4.00 | 10 / 15 / 8 | +0.88 (-0.42 to +2.61) | 3.74 |
| L1 w=0.5 | 0.788 | 0.879 | 0.939 | 0.939 | 9.64 | 4.00 | 10 / 15 / 8 | +2.34 (-0.21 to +6.27) | 5.36 |
| L2 pair | 0.697 | 0.849 | 0.879 | 0.909 | 16.99 | 5.80 | 8 / 1 / 24 | +9.69 (+2.54 to +20.86) | 12.11 |
| L2 chain | 0.154 | 0.186 | 0.218 | 0.253 | 193.24 | 124.00 | 3 / 0 / 30 | +185.94 (+123.97 to +278.40) | 27.33 |
| L3 w=0.25 | 0.788 | 0.894 | 0.909 | 0.970 | 8.71 | 4.00 | 9 / 15 / 9 | +1.41 (+0.24 to +3.08) | 3.84 |
| L3 w=0.5 | 0.788 | 0.879 | 0.909 | 0.921 | 10.71 | 4.00 | 9 / 15 / 9 | +3.41 (+0.58 to +7.88) | 5.26 |
| L4 | 0.788 | 0.879 | 0.924 | 0.980 | 8.18 | 4.00 | 11 / 12 / 10 | +0.88 (-0.40 to +2.56) | 3.90 |
| T | 0.849 | 0.909 | 0.939 | 1.000 | 7.55 | 4.00 | 9 / 10 / 14 | +0.25 (-0.14 to +0.69) | 1.43 |
| T + L1 w=0.25 | 0.788 | 0.909 | 0.909 | 0.970 | 8.21 | 4.00 | 13 / 7 / 13 | +0.91 (-0.32 to +2.62) | 3.79 |
| T + L4 | 0.788 | 0.879 | 0.909 | 0.970 | 8.24 | 4.00 | 11 / 8 / 14 | +0.94 (-0.27 to +2.55) | 3.95 |
| H1 link count | 0.061 | 0.121 | 0.121 | 0.227 | 176.20 | 134.50 | 2 / 0 / 31 | +168.90 (+103.54 to +255.25) | 26.89 |
| H1b in-list links | 0.061 | 0.099 | 0.121 | 0.229 | 174.94 | 134.00 | 2 / 0 / 31 | +167.64 (+102.63 to +254.36) | 26.72 |
| H3 w=0.25 | 0.856 | 0.912 | 0.952 | 0.996 | 7.06 | 3.63 | 14 / 1 / 18 | -0.24 (-1.15 to +0.44) | 4.25 |
| H3 w=0.5 | 0.807 | 0.910 | 0.949 | 0.972 | 8.56 | 5.25 | 8 / 1 / 24 | +1.26 (-0.47 to +3.35) | 8.79 |

### project B, experiment B: 40 lists, 139 needed entries

| Variant | Top 10 | Top 20 | Top 30 | Top 50 | Mean rank | Median rank | Up / stay / down | Change of mean rank, 95% | Non-needed into top 30 per list |
|---|---|---|---|---|---|---|---|---|---|
| baseline | 0.756 | 0.871 | 0.922 | 0.965 | 9.79 | 4.00 |  |  |  |
| L1 w=0.25 | 0.763 | 0.892 | 0.942 | 0.986 | 8.65 | 4.00 | 39 / 75 / 25 | -1.14 (-2.46 to +0.02) | 3.82 |
| L1 w=0.5 | 0.763 | 0.885 | 0.946 | 0.983 | 8.50 | 4.00 | 40 / 71 / 28 | -1.29 (-2.92 to +0.25) | 5.13 |
| L2 pair | 0.638 | 0.821 | 0.863 | 0.973 | 12.45 | 6.00 | 25 / 24 / 90 | +2.66 (+0.90 to +4.43) | 7.90 |
| L2 chain | 0.250 | 0.433 | 0.514 | 0.746 | 31.76 | 29.00 | 17 / 9 / 113 | +21.97 (+17.07 to +26.89) | 16.41 |
| L3 w=0.25 | 0.757 | 0.861 | 0.914 | 0.964 | 10.21 | 4.00 | 25 / 74 / 40 | +0.42 (-0.26 to +1.07) | 3.77 |
| L3 w=0.5 | 0.737 | 0.833 | 0.900 | 0.959 | 10.67 | 4.00 | 25 / 69 / 45 | +0.88 (-0.00 to +1.72) | 5.28 |
| L4 | 0.758 | 0.888 | 0.942 | 0.978 | 8.77 | 4.00 | 43 / 68 / 28 | -1.02 (-2.47 to +0.27) | 4.31 |
| T | 0.748 | 0.871 | 0.921 | 0.957 | 10.30 | 4.00 | 22 / 72 / 45 | +0.51 (+0.05 to +0.95) | 1.51 |
| T + L1 w=0.25 | 0.755 | 0.892 | 0.942 | 0.986 | 8.77 | 4.00 | 39 / 63 / 37 | -1.02 (-2.34 to +0.16) | 3.94 |
| T + L4 | 0.748 | 0.892 | 0.942 | 0.978 | 8.84 | 4.00 | 41 / 62 / 36 | -0.95 (-2.39 to +0.35) | 4.36 |
| H1 link count | 0.209 | 0.334 | 0.487 | 0.749 | 34.16 | 32.00 | 26 / 0 / 113 | +24.37 (+19.02 to +30.21) | 18.63 |
| H1b in-list links | 0.400 | 0.636 | 0.806 | 0.934 | 19.01 | 14.50 | 29 / 5 / 105 | +9.22 (+4.94 to +13.46) | 15.35 |
| H3 w=0.25 | 0.774 | 0.875 | 0.936 | 0.984 | 8.54 | 3.73 | 67 / 9 / 63 | -1.25 (-2.30 to -0.39) | 4.72 |
| H3 w=0.5 | 0.762 | 0.887 | 0.937 | 0.984 | 8.65 | 4.12 | 52 / 2 / 85 | -1.14 (-2.58 to +0.12) | 7.55 |

### project C, small size: 10 lists, 37 needed entries

| Variant | Top 10 | Top 20 | Top 30 | Top 50 | Mean rank | Median rank | Up / stay / down | Change of mean rank, 95% | Non-needed into top 30 per list |
|---|---|---|---|---|---|---|---|---|---|
| baseline | 0.964 | 0.980 | 1.000 | 1.000 | 3.28 | 2.50 |  |  |  |
| L1 w=0.25 | 0.973 | 0.973 | 1.000 | 1.000 | 3.32 | 2.50 | 5 / 27 / 5 | +0.04 (-0.08 to +0.21) | 2.60 |
| L1 w=0.5 | 0.973 | 0.973 | 1.000 | 1.000 | 3.46 | 2.50 | 6 / 25 / 6 | +0.18 (-0.07 to +0.47) | 3.40 |
| L2 pair | 0.973 | 0.980 | 1.000 | 1.000 | 3.44 | 2.50 | 3 / 23 / 11 | +0.16 (-0.11 to +0.49) | 0.27 |
| L2 chain | 0.973 | 0.980 | 1.000 | 1.000 | 3.55 | 3.00 | 2 / 23 / 12 | +0.27 (-0.06 to +0.70) | 0.28 |
| L3 w=0.25 | 0.973 | 1.000 | 1.000 | 1.000 | 3.35 | 2.50 | 8 / 22 / 7 | +0.07 (-0.15 to +0.33) | 2.06 |
| L3 w=0.5 | 0.973 | 1.000 | 1.000 | 1.000 | 3.51 | 2.50 | 9 / 19 / 9 | +0.23 (-0.15 to +0.65) | 3.30 |
| L4 | 0.973 | 0.973 | 1.000 | 1.000 | 3.31 | 2.00 | 11 / 16 / 10 | +0.03 (-0.09 to +0.18) | 3.11 |
| T | 0.973 | 0.973 | 1.000 | 1.000 | 3.24 | 2.00 | 10 / 18 / 9 | -0.04 (-0.12 to +0.03) | 1.58 |
| T + L1 w=0.25 | 0.973 | 0.973 | 1.000 | 1.000 | 3.30 | 2.00 | 11 / 15 / 11 | +0.02 (-0.12 to +0.19) | 2.75 |
| T + L4 | 0.973 | 0.973 | 1.000 | 1.000 | 3.30 | 2.00 | 11 / 16 / 10 | +0.02 (-0.10 to +0.16) | 3.11 |
| H1 link count | 0.171 | 0.459 | 0.623 | 0.928 | 26.07 | 24.50 | 1 / 0 / 36 | +22.79 (+20.32 to +25.69) | 14.18 |
| H1b in-list links | 0.498 | 0.839 | 0.918 | 0.998 | 12.95 | 10.50 | 4 / 2 / 31 | +9.67 (+6.60 to +13.11) | 9.06 |
| H3 w=0.25 | 0.956 | 0.989 | 1.000 | 1.000 | 3.32 | 2.43 | 17 / 2 / 18 | +0.04 (-0.06 to +0.14) | 3.96 |
| H3 w=0.5 | 0.958 | 0.990 | 1.000 | 1.000 | 3.63 | 2.57 | 12 / 2 / 23 | +0.35 (+0.19 to +0.54) | 5.14 |

### project C, large size: 10 lists, 37 needed entries

| Variant | Top 10 | Top 20 | Top 30 | Top 50 | Mean rank | Median rank | Up / stay / down | Change of mean rank, 95% | Non-needed into top 30 per list |
|---|---|---|---|---|---|---|---|---|---|
| baseline | 0.937 | 0.978 | 0.996 | 1.000 | 4.86 | 3.50 |  |  |  |
| L1 w=0.25 | 0.919 | 1.000 | 1.000 | 1.000 | 4.59 | 3.00 | 12 / 21 / 4 | -0.27 (-1.00 to +0.42) | 3.35 |
| L1 w=0.5 | 0.919 | 0.973 | 1.000 | 1.000 | 4.64 | 3.00 | 15 / 16 / 6 | -0.22 (-1.24 to +1.00) | 4.72 |
| L2 pair | 0.872 | 0.999 | 1.000 | 1.000 | 5.03 | 3.50 | 9 / 8 / 20 | +0.17 (-0.55 to +0.81) | 4.52 |
| L2 chain | 0.806 | 0.969 | 1.000 | 1.000 | 6.56 | 5.00 | 6 / 7 / 24 | +1.70 (+1.05 to +2.29) | 7.77 |
| L3 w=0.25 | 0.892 | 1.000 | 1.000 | 1.000 | 4.59 | 3.00 | 13 / 20 / 4 | -0.27 (-0.96 to +0.42) | 3.35 |
| L3 w=0.5 | 0.919 | 0.973 | 1.000 | 1.000 | 4.70 | 3.50 | 13 / 18 / 6 | -0.16 (-1.15 to +1.04) | 5.15 |
| L4 | 0.919 | 1.000 | 1.000 | 1.000 | 4.64 | 3.00 | 13 / 18 / 6 | -0.22 (-0.95 to +0.45) | 3.72 |
| T | 0.946 | 1.000 | 1.000 | 1.000 | 4.49 | 3.00 | 15 / 16 / 6 | -0.37 (-0.89 to -0.05) | 1.14 |
| T + L1 w=0.25 | 0.919 | 1.000 | 1.000 | 1.000 | 4.51 | 3.00 | 16 / 14 / 7 | -0.35 (-1.11 to +0.38) | 3.48 |
| T + L4 | 0.919 | 1.000 | 1.000 | 1.000 | 4.62 | 3.00 | 15 / 15 / 7 | -0.24 (-0.95 to +0.44) | 3.75 |
| H1 link count | 0.000 | 0.027 | 0.030 | 0.069 | 167.85 | 163.00 | 0 / 0 / 37 | +162.99 (+145.57 to +183.45) | 26.26 |
| H1b in-list links | 0.000 | 0.027 | 0.033 | 0.073 | 166.50 | 161.50 | 0 / 0 / 37 | +161.64 (+143.96 to +182.17) | 26.18 |
| H3 w=0.25 | 0.889 | 0.984 | 0.998 | 1.000 | 5.18 | 4.05 | 11 / 1 / 25 | +0.32 (+0.16 to +0.50) | 6.76 |
| H3 w=0.5 | 0.826 | 0.955 | 0.983 | 1.000 | 6.49 | 5.33 | 7 / 0 / 30 | +1.63 (+0.99 to +2.38) | 12.34 |

### project C, experiment B: 40 lists, 137 needed entries

| Variant | Top 10 | Top 20 | Top 30 | Top 50 | Mean rank | Median rank | Up / stay / down | Change of mean rank, 95% | Non-needed into top 30 per list |
|---|---|---|---|---|---|---|---|---|---|
| baseline | 0.959 | 0.985 | 0.993 | 0.999 | 3.75 | 2.50 |  |  |  |
| L1 w=0.25 | 0.960 | 0.985 | 1.000 | 1.000 | 3.70 | 2.50 | 17 / 98 / 22 | -0.05 (-0.46 to +0.23) | 4.73 |
| L1 w=0.5 | 0.945 | 0.993 | 1.000 | 1.000 | 3.85 | 2.50 | 20 / 88 / 29 | +0.10 (-0.51 to +0.54) | 5.94 |
| L2 pair | 0.954 | 0.985 | 0.993 | 0.999 | 3.93 | 2.50 | 1 / 116 / 20 | +0.18 (+0.08 to +0.28) | 0.41 |
| L2 chain | 0.954 | 0.985 | 0.993 | 0.999 | 3.99 | 2.50 | 0 / 115 / 22 | +0.24 (+0.10 to +0.43) | 0.42 |
| L3 w=0.25 | 0.967 | 0.993 | 0.993 | 0.996 | 3.84 | 2.50 | 19 / 90 / 28 | +0.09 (-0.03 to +0.22) | 3.00 |
| L3 w=0.5 | 0.958 | 0.993 | 0.993 | 0.993 | 4.10 | 3.00 | 24 / 75 / 38 | +0.35 (+0.18 to +0.56) | 3.95 |
| L4 | 0.960 | 0.985 | 1.000 | 1.000 | 3.70 | 2.50 | 26 / 83 / 28 | -0.05 (-0.48 to +0.27) | 5.38 |
| T | 0.956 | 0.985 | 0.993 | 0.993 | 3.88 | 3.00 | 15 / 92 / 30 | +0.13 (+0.04 to +0.26) | 2.52 |
| T + L1 w=0.25 | 0.964 | 0.985 | 1.000 | 1.000 | 3.72 | 3.00 | 26 / 77 / 34 | -0.03 (-0.44 to +0.26) | 4.82 |
| T + L4 | 0.964 | 0.985 | 1.000 | 1.000 | 3.70 | 3.00 | 29 / 73 / 35 | -0.05 (-0.47 to +0.28) | 5.40 |
| H1 link count | 0.180 | 0.332 | 0.503 | 0.715 | 37.22 | 30.00 | 12 / 2 / 123 | +33.47 (+28.56 to +37.95) | 17.59 |
| H1b in-list links | 0.529 | 0.704 | 0.803 | 0.920 | 17.08 | 9.50 | 34 / 3 / 100 | +13.33 (+9.65 to +17.04) | 13.73 |
| H3 w=0.25 | 0.962 | 0.987 | 0.995 | 0.999 | 3.70 | 2.57 | 62 / 13 / 62 | -0.05 (-0.23 to +0.09) | 6.41 |
| H3 w=0.5 | 0.942 | 0.986 | 0.993 | 0.999 | 4.11 | 2.92 | 36 / 7 / 94 | +0.36 (+0.10 to +0.57) | 8.34 |

### project D, small size: 10 lists, 34 needed entries

| Variant | Top 10 | Top 20 | Top 30 | Top 50 | Mean rank | Median rank | Up / stay / down | Change of mean rank, 95% | Non-needed into top 30 per list |
|---|---|---|---|---|---|---|---|---|---|
| baseline | 0.971 | 1.000 | 1.000 | 1.000 | 3.34 | 2.75 |  |  |  |
| L1 w=0.25 | 1.000 | 1.000 | 1.000 | 1.000 | 3.19 | 3.00 | 7 / 24 / 3 | -0.15 (-0.38 to +0.00) | 4.02 |
| L1 w=0.5 | 1.000 | 1.000 | 1.000 | 1.000 | 3.19 | 3.00 | 9 / 22 / 3 | -0.15 (-0.39 to +0.03) | 4.55 |
| L2 pair | 0.912 | 1.000 | 1.000 | 1.000 | 4.54 | 3.33 | 4 / 7 / 23 | +1.20 (+0.49 to +1.85) | 4.31 |
| L2 chain | 0.673 | 0.912 | 0.971 | 1.000 | 8.99 | 6.86 | 3 / 7 / 24 | +5.65 (+2.61 to +9.07) | 6.56 |
| L3 w=0.25 | 1.000 | 1.000 | 1.000 | 1.000 | 3.19 | 3.00 | 9 / 20 / 5 | -0.15 (-0.38 to +0.00) | 3.94 |
| L3 w=0.5 | 1.000 | 1.000 | 1.000 | 1.000 | 3.22 | 2.75 | 10 / 19 / 5 | -0.12 (-0.38 to +0.05) | 4.74 |
| L4 | 1.000 | 1.000 | 1.000 | 1.000 | 3.18 | 2.50 | 13 / 14 / 7 | -0.16 (-0.43 to +0.00) | 4.56 |
| T | 0.971 | 1.000 | 1.000 | 1.000 | 3.41 | 3.00 | 10 / 12 / 12 | +0.07 (+0.00 to +0.16) | 1.79 |
| T + L1 w=0.25 | 1.000 | 1.000 | 1.000 | 1.000 | 3.26 | 3.00 | 12 / 12 / 10 | -0.08 (-0.16 to +0.00) | 4.11 |
| T + L4 | 1.000 | 1.000 | 1.000 | 1.000 | 3.24 | 3.00 | 11 / 14 / 9 | -0.10 (-0.24 to +0.00) | 4.56 |
| H1 link count | 0.297 | 0.613 | 0.694 | 0.906 | 21.49 | 15.75 | 4 / 0 / 30 | +18.15 (+11.48 to +25.37) | 13.70 |
| H1b in-list links | 0.466 | 0.628 | 0.805 | 1.000 | 15.91 | 11.75 | 5 / 0 / 29 | +12.57 (+7.30 to +17.95) | 12.32 |
| H3 w=0.25 | 0.982 | 1.000 | 1.000 | 1.000 | 3.19 | 2.63 | 19 / 0 / 15 | -0.15 (-0.28 to -0.04) | 6.08 |
| H3 w=0.5 | 0.978 | 0.995 | 1.000 | 1.000 | 3.40 | 2.69 | 15 / 1 / 18 | +0.06 (-0.04 to +0.14) | 7.36 |

### project D, large size: 10 lists, 34 needed entries

| Variant | Top 10 | Top 20 | Top 30 | Top 50 | Mean rank | Median rank | Up / stay / down | Change of mean rank, 95% | Non-needed into top 30 per list |
|---|---|---|---|---|---|---|---|---|---|
| baseline | 0.912 | 0.961 | 0.971 | 0.971 | 7.56 | 3.50 |  |  |  |
| L1 w=0.25 | 0.912 | 0.971 | 0.971 | 0.971 | 5.60 | 3.00 | 9 / 23 / 2 | -1.96 (-4.92 to +0.00) | 4.00 |
| L1 w=0.5 | 0.912 | 0.971 | 0.971 | 0.989 | 5.35 | 3.00 | 11 / 21 / 2 | -2.21 (-5.51 to -0.02) | 5.47 |
| L2 pair | 0.607 | 0.912 | 0.929 | 1.000 | 9.65 | 5.00 | 7 / 5 / 22 | +2.09 (-3.01 to +6.84) | 11.72 |
| L2 chain | 0.298 | 0.332 | 0.337 | 0.464 | 62.40 | 62.00 | 4 / 4 / 26 | +54.84 (+39.02 to +73.37) | 25.17 |
| L3 w=0.25 | 0.912 | 0.971 | 0.971 | 0.971 | 5.84 | 3.25 | 9 / 20 / 5 | -1.72 (-4.71 to +0.57) | 3.74 |
| L3 w=0.5 | 0.912 | 0.971 | 0.971 | 0.989 | 5.74 | 3.00 | 12 / 17 / 5 | -1.82 (-5.17 to +0.84) | 5.47 |
| L4 | 0.912 | 0.971 | 0.971 | 0.971 | 5.57 | 3.00 | 12 / 18 / 4 | -1.99 (-5.04 to +0.02) | 4.20 |
| T | 0.912 | 0.971 | 0.971 | 0.971 | 6.79 | 3.00 | 9 / 15 / 10 | -0.77 (-2.21 to +0.25) | 2.25 |
| T + L1 w=0.25 | 0.912 | 0.971 | 0.971 | 0.971 | 5.50 | 3.00 | 11 / 15 / 8 | -2.06 (-5.26 to +0.08) | 4.16 |
| T + L4 | 0.912 | 0.971 | 0.971 | 0.971 | 5.47 | 3.00 | 12 / 15 / 7 | -2.09 (-5.32 to +0.08) | 4.33 |
| H1 link count | 0.088 | 0.157 | 0.313 | 0.454 | 72.56 | 56.50 | 4 / 0 / 30 | +65.00 (+45.72 to +89.92) | 24.25 |
| H1b in-list links | 0.088 | 0.157 | 0.324 | 0.459 | 71.87 | 55.50 | 4 / 0 / 30 | +64.31 (+45.17 to +88.69) | 24.16 |
| H3 w=0.25 | 0.894 | 0.966 | 0.971 | 0.972 | 7.42 | 3.45 | 16 / 0 / 18 | -0.14 (-0.78 to +0.50) | 5.01 |
| H3 w=0.5 | 0.889 | 0.966 | 0.969 | 0.974 | 7.66 | 3.78 | 14 / 0 / 20 | +0.10 (-0.59 to +0.86) | 10.44 |

### project D, experiment B: 40 lists, 152 needed entries

| Variant | Top 10 | Top 20 | Top 30 | Top 50 | Mean rank | Median rank | Up / stay / down | Change of mean rank, 95% | Non-needed into top 30 per list |
|---|---|---|---|---|---|---|---|---|---|
| baseline | 0.882 | 0.969 | 0.987 | 0.991 | 5.33 | 3.00 |  |  |  |
| L1 w=0.25 | 0.890 | 0.974 | 0.980 | 0.987 | 5.38 | 3.00 | 28 / 105 / 19 | +0.05 (-0.41 to +0.59) | 4.14 |
| L1 w=0.5 | 0.895 | 0.974 | 0.974 | 0.980 | 5.61 | 3.00 | 32 / 96 / 24 | +0.28 (-0.31 to +1.01) | 5.07 |
| L2 pair | 0.756 | 0.889 | 0.962 | 0.994 | 8.25 | 5.00 | 27 / 30 / 95 | +2.92 (+1.44 to +4.58) | 7.94 |
| L2 chain | 0.483 | 0.631 | 0.752 | 0.940 | 18.13 | 12.00 | 26 / 22 / 104 | +12.80 (+9.55 to +16.48) | 14.31 |
| L3 w=0.25 | 0.890 | 0.974 | 0.980 | 0.987 | 5.25 | 3.00 | 36 / 90 / 26 | -0.08 (-0.49 to +0.31) | 4.23 |
| L3 w=0.5 | 0.890 | 0.974 | 0.980 | 0.987 | 5.44 | 3.00 | 38 / 81 / 33 | +0.11 (-0.42 to +0.62) | 5.39 |
| L4 | 0.890 | 0.967 | 0.980 | 0.987 | 5.54 | 3.00 | 44 / 84 / 24 | +0.21 (-0.38 to +1.01) | 4.30 |
| T | 0.882 | 0.974 | 0.987 | 0.993 | 5.14 | 3.00 | 45 / 70 / 37 | -0.19 (-0.44 to +0.02) | 1.77 |
| T + L1 w=0.25 | 0.888 | 0.974 | 0.980 | 0.987 | 5.34 | 3.00 | 47 / 68 / 37 | +0.01 (-0.46 to +0.54) | 4.16 |
| T + L4 | 0.888 | 0.967 | 0.980 | 0.987 | 5.53 | 3.00 | 51 / 67 / 34 | +0.20 (-0.39 to +1.02) | 4.31 |
| H1 link count | 0.128 | 0.238 | 0.344 | 0.646 | 41.79 | 40.00 | 13 / 0 / 139 | +36.46 (+32.23 to +41.11) | 19.93 |
| H1b in-list links | 0.186 | 0.308 | 0.404 | 0.693 | 37.77 | 37.50 | 16 / 0 / 136 | +32.44 (+27.71 to +37.65) | 19.33 |
| H3 w=0.25 | 0.886 | 0.966 | 0.977 | 0.986 | 5.75 | 3.40 | 67 / 7 / 78 | +0.42 (+0.02 to +0.96) | 6.64 |
| H3 w=0.5 | 0.873 | 0.956 | 0.970 | 0.985 | 6.31 | 3.70 | 52 / 0 / 100 | +0.98 (+0.50 to +1.58) | 9.39 |
