# Concepts as durable named sets: dialogue record

Session of 2026-10-30, Christopher and Claude. Everything under "Candidates" is unsettled and was deliberately left for a future session.

## How it came up

Christopher asked for an overview of the larger open concepts in the graph, naming rules, ingestion of issues and projections as examples. Claude built it by clustering the open entries: an export of the graph, summary embeddings, a weighted graph over references, shared neighbours and semantic similarity, Louvain community detection, then curation into named clusters with a maturity mark each.

Two things followed. Christopher observed that the clustering is a concept of its own. And most of the names Claude had given the clusters were not understandable to him; they were replaced with the graph's own wording, and one cluster named with a Claude coinage ("remote lineage") had to be reframed entirely, as "graph branches", because the coined term hid what the cluster was about.

## The why, in Christopher's words

> The whole cluster thing is a concept for itself (automatic / emergent and/or explicit cluster entries). The rough idea would be that these form sets (nested/overlapping) over the graph entries.

> The graph can hold a complex picture from many pieces. As humans we need concepts and terms to work with that. It should be durable and not be created on the fly every session. It should not hold decisions for itself to compete with other entries.

> Also: how does this relate to topics?? Are topics that already somehow? But: these concepts could need some definition / content of their own.

Added at playback, a second reason:

> One more WHY: to be able to scale for projects in size and over time, the graph needs capabilities for "divide and conquer". This means looking at "subgraphs" to reason over instead of the whole thing.

And, clarifying it:

> Well, I don't mean technically the scale of the graph. But the UX of the graph!

## What the graph held on 2026-10-30

- **Topics.** Hierarchical labels, written at capture or laid over existing entries by an annotation. An entry can carry several labels, so sets overlap; `topic(L)` matches by path prefix, so sets nest along one path tree. The idea started as the request for emergent clustering in 20260410-110420-s-cpt-s43. A label is only a string: no entry owns or defines it, and labels cannot be renamed or merged (20260601-162249-s-cpt-k1b).
- **Annotations** (20260818-110500-s-prc-ann). Lay labels over existing entries; membership is binary; an annotation commits nothing, ranks nothing and closes nothing. Its body names the cluster in a line at most, and any argument belongs in an entry of its own. The fact leaves room: "structure of other sorts can join later without changing what an annotation is". None has been captured since June; the engine has no capture path for one (20260722-141909-s-tac-6rd), which also blocks bootstrap's topic founding (20260722-141942-s-cpt-ksz).
- **Definitions.** Only facts enrolled in the pull index carry a title and a topic (20260719-133529-d-cpt-qhn). SDD's own terms are to ship as one fact declaring `class: vocabulary`, replaceable by a project through supersession (20260904-141736-d-cpt-xob). A project's domain terms have no home (20260818-143339-s-cpt-8h2).
- **Clusters made on the fly.** The skill-era catch-up derived clusters on each run (20260505-153654-d-tac-qom). The vision landscape's six regions exist only in an attachment of 20260818-133112-d-tac-okx. This session's overview computed and named its clusters itself.
- **Derived views.** The graph's stance is to compute per-participant and similar views on read and never store them (20260604-152617-s-cpt-dnh); the one stored derivation, summaries, drifts (20260427-151153-s-cpt-tdp).
- **Topics in served knowledge.** No base fact explains topics; the type-system overview does not mention them. The capture procedure's topics lane and a paragraph in 20260702-222259-d-cpt-7iy carry the rules.
- **Focus.** A focus also names a set of entries, but it is a decision with its own lifecycle, which is the competing shape the why rules out.

## Correction at playback

Reading the drafted gap, Christopher corrected the stated need:

> They need _clusters_ having names instead of a web of entries. A concept/term could be one such cluster.

The entry therefore speaks of named clusters, with a concept or term as one possible kind of cluster. Where the candidates below say "concept", read a named cluster.

## Candidates

### 1. Concept and domain term

Asked whether a concept with a name, a definition and a member set is the same thing as a domain term from 20260818-143339-s-cpt-8h2, Christopher's sense was that they are connected or the same.

For it: a domain term is a name, a meaning and the entries that talk about it. The annotation fact's own example label `timber/stock-quality` acts as a domain concept. In this repository SDD's domain is SDD itself, so concepts such as "Rule system" or "Intake model" are domain terms of this project. The difference still visible: a concept can be a larger set containing other concepts, where a domain term is usually one word.

### 2. The topic label as the concept's canonical handle

Christopher's proposal: if a concept has topics, those can be the canonical for that concept. The concept entry holds the meaning; membership keeps running through topics, at capture or by annotation.

What it gives: durable overlapping and nesting sets without a new membership mechanism; a home for definitions, of which the vocabulary fact of 20260904-141736-d-cpt-xob would be one instance; and a route to renames. An actor carries a canonical name with aliases and evolves by supersession (20260818-110000-s-prc-act). A concept could do the same: superseding it with a new canonical label and the old one as an alias lets old labels resolve to the concept, which would answer 20260601-162249-s-cpt-k1b without touching immutable entries.

Open: sets that do not follow one path, since "The world flowing in" spans `collaboration/intake` and the event stream on different paths, so a concept may own several labels or point at sub-concepts.

### 3. Reference to a concept entry versus topic membership

Raised by Christopher. Claude's reading:

- A topic label says what an entry is about: binary, no reason, no heat, assignable afterwards by an annotation.
- A reference says what an entry's reasoning relies on and why: a ref kind, written in the entry, giving the target heat.

Keep them distinct. Most members will not reference the concept entry, and some entries will reference it without being members, for example one grounded in its definition or arguing against it. If a reference implied membership, "used often" and "belongs here" would collapse. They can feed each other: references show a concept is in use, as the rule plan uses `grounded-in` refs to measure rule usage (20260622-084244-d-tac-eho), and references from outside the set point at candidate members.

### 4. Nesting

Raised by Christopher. Two options:

- Label paths, which exist but form one tree: a concept gets one parent, and cross-cutting groupings do not fit.
- Relations between concepts: a concept is part of one or more others, allowing several parents and overlaps, with a parent's members derived on read as the union of its parts.

Claude's preference: relations for the structure, paths as naming. Running both as structure gives two hierarchies that drift apart.

### 5. Meaning and structure split

Immutability decides where relations live. If a concept entry lists its parts, adding a part means superseding it. Hence the split: concept entries hold meaning, annotations hold structure, both membership and the placement of concepts inside concepts, so structure can change at any time without rewriting meaning. Christopher: "sounds reasonable so far", to be discussed further.

### 6. Explicit and emergent

Raised by Christopher: explicit through annotations or direct topic assignment; but how do concepts emerge, by algorithm and a procedure that identifies new clusters?

- Explicit: a label at capture, an annotation later, a concept named in dialogue. Needs engine annotation capture first (20260722-141909-s-tac-6rd).
- Emergent: the algorithm proposes, dialogue decides. A procedure runs clustering of the kind used this session and compares it with the named concepts, surfacing clusters with no name, entries that look like members but lack the label, concepts that drifted, split or merged, and labels nobody defined. The user picks what to name; the result lands as concept entries and annotations. Naming is what makes a cluster durable; later runs measure drift against it. This follows the intake principle that candidates become entries only through dialogue (20260603-172628-d-cpt-fbi, 20260410-205013-s-cpt-bn1). The clustering would have to be an engine capability, because served procedures may only use engine capabilities (20260707-134311-d-cpt-476).

### 7. The carrier

A fact with a declared class, the pattern of 20260904-141736-d-cpt-xob; an annotation allowed a body; or a new signal kind. A signal in each case, so a concept commits nothing.

## The clustering run, as an instance

- `sdd export` gave every entry with derived status and heat; 227 open entries were in scope.
- Summaries were embedded with the search index's embedding model.
- The weighted graph combined direct references, shared neighbours damped by neighbour degree, and the nearest semantic neighbours above a cosine threshold; Louvain community detection gave 17 communities, curated into 26 named clusters with maturity marks.
- The names were then replaced with the graph's own wording, because Claude's names were not understandable.
- The scripts and the resulting overview stay in the local, uncommitted `.sdd/tmp/` directory of the session's checkout.
