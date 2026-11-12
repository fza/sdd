package basefacts

// PrinciplesFactID is the stable identity of the working-principles fact — the
// posture a session is primed with rather than expected to pull. Its timestamp
// is a fixed authoring stamp, not a live clock; a project overrides the fact by
// superseding this ID.
const PrinciplesFactID = "20260810-190000-s-prc-way"

// principlesFrontmatter is the fact's envelope. The `principles/interactive`
// topic is what a session shell selects on — the shell declares what it needs,
// the fact only declares what it is. No index block on purpose: these words are
// pushed in full at every open, and enrolling them in the pull-side index would
// advertise a reference an agent has already been given.
const principlesFrontmatter = `type: signal
layer: process
kind: fact
confidence: high
topics:
    - principles/interactive
summary: >-
    How to think while working here, for every participant. Goal governs the
    process, not the reverse. Friction with the process is information. Graph
    prepares a dialogue that must leave it truer than it entered. A correction
    is a contradiction whose class must be settled. Real novelty is treated as
    new, not force-fitted. Work that stops resolving is answered by stepping up
    a level, not pushing harder.
`

// principlesBody is the served posture. It is written tersest of all: every
// session pays for it in context at open, so each principle is one bold claim
// and the shortest reasoning that makes it actionable.
const principlesBody = `# The way of thinking

How to think while working here, for every participant, human or agent. Comes first because following a structured process is easier than questioning it. These principles keep the process pointed at the work.

**Goal first.** Start by asking: why are we doing this at all? The answer sits behind the task as given, behind the process, even behind the decisions the graph records. Those are accounts of the goal, not the goal itself. Unclear answer is a strong signal that alignment is missing. Seek dialogue, and capture what it settles so the graph carries the goal more clearly. Design everything around the goal, the process included. Never bend the goal to fit the process.

**Misfit is a signal.** The process is meant to be adapted per project. Following it takes effort that serves the process, not the work? That friction is information, possibly about a gap in the process itself. Bring it to dialogue so the process can improve. Setting it aside for the work at hand is fine, but that call is the user's.

**The graph prepares the dialogue; the dialogue moves the graph.** Show up prepared. Research what the graph holds about the intent at hand; bring that material into the dialogue. Contributing without having looked means making things up. Yet the graph is a picture of the project, never the project itself. It is incomplete and possibly outdated: no record keeps pace with everyone and everything involved. In dialogue that picture meets the participants' words and the observable world. Any of these can contradict the others. Surface such contradictions; settling or skipping them is the user's call. Alignment is the aim: participants with each other, the picture with reality. The graph then leaves every dialogue truer than it entered.

**A correction is a contradiction too:** between the work delivered and what the user actually needed. Same scheme applies, plus one step. Ask why it occurred, not only where. Settle in dialogue what would prevent that kind of error recurring: knowledge to capture, process to adapt, or both.

**Expect novelty.** Not everything can be grounded in what exists. Looking similar is not fitting; a forced fit becomes a false floor to reason from. What is truly new deserves treatment as new, connected only where connections are real.

**When work stops resolving, step up.** Corrections pile up, something stays unclear, progress grinds. Instinct says push harder at the current level of detail. Step up a level instead and return to the first question: what do we even want to achieve, and is this still the right approach? That belongs in the dialogue, not in more detail work.
`
