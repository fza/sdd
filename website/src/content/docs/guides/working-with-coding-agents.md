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

sdd sends each piece of text in full once: an instruction lane, a framing block, the fields a report schema describes, an entry. A schema's list of required fields comes with every step, so steps that ask for the same fields share one piece. Each piece carries a short tag such as `⟦k3f9⟧` before one of its words. On a later call the agent lists, for each piece it still sees, the tag and the word right after it, as `k3f9:once`, and sdd sends a short pointer instead of the text; an entry comes as its ID and brief with `(held as tag k3f9; next proof k3f9:<the word right after "graph">)`. A proof works once: each pointer names the next one, the word right after a word that occurs once in the piece's text near its tag, never a word such as "the" or "and" that could be guessed, so only an agent that still sees the text can answer it, and a proof kept from an earlier call never holds. After its last challenge, at most three, the piece comes back in full with a fresh tag. Proofs never carry over from one call to the next: an agent that lists only the tags of its last result gets every piece served before that in full again, such as the type-system overview at each new capture. So every result names, in its `unproved` field, the pieces earlier results sent in full that the agent has not proved since, the newest 20 at most, each by bare tag and name, such as `tag k3f9 for lane open`, and asks the agent to prove each one whose text it still sees. A piece leaves the list once proved or sent in full again. Entry lines in a gather listing are never on it: they always come in full.

When a summary drops a tag or the word after it, that proof fails and the text comes back in full. A proof with the wrong word or a tag sdd never sent also brings a `proof_notice` that names it; a spent proof brings none. A subagent working on the same session holds no tags and gets the full text too. An agent that has lost a piece names it, and sdd sends it in full again. An entry asked for by its ID follows the same proof: a pointer while the call proves the entry, the full entry once the call leaves its tag out. A move waiting for a delegate's delivery, such as a capture waiting for its deep verifier, serves one line per check, with no framing: what it waits for, and to call next again at most every 20 seconds, never waiting with sleep or a timer. A step that holds a report it just took, because fields are missing or a check fails, sends a schema of only the fields that hold it; every other field stays reportable. A step that only launches the verifier, weighs its findings or confirms a summary carries only the session's basic facts (participant, language, search, branch), not the working principles or the recent graph movement. The recent graph movement lists the newest 5 entries. A listing cut by its count or its size ends with a line naming how many more entries matched and the layout that serves them.

No result sdd sends runs past 18,000 characters, counted as the text content the agent receives: the result's JSON. The bound is the same for every agent. Codex prints a result within a budget of 10,000 tokens and drops the middle of a larger one; Claude Code saves a result past 50,000 characters to a file the agent then has to read back. A read too large for one result, such as `show` of many entries, a long attachment or the registry, comes in pages, and each page names the call that serves the next. A step whose instructions would not fit serves its first parts and names the rest. The next call to that step, with or without a report, serves them and points at what the call before sent: its parts, framing, schema and entries. An agent that keeps no proofs still gets every part. A piece the call names in `reread`, or proves with a wrong word, comes in full instead. A move that ends serves the results a caller acts on, such as the entry it wrote, and names the working state it leaves out. The dialogue's own instructions, served again when a move ends or is parked, wait the same way where they would not fit beside the rest. Framing blocks and the vocabulary table wait too, once the instructions are down to their first part, so the opening of a session, its tag guide counted, and a resume fit the bound as well. A resume serves the open moves first; the framing takes the room they leave. A notice that sync is blocked arrives when it starts or changes, not on every result; resuming the session serves it again.

In practice it often works better to avoid compaction in long work. Capture the current state, for example as progress notes on the implementation or a partial done, and continue in a fresh agent session.

## Resume a session in a fresh agent session

A session you did not wrap up stays open. In a new agent session, give the agent the session ID and ask it to resume that session. It receives the open moves at their current steps and can continue an unfinished capture or implementation where the old conversation stopped. Ask your agent for the session ID before you close a conversation you may want to continue.

How best to hand work between agent sessions this way is still being worked out. Capturing the state as entries first is the safer path.

## Parked captures

Under `dialogue.mode: auto`, a capture that a check blocks a second time is parked: nothing is written and the session stays open at that step. The agent ends its turn with `parked at <step>: resume_session <session>`. `sdd sessions` marks it, for example `capture parked at deepUser · 2h ago`, and `sdd sessions --format json` lists every open session with `session`, `procedure`, `parked`, `step`, `reason` and `since`. Your next guided session names each parked capture when it opens. Resume the session in a guided dialogue to answer the step yourself. The agent cannot drop a blocked capture: sdd refuses its abandon of the move, and its end of the session by the session's handle, while the move waits at such a step. You drop it in a guided dialogue, or end the session with `sdd sessions abandon <session>`.

## Decisions an agent takes alone

Under `dialogue.mode: auto` no one confirms the playback, so the reason a decision rests on is the agent's own. sdd marks every entry it writes in `auto` as the agent's:

```yaml
decided-by: agent
run-by: Ada              # the participant sdd runs as
participants: [Claude]   # the agent's actor
reason: The flaky step fails one run in ten.   # decisions only
```

The participant is the actor whose canonical or alias matches the name the MCP client that started the capture reports: Claude Code reports `claude-code`, which matches an actor `Claude` with the alias `Claude Code`. sdd never writes the name a client reports. A capture from a client no actor matches is held at its draft, before any check calls a model, with the fix: capture an actor signal for the client first, for example canonical `Claude` with the client's name among its aliases, then report the draft again. The agent may capture that actor itself, in the same session; that capture is not held, and the actor it writes is its own participant. A later attach to the session, such as `sdd new --session`, does not change it. A client that reports no name cannot start or write a capture in `auto`; sdd refuses it and tells the client to send a name. Its session, its reads and its abandon still work. The agent reports no participants itself, and a decision without its own `reason` does not pass the draft. An entry without `decided-by` is a person's.

A decision the agent drafted in `auto` holds no words of yours. When you confirm or write it in a guided dialogue, after a park or after you switched the mode, the capture returns to the draft and asks for your own reason, and for a directive whether it applies now or is work still to be done. With your words the draft is played back once more and written as yours, without `decided-by`.

An agent alone writes every signal kind, and plans, activities and pending directives on the tactical and operational layers. It never supersedes or closes an entry a person decided; a done may close any. Any other draft parks for you before a check runs: every strategic, conceptual and process decision, a guiding or settled directive, an aspiration, contract, role, focus or procedure, and an approval. The park reason names the rule, for example `agent limit: a conceptual decision takes a person`, and `sdd sessions` lists the capture parked at `agentLimit`. Resume the session in a guided dialogue: you adjust the draft with your own reason and it is written as yours, or you drop it.

An agent's entry built on another unapproved agent entry through `grounded-in`, `builds-on`, `refines` or `depends-on` carries the mark on: `sdd view` and `sdd show` name its origin, as in `{agent-reasoned via d-tac-agt, 2 steps}`. A person's entry is never marked and stops the chain. The catch-up lists each chain still waiting for review.

To review a chain, approve or dismiss it in a guided session or with `sdd new --approves`. An approval is a settled directive that lists the entries it vouches for:

```yaml
kind: directive
intent: settled
approves:
    - 20261117-101500-d-tac-agt
```

An approved entry loses its mark and stops the chain: an agent's entry built on it starts a chain of its own. A dismissal is a settled directive that closes the agent's entries; the catch-up then drops a chain whose start it closed. In `auto` both park for you.

## Entries you approve

Under `dialogue.mode: assisted` the agent works as in `auto` up to one final gate per entry. It drafts the capture, runs every check, and answers each step itself. Once the writing guide, pre-flight and the verifier have passed, the draft is sealed and played back to you once, through your agent's question tool: `AskUserQuestion` in Claude Code, `request_user_input` in Codex where its tools list it. Every choice sdd puts to you goes the same way, in `guided` too. Your answer is relayed verbatim, as in a guided dialogue. The agent cannot answer that step, and it cannot drop a sealed draft; `abort` is yours.

Your approval binds to the draft you saw. Any change after it, such as an `adjust`, sends the draft back through the checks and plays it back again. The entry is then yours, without `decided-by`:

```yaml
approved-by: Ada         # the participant sdd runs as
participants: [Ada]
reason: The flaky step fails one run in ten.   # decisions only
reason-by: Ada           # decisions only
intent-by: Ada           # directives only
```

A decision always carries your reason. When your answer states why, your words are the reason. When you only approve, as with `ok`, the agent asks you for the reason before it confirms: you pick its drafted reason, shown in full, or write your own. For several drafts it offers one reason for all, a reason per entry, or one you write, which applies to every entry unless you name which. The reason you pick or write is relayed verbatim and stored as yours; sdd refuses an approval of a decision without it. The playback names a directive's intent plainly: `pending` needs follow-up, `guiding` is a standing guideline, `settled` is closed when written. Approving adopts it as yours; changing it is an `adjust`. No agent limit applies, since you approve every entry. An approved entry is never marked as agent reasoning and stops a chain as a person's entry does.

The agent may hold several sealed drafts and play them back together. One answer settles them all: the agent answers one draft's playback and names the others in `batch`, by the choice each takes. In "all except 3", the others take `confirm` and draft 3 takes `adjust`: it returns to the draft and waits there until the agent revises it, then is checked and played back again. A reason in the answer applies to every draft taking the same choice; a draft listed as `{"instance": "<id>", "reasonWords": "..."}` carries its own. Each approval still binds to its own draft.

A sealed draft you never answer waits at its playback, parked. Nothing is written. `sdd sessions` lists it as parked at `playback`, and every `assisted` session opens by naming the drafts other sessions left waiting, so you approve, batch or drop them. Resume the session in `assisted` or `guided` to answer it yourself. Resumed in `auto`, the agent confirms it as its own, with `decided-by: agent`, and a draft past the agent limit parks at `agentLimit`, named there as drafted in `assisted`.

## Wrap up sessions

At a natural stop, the agent offers to wrap up and names every thread that is still open. Wrapping up ends the session for good: an open thread left behind cannot be resumed afterwards. Finish the threads worth finishing, drop the others with a reason, and keep the session open if something should continue. Leaving without wrapping up costs nothing; the session stays resumable by its ID.

## Deep verification needs Claude Code

`verify.mode: deep` has a second agent, run by Claude Code, check each draft. In Codex, a session under `deep`, or under a kind set to `deep` in `verify.kinds`, fails to start or resume and names the setting. For a project you work on in Codex, set it to `prose`, `grounding` or `off`. `verify.runner: codex` no longer exists.

## In the dialogue

- **Read the playback.** Confirming writes exactly what the agent showed. Ask for changes until it says what you mean.
- **Override the pre-flight check only on purpose.** A serious finding stops the write; overriding is your call alone and is recorded on the entry.
- **Say it in your own words.** The agent turns it into an entry and asks when something is unclear. You don't need to know the [entry kinds](/sdd/reference/entry-kinds/) or the [working modes](/sdd/working-modes/) by name.
