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

sdd sends each piece of text in full once: an instruction lane, a framing block, the fields a report schema describes, an entry. A schema's list of required fields comes with every step, so steps that ask for the same fields share one piece. Each piece carries a short tag such as `⟦k3f9⟧` before one of its words. On a later call the agent lists, for each piece it still sees, the tag and the word right after it, and sdd sends a short pointer instead of the text. When a summary drops a tag or the word after it, that proof fails and the text comes back in full. A subagent working on the same session holds no tags and gets the full text too. An agent that has lost a piece names it, and sdd sends it in full again. A step that holds a report it just took, because fields are missing or a check fails, sends a schema of only the fields that hold it; every other field stays reportable. A step that only waits, launches the verifier or confirms a summary carries only the session's basic facts (participant, language, search, branch), not the working principles or the recent graph movement. The recent graph movement lists the newest 5 entries.

No result sdd sends runs past 50,000 characters, counted as the text content Claude Code receives: the result's JSON. Claude Code saves a larger result to a file the agent then has to read back. A read too large for one result, such as `show` of many entries, a long attachment or the registry, comes in pages, and each page names the call that serves the next. A step whose instructions would not fit serves its first parts and names the rest. The next call to that step without a report serves them, and points at the parts sent the call before, so an agent that keeps no proofs still gets every part. A notice that sync is blocked arrives when it starts or changes, not on every result; resuming the session serves it again.

In practice it often works better to avoid compaction in long work. Capture the current state, for example as progress notes on the implementation or a partial done, and continue in a fresh agent session.

## Resume a session in a fresh agent session

A session you did not wrap up stays open. In a new agent session, give the agent the session ID and ask it to resume that session. It receives the open moves at their current steps and can continue an unfinished capture or implementation where the old conversation stopped. Ask your agent for the session ID before you close a conversation you may want to continue.

How best to hand work between agent sessions this way is still being worked out. Capturing the state as entries first is the safer path.

## Wrap up sessions

At a natural stop, the agent offers to wrap up and names every thread that is still open. Wrapping up ends the session for good: an open thread left behind cannot be resumed afterwards. Finish the threads worth finishing, drop the others with a reason, and keep the session open if something should continue. Leaving without wrapping up costs nothing; the session stays resumable by its ID.

## In the dialogue

- **Read the playback.** Confirming writes exactly what the agent showed. Ask for changes until it says what you mean.
- **Override the pre-flight check only on purpose.** A serious finding stops the write; overriding is your call alone and is recorded on the entry.
- **Say it in your own words.** The agent turns it into an entry and asks when something is unclear. You don't need to know the [entry kinds](/sdd/reference/entry-kinds/) or the [working modes](/sdd/working-modes/) by name.
