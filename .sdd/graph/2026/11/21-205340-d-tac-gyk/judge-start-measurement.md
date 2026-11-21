# Relevance judge: known-far floor and replayed entries on five graphs

Measured 2026-11-21 on five graphs of 250 to 5050 entries: 1097 judge calls, 5.479 USD, 0 failed or incomplete answers.
Experiment A plants three known-far entries in every batch and sets aside each candidate that scores at or below their median. That rule set aside 37.9% to 46.5% of about 65 candidates and 46.6% to 62.9% of the large lists, and 0 of 179 needed entries at either size.
Experiment B replays 40 past entries per graph as whole checks. A cut that keeps 95% of the referenced entries lies between 0.10 and 0.45 and sets aside 47.9% to 92.7% of the unreferenced candidates.

## Method

### Graphs

The first graph is this repository's graph. The other four are private and appear as project A, project B, project C and project D. All four are software development projects.

### Judge

- Model `claude-haiku-5-5`, effort low, answer bound to a JSON schema.
- The production prompt and candidate line. A candidate line carries type, kind, title and summary. Entry IDs are stripped.
- Draft text in the production format: kind, topics and body, no title line.
- The judge scores every candidate from 0 to 1.

### Experiment A: known-far entries in every batch

- Drafts: ten past decisions per graph, taken newest first, each with 2 to 8 references and no topic shared with an already picked draft. The judge sees each as a draft under check.
- Needed entries: the entries a draft references.
- Small size: about 65 candidates per draft. These are the needed entries, up to 30 entries of the draft's topics, and random earlier entries for the rest.
- Large size: 764 candidates per draft, the small set padded with random earlier entries. It runs as 8 batches of 95 or 96 lines.
- Known-far entries: 3 per draft, drawn at random from the earlier entries that share no topic and no topic area with the draft, are not a candidate at either size, and lie more than 2 reference hops, in either direction, from every needed entry. The same 3 go into every batch of the draft at random positions. A batch with them holds at most 99 lines.
- Arms: A0 without the planted lines, A1 with them, T as A0 repeated unchanged. T runs for all ten drafts at the small size and for two drafts at the large size.
- Rule under test: a candidate is surely out when its A1 score is at or below the median of the 3 known-far scores of its batch. That median is the floor of the batch.
- Criteria: the rule sets aside at least 30% of the candidates and no needed entry. The planted lines shift the scores of the other candidates, A0 to A1, no more than a repeat does, A0 to T.

### Experiment B: past entries replayed as whole checks

- Entries: 40 past entries per graph with 2 to 8 earlier references, one per equal slice of the date order, least-used topics first. The ten A drafts are excluded.
- Candidates: the referenced entries plus the nearest earlier entries, to 100 lines, shuffled. Entries superseded before the replayed entry are dropped. One judge call per entry.
- Nearest, on this repository's graph: semantic search over the graph with the entry's title and first paragraph as the query.
- Labels: a referenced entry is a positive. Every other candidate is a weak negative.
- Criterion: a cut that keeps 95% of the positives sets aside at least 20% of the weak negatives.
- Ablation: 10 of the 40 entries run a second time with every sentence removed that names a referenced entry by full ID, short ID or title. The candidates stay the same.

### Deviations per graph

- Large size below 764. Project B: 764 for 6 drafts, and every earlier entry except the 3 known-far for 4 drafts, at 590, 305, 293 and 251 candidates. Project C: every earlier entry except the 3 known-far, 266 to 432 per draft. Project D: the same, 184 to 244 per draft, superseded entries included. No random padding occurs where the list is every earlier entry.
- Relaxed topic rule. Project B: the 10th draft shares a topic-area topic with a picked draft. Project C: three drafts share a topic with a picked draft. Project D: two drafts do, with a topic on at most two drafts. This repository's graph and project A needed no relaxation.
- No search index for the four projects. Nearest in B means the earlier entries sharing the most topics, then entries within 2 reference hops, then random earlier entries. Project A: topic sharing filled all 40 checks. Project B: a mean of 63 lines per check share a topic, 17 come by hops alone, 16 are random, and 18 of 40 checks hold random lines. Project C: of 3863 weak negatives 259 came by topic, 479 by hops, 3125 at random. Project D: 1996 by topic, 251 by hops alone, 1568 at random, and 4 early checks have 88 to 94 lines.
- Needed entries. Projects B, C and D count an entry's `supersedes` and `closes` targets as references. Project A counts `refs` alone and leaves a draft's `closes` and `supersedes` targets out of every candidate list.
- Test-retest at the large size. Two drafts on four graphs. All ten drafts on project D.
- Ablation. Project A: 9 entries, because only 9 of the 40 name a referenced entry in their text. Project B: none of the 40 names one in its body, so the ablation runs on 10 other entries that do, each once in full and once ablated.
- Entry types in B. Project C: all 40 are signals.

## Table A: known-far entries in every batch

Values are small size / large size unless a column says otherwise.

### A-1: sets

| Graph | Entries | Drafts | Needed entries | Candidates | Batches | Large size per draft | Entries qualifying as known-far, per draft |
|---|---|---|---|---|---|---|---|
| This repository's graph | 1840 | 10 | 39 | 639 / 7640 | 10 / 80 | 764 | 295 to 622 |
| Project A | 5050 | 10 | 36 | 636 / 7640 | 10 / 80 | 764 | 2355 to 3705 |
| Project B | 857 | 10 | 33 | 633 / 6023 | 10 / 66 | 764, 590, 305, 293, 251 | 33 to 468 |
| Project C | 437 | 10 | 37 | 637 / 3911 | 10 / 46 | 266 to 432 | 176 to 318 |
| Project D | 250 | 10 | 34 | 634 / 2254 | 10 / 29 | 184 to 244 | 34 to 165 |
| Total | | 50 | 179 | | | | |

### A-2: median-of-3 rule

| Graph | Share surely out | Per draft at the small size, min / median / max | Per draft at the large size, min / median / max | Needed entries surely out | Smallest margin of a needed entry above its floor | Drafts under 30% |
|---|---|---|---|---|---|---|
| This repository's graph | 46.5% / 62.9% | 27% / 44% / 81% | 47% / 62% / 79% | 0 of 39 / 0 of 39 | 0.27 / 0.04, next 0.25 | 1 of 10 at 27% / 0 of 10 |
| Project A | 37.9% / 60.6% | 9% / 34% / 89% | 39% / 64% / 76% | 0 of 36 / 0 of 36 | 0.27 / 0.07, next 0.08, then 0.33 | 4 of 10 at 9%, 20%, 22%, 27% / 0 of 10 |
| Project B | 40.0% / 56.6% | 12% / 42% / 76% | 28% / 47% / 84% | 0 of 33 / 0 of 33 | 0.33 / 0.15, next 0.26 | 3 of 10 / 1 of 10 |
| Project C | 39.6% / 56.1% | 12.5% / 37.3% / 65.1% | 46.0% / 52.5% / 73.7% | 0 of 37 / 0 of 37 | 0.27 / 0.06, next 0.38 | 4 of 10 at 12.5%, 23.4%, 23.4%, 25.0% / 0 of 10 |
| Project D | 42.6% / 46.6% | 11% / 45% / 58% | 25% / 46% / 71% | 0 of 34 / 0 of 34 | 0.10, next 0.51 / 0.12, next 0.50 | 1 of 10 at 11% / 1 of 10 at 25% |
| Needed entries lost, total | | | | 0 of 179 / 0 of 179 | | |

Scored in A0 or in T and read against the A1 floor, no needed entry falls at or below it on any graph at either size. At the large size the T reading covers 10, 10, 4, 6 and 34 needed entries.

### A-3: other rules, share out and needed entries lost

| Graph | Lowest of 3 | Highest of 3 | Fixed cut, out at or below 0.05, A1 scores |
|---|---|---|---|
| This repository's graph | 33.2%, 0 / 49.4%, 0 | 56.0%, 0 / 72.9%, 0 | 56.3%, 0 / 75.9%, 0 |
| Project A | 34.3%, 0 / 54.3%, 0 | 58.6%, 0 / 70.6%, 1 | 73.1%, 0 / 81.2%, 0 |
| Project B | 24.3%, 0 / 37.8%, 0 | 50.1%, 0 / 71.5%, 0 | 47.7%, 0 / 61.8%, 0 |
| Project C | 39.6%, 0 / 49.2%, 0 | 56.2%, 0 / 71.6%, 0 | 61.7%, 0 / 71.8%, 0 |
| Project D | 24.3%, 0 / 38.2%, 0 | 55.8%, 0 / 57.3%, 0 | 63.7%, 0 / 69.9%, 0 |
| Needed entries lost, total | 0 of 179 / 0 of 179 | 0 of 179 / 1 of 179 | 0 of 179 / 0 of 179 |

- Fixed cut 0.05 on A0 scores. Project C: 58.2%, 0 / 74.3%, 0. Project D: 62.1%, 0 / 67.0%, 1. Project D on T scores: 63.2%, 0 / 69.9%, 0. Other graphs: not measured.
- Higher fixed cuts, needed entries lost at the large size. This repository's graph loses 1 from 0.10 up. Project A loses 1 at 0.10 and 2 at 0.20. Project B loses none up to 0.10, where the cut sets aside 65.6% / 79.7%, and its first at 0.20. Project C loses 1 at 0.10 with 80.6% set aside. Project D: not measured.
- Project A's loss under the highest-of-3 rule is a needed entry at 0.10 in a batch whose known-far entries scored 0.03, 0.03 and 0.25.

### A-4: shift against test-retest

Mean absolute score difference over all candidates that are not planted. At the large size the figures cover the drafts that have a T arm.

| Graph | Small size, shift A0 to A1 / retest A0 to T | Small size, shift minus retest per batch, bootstrap 95% | Large size, shift A0 to A1 / retest A0 to T | Large size, shift minus retest per batch, bootstrap 95% | Large size, shift over all ten drafts |
|---|---|---|---|---|---|
| This repository's graph | 0.043 / 0.034 | +0.009 (-0.004 to +0.022) | 0.042 / 0.037, two drafts, 16 batches | +0.005 (0.000 to +0.011) | 0.030 |
| Project A | 0.028 / 0.027 | +0.001 (-0.005 to +0.005) | 0.022 / 0.023, two drafts, 16 batches | -0.001 (-0.004 to +0.004) | 0.026 |
| Project B | 0.047 / 0.046 | +0.001 (-0.005 to +0.007) | 0.024 / 0.024, two drafts, 15 batches | +0.000 (-0.005 to +0.005) | 0.036 |
| Project C | 0.035 / 0.032 | +0.003 (-0.003 to +0.010) | 0.043 / 0.047, two drafts, 10 batches | -0.004 (-0.025 to +0.012) | 0.029 |
| Project D | 0.030 / 0.033 | -0.003 (-0.011 to +0.004) | 0.035 / 0.036, ten drafts, 29 batches | -0.001 (-0.007 to +0.004) | 0.035 |

Project D on its first two drafts alone, 6 batches: 0.044 / 0.043, difference +0.001 (-0.015 to +0.015).

### A-5: candidates changing sides between two runs

Each cell gives A0 to A1 / A0 to T.

| Graph | Floor rule, small size | Floor rule, large size | Fixed cut 0.05, small size | Fixed cut 0.05, large size |
|---|---|---|---|---|
| This repository's graph | 14.9% / not measured | 20.0% / not measured | not measured | not measured |
| Project A | 22.5% / 21.2% | 25.8% over ten drafts, 25.3% on the T drafts / 31.5% on the T drafts | 15.4% / 11.8% | 11.8% over ten drafts / 9.8% on the T drafts |
| Project B | 17.7% / 18.0% | 16.6% over ten drafts / 15.9% on the T drafts | 18.3% / 18.3% | 17.0% over ten drafts / 12.5% on the T drafts |
| Project C | 16.2% / 15.5% | 19.9% over ten drafts / 17.3% on the T drafts | 11.6% / 10.2% | 10.1% over ten drafts / 9.1% on the T drafts |
| Project D | 12.8% / 14.7% | 27.5% / 22.6% | 8.2% / 13.4% | 14.7% / 15.3% |

T to A1 under the floor rule: project C 16.0% / 22.7%, project D 18.3% / 23.2%. Under the fixed cut: project C 9.3% / 8.6%. Other graphs: not measured.

### A-6: floors and known-far scores, min / median / max

| Graph | Floor per batch, small size | Floor per batch, large size | Known-far scores, small size | Known-far scores, large size | Known-far scores above 0.10, large size | Widest range of one known-far entry between batches |
|---|---|---|---|---|---|---|
| This repository's graph | 0.02 / 0.03 / 0.20 | 0.02 / 0.03 / 0.20 | 0.01 / 0.03 / 0.45 | 0.01 / 0.03 / 0.45 | 13 of 240 | 0.40 |
| Project A | 0.02 / 0.03 / 0.05 | 0.01 / 0.03 / 0.10 | 0.02 / 0.03 / 0.15 | 0.01 / 0.03 / 0.35 | 11 of 240 | 0.31 |
| Project B | 0.02 / 0.05 / 0.10 | 0.02 / 0.05 / 0.20 | 0.01 / 0.05 / 0.10 | 0.01 / 0.05 / 0.30 | 16 of 198 | 0.26 |
| Project C | 0.02 / 0.025 / 0.15 | 0.01 / 0.02 / 0.30 | 0.02 / 0.03 / 0.20 | 0.01 / 0.03 / 0.50 | 15 of 138 | not measured |
| Project D | 0.02 / 0.02 / 0.05 | 0.01 / 0.03 / 0.25 | 0.01 / 0.02 / 0.35 | 0.01 / 0.03 / 0.30 | 3 of 87 | 0.25 |

### A against the criteria

| Graph | At least 30% out, 0 needed entries out | Shift no larger than retest |
|---|---|---|
| This repository's graph | Met at both sizes | Not met by the point estimate at either size. The interval touches zero at both. |
| Project A | Met at both sizes | Met at the large size. A tie at the small size, +0.001. |
| Project B | Met at both sizes | Met within noise at both sizes |
| Project C | Met at both sizes | Met at the large size. Over by 0.003 at the small size, interval spans zero. |
| Project D | Met at both sizes | Met at both sizes |

## Table B: past entries replayed as whole checks

### B-1: sets and score percentiles

| Graph | Replayed entries | Positives | Weak negatives | Positives, percentile 1 / 2 / 5 / 10 / 25 / 50 | Weak negatives, percentile 1 / 2 / 5 / 10 / 25 / 50 | Weak negatives, percentile 75 / 90 | Mean, positives / weak negatives | Lowest positive |
|---|---|---|---|---|---|---|---|---|
| This repository's graph | 40: 30 signals, 10 decisions | 120 | 3880 | 0.08 / 0.10 / 0.40 / 0.60 / 0.80 / 0.90 | 0.01 / 0.01 / 0.02 / 0.02 / 0.03 / 0.05 | 0.20 / 0.45 | 0.821 / 0.152 | 0.02 |
| Project A | 40: 21 signals, 19 decisions | 128 | 3872 | 0.03 / 0.05 / 0.20 / 0.40 / 0.60 / 0.90 | 0.02 / 0.02 / 0.02 / 0.02 / 0.03 / 0.05 | 0.15 / 0.40 | 0.766 / 0.141 | 0.02 |
| Project B | 40: 2 signals, 38 decisions | 139 | 3861 | 0.04 / 0.05 / 0.10 / 0.20 / 0.45 / 0.80 | 0.02 / 0.02 / 0.02 / 0.03 / 0.05 / 0.10 | 0.20 / 0.40 | 0.672 / 0.150 | 0.02 |
| Project C | 40: 40 signals | 137 | 3863 | 0.10 / 0.15 / 0.30 / 0.40 / 0.60 / 0.85 | 0.01 / 0.01 / 0.02 / 0.02 / 0.02 / 0.03 | 0.05 / 0.20 | 0.741 / 0.075 | 0.05 |
| Project D | 40: 15 signals, 25 decisions | 152 | 3815 | 0.04 / 0.10 / 0.40 / 0.50 / 0.70 / 0.90 | 0.01 / 0.01 / 0.02 / 0.02 / 0.02 / 0.05 | 0.10 / 0.35 | 0.806 / 0.115 | 0.02 |

### B-2: recall cuts

Each cell gives the score cut, keep at or above; the share of weak negatives below it; the positives below it; the checks that lose a positive.

| Graph | Recall 0.90 | Recall 0.95 | Recall 0.98 | Cut 0.20: recall on replay, weak negatives below |
|---|---|---|---|---|
| This repository's graph | 0.60; 93.4%; 10; 8 of 40 | 0.45; 89.3%; 6; 6 of 40 | 0.10; 57.4%; 2; 2 of 40 | 0.975, 74.0% |
| Project A | 0.40; 89.4%; 11; 9 of 40 | 0.20; 75.9%; 5; 5 of 40 | 0.05; 28.2%; 2; 2 of 40 | recall not measured, 75.9% |
| Project B | 0.20; 72.5%; 8; 6 of 40 | 0.10; 47.9%; 4; 3 of 40 | 0.05; 17.6%; 2; 1 of 40 | 0.942, 72.5% |
| Project C | 0.40; 95.7%; 11; 9 of 40 | 0.30; 92.7%; 4; 4 of 40 | 0.15; 86.3%; 2; 2 of 40 | 0.978, 88.3% |
| Project D | 0.50; 93.8%; 13; 11 of 40 | 0.40; 91.1%; 7; 6 of 40 | 0.10; 67.9%; 3; 3 of 40 | 0.974, 80.7% |

- Recall reached at the three cuts on project B: 0.942, 0.971, 0.986. Other graphs: not measured.
- Criterion, a recall-0.95 cut that sets aside at least 20% of weak negatives: met on all five graphs.
- 0.20 is the set-aside cut currently seeded for this judge on this repository's graph. That seeding holds recall 0.98 over 5174 referenced labels scored among themselves and serves in full from 0.012.

### B-3: serve in full

The line is the lowest score at and above which half of the candidates are positives.

| Graph | Line | Positives / weak negatives at or above it | Share positive | Candidates per check at or above it, mean / median / range | Share of all positives at or above it | Precision at 0.85 / 0.80 / 0.50 |
|---|---|---|---|---|---|---|
| This repository's graph | 0.90 | 68 / 58 | 54% | 3.2 / 3 / 0 to 9 | 56.7% | 48.5% / 43.1% / 22.8% |
| Project A | 0.75 | 87 / 82 | 51.5% | 4.2 / 3.5 / 1 to 12 | 68.0% | 59.4% / 52.8% / 28.5% |
| Project B | 0.75 | 75 / 62 | 54.7% | 3.4 / 3 / 0 to 9 | 54.0% | 60.8% / 55.6% / 27.3% |
| Project C | 0.50 | 113 / 98 | 53.6% | 5.3 / 4 / 0 to 17 | 82.5% | 82.1% / 79.0% / 53.6% |
| Project D | 0.65 | 124 / 101 | 55% | 5.6 / 4.5 / 1 to 16 | 81.6% | 70.9% / 61.5% / 37.2% |

At the seeded serve-in-full line of 0.012 nearly all 100 candidates of a check on this repository's graph lie at or above the line. Precision at 0.90 on project A: 69.8%.

### B-4: ablation, citing sentences removed

| Graph | Entries, positives | Mean score of positives, full / ablated | Drop per positive, mean / median / max | Positives dropping by 0.10 or more | Positives below 0.50, full / ablated | Positives below 0.20, full / ablated | Mean score of weak negatives, full / ablated | Weak negatives, mean absolute difference |
|---|---|---|---|---|---|---|---|---|
| This repository's graph | 10, 31 | 0.751 / 0.632 | 0.118 / 0.05 / 0.65 | 39% | 5 / 8 | 2 / 3 | 0.153 / 0.156 | 0.05 |
| Project A | 9, 29 | 0.699 / 0.608 | 0.091 / 0.05 / 0.45 | 31% | 5 / 8 | 4 / 4 | 0.125 / 0.119 | 0.048 |
| Project B | 10 other entries, 34 | 0.836 / 0.800 | 0.037 / 0.00 / 0.70 | 4 of 34 | 2 / 5 | 1 / 1 | 0.171 / 0.162 | 0.051 |
| Project C | 10, 37 | 0.761 / 0.650 | 0.112 / 0.07 / 0.87 | 40.5% | 6 / 12 | 1 / 2 | 0.077 / 0.070 | 0.037 |
| Project D | 10, 36 | 0.805 / 0.702 | 0.103 / 0.05 / 0.57 | 39% | 3 / 7 | 0 / 3 | 0.110 / 0.105 | 0.039 |

Project B: 4 of 34 positives rise by 0.10 or more, the largest rise is 0.70. The 28 positives named in the body drop from 0.814 to 0.772. The 6 not named drop from 0.942 to 0.928.

## Limits

### Candidate lists differ from production

- The small size pads with random earlier entries on every graph. The large size does so on this repository's graph, project A and six drafts of project B. Random padding is easier to set aside than a prefiltered list, so the shares surely out are an upper reading for production.
- On projects C and D, and four drafts of project B, the large size is every earlier entry of one graph, with no padding. On project D 18% of that list shares a topic with the draft, 60% for two drafts. The share out is lowest there, 46.6%. The cause was not tested.
- On project B the two drafts with the lowest small-size shares, 12% and 14%, have 296 and 308 earlier entries, so their random lines come from a narrow graph.
- No run used candidates as production lists them.

### Weak negatives are unlabelled neighbours

- No label says whether an unreferenced candidate was relevant. A weak negative can be a relevant entry the author did not reference.
- On this repository's graph the weak negatives are the nearest entries by embedding. That is the lower reading for the share set aside: 57.4% lie below the recall-0.98 cut.
- On the four projects the weak negatives are topic neighbours, hop neighbours or random entries. Project A: 138 topics, the ten largest on 584 to 1010 entries each, so a shared topic is a weaker nearness than a search hit and the shares set aside are likely an upper reading. Project B: the net direction is unknown. Project C: 81% of the weak negatives are random fill, so the pooled shares are an upper reading. Project D: one topic is on 60% of the graph.
- Project C by tier, share below the recall 0.90 / 0.95 / 0.98 cut: sharing a topic, 259 entries, 75.7% / 64.5% / 37.8%; within 2 hops, 479 entries, 83.5% / 70.8% / 52.2%; random, 3125 entries, 99.2% / 98.4% / 95.5%.
- Project B by topic: 2517 weak negatives share a topic with the draft, median 0.10, 42.6% below 0.10. 1344 share none, median 0.05, 57.8% below 0.10.
- Project A left a replayed entry's `closes` and `supersedes` targets out. They count as neither positive nor negative there.

### Replayed drafts name their references

- The text of a replayed entry can name the entries it references. Removing the citing sentences lowers the mean score of positives by 0.091 to 0.118 on four graphs and by 0.037 on project B, where rises are as large as drops and the drop does not separate from run-to-run spread.
- The share of texts that cite varies. Project A: 308 of the 2552 entries with 2 or more references name one by full ID, 59 by title, and 31 of the 40 replayed entries hold no citing sentence. Project B: 27 of the 562 entries with 2 or more references name one in the body, none of the 40. Project C: 17 of the 40 name none.
- Project B's positives score lowest, mean 0.672. A likely cause, not proven: its bodies do not name their references, and 67 of its 139 positives are `grounded-in` references with a median of 0.60, against 0.90 for the 54 `refines` references.
- The ablation rests on 29 to 37 positives per graph. That is too few for a recall cut on ablated drafts. Not measured.

### Scores move between runs

- The same prompt scored twice moves a candidate by 0.023 to 0.047 on average. 12.8% to 31.5% of the candidates change sides of the floor between two runs, and 8.2% to 18.3% change sides of a fixed 0.05.
- Scores pile up at the floor. On project A 57.5% of the 764-size candidates score at or below 0.03 and 81.2% at or below 0.05, with the median floor at 0.03. A floor that moves by 0.01 moves many candidates across it.
- Needed entries move too. Project D: one needed entry scored 0.05 in the large A0 run, 0.85 in A1 and 0.70 in T. The largest move of a needed entry between two runs there is 0.80. Project C: the lowest needed entry scored 0.08 in the large A1 run, 0.35 in A0 and 0.30 at the small size. Project D's lowest needed entry scored 0.15 in A1 at both sizes, 0.30 and 0.45 in A0, 0.35 and 0.40 in T.
- A1 ran once. The floor's own run-to-run spread is known only through A0 and T scores read against the A1 floor.
- The large-size retest rests on two drafts on four graphs, 10 to 16 batches each.
- The three smallest large-size margins are 0.04, 0.06 and 0.07. Had each needed entry landed in a random batch of its draft, the expected loss on this repository's graph is 0.12 of 39 by A1 scores and 0.25 of 39 by A0 scores. On the four projects it is 0.0.

### Far entries that score high

- A known-far entry does not always score low. Table A-6 gives the counts above 0.10 and the range of one entry between batches.
- The median of 3 absorbed every such case. The highest of 3 did not on project A.
- Project C: one draft's known-far entries score 0.10 to 0.50 and give 13 of the 15 scores above 0.10. Its floors are 0.15 at the small size and 0.20 to 0.30 at the large size. Its needed entries score 0.70 and above.
- Project B: the floor reached 0.20 in 2 of 66 batches. Project D: one large batch had a floor of 0.25, and the lowest needed entry of that draft scored 0.70.
- At the small size on project C the lowest of 3 equals the median, because the two lowest known-far scores tie in all ten batches.

### Differences between graphs

- The recall-0.95 cut ranges from 0.10 to 0.45, the recall-0.98 cut from 0.05 to 0.15, the serve-in-full line from 0.50 to 0.90. The 5th percentile of positives ranges from 0.10 to 0.40.
- The floor rule sets aside less than a fixed 0.05 cut on every graph at both sizes, 37.9% against 73.1% at the small size on project A. The rule leaves 1 to 4 of 10 drafts under 30% at the small size.
- Rank correlation per batch, A0 to A1, small / large over all ten drafts: this repository's graph 0.881 / 0.773, project A 0.769 / 0.700, project B 0.860 / 0.776, project C 0.877 / 0.783, project D 0.868 / 0.791.
- The seeded thresholds were read for this repository's graph alone. Its seeded serve-in-full line of 0.012 and the replayed 0.90 differ because references scored among themselves carry no negatives.
- Project C's graph covers a short period, so a spread over months was not measured there, and its B rests on signals alone. Project B's replayed entries cluster in the graph's densest stretch, 18 of the 40 on one day.
- Percentiles 1 and 2 of the positives rest on the two to four lowest of 120 to 152 scores.

## Cost

Each cell gives judge calls, US dollars.

| Graph | A, small size: A0, A1, T | A, large size: A0, A1, T | B, full | B, ablation | Total |
|---|---|---|---|---|---|
| This repository's graph | 30, 0.105 | 176, 0.942 | 40, 0.259 | 10, 0.064 | 256, 1.369 |
| Project A | 30, 0.093 | 176, 0.935 | 40, 0.223 | 9, 0.050 | 255, 1.301 |
| Project B | 30, 0.085 | 147, 0.632 | 40, 0.204 | 20, 0.102 | 237, 1.023 |
| Project C | 30, 0.105 | 102, 0.592 | 40, 0.244 | 10, 0.060 | 182, 1.000 |
| Project D | 30, 0.100 | 87, 0.392 | 40, 0.237 | 10, 0.057 | 167, 0.786 |
| Total | 150 | 688 | 200 | 59 | 1097, 5.479 |

- Part costs are rounded, so a row can differ from its total by 0.001.
- T calls repeat an A0 prompt and were billed at cached-input rates, 0.001 to 0.004 USD each where stated.
- The planted lines add no call. They take 3 of 99 lines per batch: 0.0058 against 0.0057 USD per call on this repository's graph.
- One draft at the large size costs 0.046 USD per arm on this repository's graph, about 0.044 on project A and 0.014 on project D.
- Project D's large-size T ran for all ten drafts: 6 calls, 0.009 USD, for the first two and 23 calls, 0.106 USD, for the other eight.
