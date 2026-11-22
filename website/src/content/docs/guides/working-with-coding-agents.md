---
title: Working with coding agents
description: Practical do's and don'ts for SDD sessions in Claude Code and Codex, covering forks, rewinds, compaction, resuming and wrapping up.
---

When you work with SDD in Claude Code or Codex, two sessions run side by side. Most surprises come from mixing them up.

- **The agent session** is your conversation with Claude Code or Codex. It holds what the agent remembers.
- **The sdd session** is the dialogue the `sdd` server tracks: which moves are running, at which step, and what they have collected so far. `/sdd` opens one, and its session ID is the only thing the agent needs to continue it.

The agent keeps the two in step by carrying the session ID and following what the server serves. The tips below are about the situations where they drift apart.

## Capture what matters

An sdd session is scaffolding, not a record. Only entries in the graph last. Anything worth keeping, a decision, an open point, the state of unfinished work, belongs in an entry. A capture you want to finish later can be started and parked, so it shows up as an open thread instead of living in the agent's memory.

## One sdd session per conversation

- **Don't let two conversations drive the same sdd session.** If you fork or branch an agent conversation, both copies carry the same session ID. Each would advance the same moves while believing it is at its own position. Keep the sdd session in one copy, and run `/sdd` in the other to open a new session.
- **Parallel agents each get their own session.** Several agents can work on one graph at the same time, each in its own sdd session. The graph is append-only and lives in git, so their entries do not overwrite each other.
- **Tell the agent when you switch branches.** The agent declares which branch the session works on, so reads and captures land on the right branch. If you switch branches or move into a worktree yourself, say so.

## Don't rewind past a step sdd has taken

Claude Code and Codex let you jump back in the conversation and continue from an earlier message. The sdd session does not jump back with you: a move that advanced stays advanced, and an entry that was written stays written and committed. After a rewind past such a step, the agent's memory and the session no longer match.

- Rewind only to a point after the last sdd step. A move started before that point keeps running in the session even if the conversation no longer remembers it.
- If you rewound too far, ask the agent to resume the sdd session. It then receives the current position again and continues from there.
- To undo an entry, record a new one that closes or replaces it. Entries are never edited or deleted.

## Compaction

When a conversation grows long, the agent compacts it into a summary. The sdd session survives this, as long as the session ID survives:

- After compaction the agent should resume the sdd session, which serves every running move at its current step together with its instructions.
- If the agent carries on from memory instead, skipping steps or answering for you, remind it: "Resume the sdd session first."
- If the summary lost the session ID, the agent cannot find the session by itself. Give it the ID, or start a new session with `/sdd`.

sdd cannot tell whether a compaction happened, so it repeats a piece of text short only while the agent shows it still holds it. sdd sends each piece in full once: an instruction lane, the fields a report schema describes, the vocabulary table. A schema's list of required fields comes with every step, so steps that ask for the same fields share one piece. Every response that starts, resumes or advances a session ends with one question about a text sdd served, in its `question` field, such as `q lane intro: 2 words after "the user first"? | lost`. The next call that advances the session (`start_procedure`, `next`, `park`, `abandon`) must answer it in its `answer` argument, with the words asked for or with `lost`; a call without it is refused. A right answer brings a short pointer in place of each piece served before. A wrong answer or `lost` brings every piece in full, and only text served from then on counts as held, so one full response restores the pointers after any loss. The question asks about the text sdd served longest ago, counting each text from its latest full copy, so a right answer shows the agent still holds everything served since. It names a phrase that occurs once in that text and asks for the words before or after it, the word between two phrases, the number after one, or the first or last words of its sentence or of the next. sdd makes it when it sends the response and never asks the same question twice in a session. Served text carries no mark for the check.

An agent that starts a new session in the same conversation passes the earlier session's handle in `start_session`'s `link`. The new session then counts the texts the earlier one still holds as served before its own, and links chain: a session linking one that links a third carries the texts of both. Its opening comes in full and asks about the oldest of them. After a right answer a text identical to one served there, session handles aside, comes as a pointer. A handle sdd does not find, never issued or removed by the sweep, is refused. A link to a session whose log holds no served texts, as stock sdd or an earlier sdd build writes it, opens the session with every text in full and a line saying the link went unused. A delegate's session links nothing and gets every text in full.

After a compaction the agent answers `lost` where the summary dropped the text, and the text comes back in full. A question pending at the compaction can be answered from the summary, so sdd notices a compaction one call late at most. A subagent working on the same session saw no question, answers `lost` and gets the full text; the main agent's next right answer still gets the pointers. An agent that has lost a piece names it in `reread`, and sdd sends it in full again. An entry comes in full on every later call, whatever the answer; within one result an entry already sent comes as its ID and brief. Resuming a session sends every piece in full.

```text
1 → start_procedure {canonical: capture, answer: lost}
  ← … instructions, framing and report schema in full …
    question: q lane intro: 2 words after "the user first"? | lost
2 → next {answer: "before drafting"}      right answer
  ← (held: lane intro — served to you before; …)
3 → next {answer: lost}                   text lost in a compaction
  ← … instructions, framing and report schema in full …
```

A framing block goes to each caller once and again only when it changes, whatever the answer. A subagent working on its parent's session sets `caller` to a name of its own on every call and gets each block once. `resume_session` sends every framing block in full.

A step that holds a report it just took, because fields are missing or a check fails, sends a schema of only the fields that hold it; every other field stays reportable. A step that only waits, launches the verifier or confirms a summary carries only the session's basic facts (participant, language, search, branch), not the working principles or the recent graph movement. The recent graph movement lists the newest 5 entries. A listing cut by its count or its size ends with a line naming how many more entries matched and the layout that serves them. A section of whole entry bodies, such as the working principles, serves each body whole or names it with the layout that serves it, never a part of one.

No result sdd sends runs past 18,000 characters, counted as the text content the agent receives: the result's JSON. The bound is the same for every agent. Codex prints a result within a budget of 10,000 tokens and drops the middle of a larger one; Claude Code saves a result past 50,000 characters to a file the agent then has to read back. A read too large for one result, such as `show` of many entries, a long attachment or the registry, comes in pages, and each page names the call that serves the next. The same call made again without `page` right after a paged result serves page 1 again, opening with a line that says so and names page 2. A step whose instructions would not fit serves its first parts and names the rest. The next call to that step, with or without a report, serves them and points at what the call before sent: its parts, framing, schema and entries. An agent that answers `lost` still gets every part. A piece the call names in `reread` comes in full instead. A move that ends serves the results a caller acts on, such as the entry it wrote, and names the working state it leaves out. The dialogue's own instructions, served again when a move ends or is parked, wait the same way where they would not fit beside the rest. Framing blocks and the vocabulary table wait too, once the instructions are down to their first part, so the opening of a session, its question guide counted, and a resume fit the bound as well. A resume serves the open moves first; the framing takes the room they leave.

In practice it often works better to avoid compaction in long work. Capture the current state, for example as progress notes on the implementation or a partial done, and continue in a fresh agent session.

## Resume a session in a fresh agent session

A session you did not wrap up stays open. In a new agent session, give the agent the session ID and ask it to resume that session. It receives the open moves at their current steps and can continue an unfinished capture or implementation where the old conversation stopped. Ask your agent for the session ID before you close a conversation you may want to continue.

How best to hand work between agent sessions this way is still being worked out. Capturing the state as entries first is the safer path.

## Parked captures

Under `dialogue.mode: auto`, a capture that a check blocks a second time is parked: nothing is written and the session stays open at that step. The agent ends its turn with `parked at <step>: resume_session <session>`. `sdd sessions` marks it with the step and the reason:

```text
$ sdd sessions
7f3c2a · idle · last 2026-11-15T03:10:00+01:00
  - 9d1e4b: capture parked at reviseOrOverride · 9h ago · blocked again after 1 unattended revise
```

`sdd sessions --format json` lists every open move, parked or not, with `session`, `procedure`, `parked`, `step` and `since`, the park time or else the last activity, and `reason` for a parked move. Your next guided session names each parked capture when it opens. Resume the session in a guided dialogue to answer the step yourself.

## Wrap up sessions

At a natural stop, the agent offers to wrap up and names every thread that is still open. Wrapping up ends the session for good: an open thread left behind cannot be resumed afterwards. Finish the threads worth finishing, drop the others with a reason, and keep the session open if something should continue. Leaving without wrapping up costs nothing; the session stays resumable by its ID.

## Deep verification needs Claude Code

`verify.mode: deep` has a second agent, run by Claude Code, check each draft. In Codex, a session under `deep`, or under a kind set to `deep` in `verify.kinds`, fails to start or resume and names the setting. For a project you work on in Codex, set it to `prose`, `grounding` or `off`, which work with every agent. Codex cannot run the verifier read-only, does not follow the served instructions reliably, and ends its turn while a verifier round still runs.

## In the dialogue

- **Read the playback.** Confirming writes exactly what the agent showed. Ask for changes until it says what you mean.
- **Override the pre-flight check only on purpose.** A serious finding stops the write; overriding is your call alone and is recorded on the entry.
- **Say it in your own words.** The agent turns it into an entry and asks when something is unclear. You don't need to know the [entry kinds](/sdd/reference/entry-kinds/) or the [working modes](/sdd/working-modes/) by name.
