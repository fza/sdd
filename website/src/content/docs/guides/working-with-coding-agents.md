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

sdd sends each piece of text in full once: an instruction lane, the fields a report schema describes. A schema's list of required fields comes with every step, so steps that ask for the same fields share one piece. Every response that starts, resumes or advances a session ends with two questions about texts sdd served, one per line in its `question` field, such as `q lane intro: 2 words after "the user first"? | lost`. The next call that advances the session (`start_procedure`, `next`, `park`, `abandon`) must answer both in its `answer` argument, in their order and parted by `;`, as in `reach sdd ; 280`: the words each asks for, or `lost` for one whose text the agent no longer sees. A call without it is refused. A call that fails or is refused answers no question, and its error says so: the next call answers the same questions again. When both answers are right, each piece served before comes as a short pointer. A wrong answer or `lost` to either brings every piece in full, and only text served from then on counts as held, so one full response restores the pointers after any loss. The first question asks about the text sdd served longest ago, counting each text from its latest full copy, so right answers show the agent still holds everything served since. The second asks about another held text, or about another spot of the same text when it holds the only one. A question names a phrase that occurs once in its text and asks for the 2 words before or after it, the word between two phrases, or the number after one. The phrase and the words asked for stand side by side on one line, with nothing between them that an agent might count as a word: no arrow, sign or code span, no entry ID, and no full stop inside the run. Two words asked for never end on a word such as "the", "of" or "on", which invites completing the phrase past them. No question rests on where a sentence ends. sdd asks nothing about what the agent or the person wrote, since a compaction summary quotes it: the draft played back, the exchange with a delegate, the reason a move is held for a person. No phrase occurs in a report, an answer to a choice, the person's words, a park note or a start input, wherever sdd echoes them back. sdd makes the questions when it sends the response and never asks the same question twice in a session. Served text carries no mark for the check.

An agent that starts a new session in the same conversation, as it does when the topic changes, passes the earlier session's handle in `start_session`'s `link`. The new session then counts the texts the earlier one still holds as served before its own, and links chain: a session linking one that links a third carries the texts of both. Its opening comes in full and asks about the oldest of them. After a right answer a text identical to one served there, session handles aside, comes as a pointer. A handle sdd does not find, never issued or removed by the sweep, is refused. A link to a session whose log holds no served texts, as stock sdd or an earlier sdd build writes it, opens the session with every text in full and a line saying the link went unused. A delegate's session links nothing and gets every text in full.

After a compaction the agent answers `lost` where the summary dropped the text, and the text comes back in full. A question pending at the compaction can be answered from the summary, so sdd notices a compaction one call late at most: the next response's fresh questions catch it. `sdd sessions` counts these catches per session, a `lost` or wrong answer right after a short serve to the same caller, as in `1 compaction caught one call late`, and `sdd sessions --format json` lists them as `lateCatches`. A subagent working on the same session saw no question, answers `lost` and gets the full text; the main agent's next right answer still gets the pointers. An agent that has lost a piece names it in `reread`, and sdd sends it in full again. An entry comes in full on every later call, whatever the answer; within one result an entry already sent comes as its ID and brief. A move waiting for a delegate's delivery, such as a capture waiting for its deep verifier, serves one line per check, with no framing: what it waits for, and to call next again at most every 20 seconds, never waiting with sleep or a timer. A step that holds a report it just took, because fields are missing or a check fails, sends a schema of only the fields that hold it; every other field stays reportable. A step that only launches the verifier, weighs its findings or confirms a summary carries only the session's basic facts (participant, language, search, branch), not the working principles or the recent graph movement. The recent graph movement lists the newest 5 entries. A listing cut by its count or its size ends with a line naming how many more entries matched and the layout that serves them.

A framing block goes to each caller once and again only when it changes, whatever the answer. A subagent working on its parent's session sets `caller` to a name of its own on every call and gets each block once. When a move ends or is abandoned, the result lands on the dialogue's base step short: the move's outcome, the step's name with its choices and the moves to start next, not the step's text. A call that ends the move with a wrong answer or `lost` gets the base step's text in full. `resume_session` sends every framing block and the base step in full. It carries a running move's collected values of up to 300 characters whole and names each larger one with its size under `collected_large`; `read_value` returns one whole, in pages where it runs past one result. A finding of the deep verifier comes in full once; after that it is one line with its label, severity, axis, how it was settled and the note, and playback shows the settled findings on one round only.

No result sdd sends is cut or saved to a file by the agent's host. Each result is bounded for the host of the call that receives it, counted as the text content the agent receives: the result's JSON. Claude Code, by its client name `claude-code`, gets up to 45,000 characters, below the 50,000 past which it saves a result to a file the agent then has to read back. When `MAX_MCP_OUTPUT_TOKENS` is set in the environment `sdd serve` runs in, or `sdd serve` gets `--mcp-output-tokens`, that bound drops to 2 characters per token of it, but never below 18,000. Every other host, Codex and Claude Desktop included, gets up to 18,000 characters: Codex prints a result within a budget of 10,000 tokens and drops the middle of a larger one. The deep verifier's reading is paged for the host of the verifier's own call, which may differ from the drafter's. A read too large for one result, such as `show` of many entries, a long attachment or the registry, comes in pages, and each page names the call that serves the next. The same call made again without `page` right after a paged result serves page 1 again, opening with a line that says so and names page 2. A step whose instructions would not fit serves its first parts and names the rest. The next call to that step, with or without a report, serves them and points at what the call before sent: its parts, framing, schema and entries. An agent that answers `lost` still gets every part. A piece the call names in `reread` comes in full instead. A move that ends serves the results a caller acts on, such as the entry it wrote, and names the working state it leaves out. The dialogue's own instructions, served again when a move is parked, wait the same way where they would not fit beside the rest. Framing blocks and the vocabulary table wait too, once the instructions are down to their first part, so the opening of a session, its question guide counted, and a resume fit the bound as well. A resume serves the open moves first; the framing takes the room they leave. A notice that sync is blocked arrives when it starts or changes, not on every result; resuming the session serves it again.

Claude Code kills a server about half a second after it asks it to stop. `sdd serve` over stdio ends its background work at once when asked and exits within 0.4 seconds. Work still running then, such as a sync round's merge, is named in the error it exits with.

In practice it often works better to avoid compaction in long work. Capture the current state, for example as progress notes on the implementation or a partial done, and continue in a fresh agent session.

## Resume a session in a fresh agent session

A session you did not wrap up stays open. In a new agent session, give the agent the session ID and ask it to resume that session. It receives the open moves at their current steps and can continue an unfinished capture or implementation where the old conversation stopped. Where a move has ended, the resume also names the last one and how it ended, for example `Last ended move: capture i_3 (abandoned)`; the other outcomes are `completed` and `cancelled`. Ask your agent for the session ID before you close a conversation you may want to continue.

A session stays with the sdd build that opened it. Upstream sdd skips a session this build opened as unreadable. A session upstream sdd opened resumes here, but a move it started, such as a capture, offers only `abandon`: start the move anew afterwards. Moves you start in that session continue as usual.

How best to hand work between agent sessions this way is still being worked out. Capturing the state as entries first is the safer path.

## Parked captures

Under `dialogue.mode: auto`, a capture that a check blocks a second time is parked: nothing is written and the session stays open at that step. The agent ends its turn with `parked at <step>: resume_session <session>`. `sdd sessions` marks it, for example `capture parked at deepUser · 2h ago`, and `sdd sessions --format json` lists every open session with `session`, `procedure`, `parked`, `step`, `reason`, `since` and, where a compaction was caught one call late, `lateCatches`. Your next guided session names each parked capture when it opens. Resume the session in a guided or assisted dialogue to answer the step yourself. The agent cannot drop a blocked capture: sdd refuses its abandon of the move, and its end of the session by the session's handle, while the move waits at such a step. You drop it in a guided dialogue, or end the session with `sdd sessions abandon <session>`.

## Decisions an agent takes alone

Under `dialogue.mode: auto` the agent works as in `assisted`, without your approval: it infers a directive's intent, and may infer a decision's reason, from the dialogue and the graph, and writes the entry itself. sdd marks every entry it writes in `auto` as the agent's:

```yaml
decided-by: agent
run-by: Ada              # the participant sdd runs as
participants: [Claude]   # the agent's actor
reason: The flaky step fails one run in ten.   # optional, decisions only
```

The participant is the actor whose canonical or alias matches the name the MCP client that started the capture reports: Claude Code reports `claude-code`, which matches an actor `Claude` with the alias `Claude Code`. sdd never writes the name a client reports. A capture from a client no actor matches is held at its draft, before any check calls a model, with the fix: capture an actor signal for the client first, for example canonical `Claude` with the client's name among its aliases, then report the draft again. The agent may capture that actor itself, in the same session; that capture is not held, and the actor it writes is its own participant. A later attach to the session, such as `sdd new --session`, does not change it. A client that reports no name cannot start or write a capture in `auto`; sdd refuses it and tells the client to send a name. Its session, its reads and its abandon still work. The agent reports no participants itself. No entry needs a reason; a directive needs its intent (`pending`, `guiding` or `settled`). An entry without `decided-by` is a person's.

A draft the agent made in `auto` that you confirm or write in a guided dialogue, after a park or after you switched the mode, is written as yours, without `decided-by`. Its drafted reason and a directive's intent become yours, marked `reason-by` and `intent-by` you, as your confirm adopts them. sdd asks you for no reason.

An agent alone writes every signal kind, and plans, activities and pending directives on the tactical and operational layers. It never supersedes or closes an entry a person decided; a done may close any. Any other draft parks for you before a check runs: every strategic, conceptual and process decision, a guiding or settled directive, an aspiration, contract, role, focus or procedure, and an approval. The park reason names the rule, for example `agent limit: a conceptual decision takes a person`, and `sdd sessions` lists the capture parked at `agentLimit`. Resume the session in a guided dialogue: you adjust the draft, with your own reason if you give one, and it is written as yours, or you drop it.

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

Under `dialogue.mode: assisted` the agent works as in `auto` up to one final gate per entry. It drafts the capture, runs every check, and answers each step itself. A check that blocks the draft a second time goes to you instead of parking, and your relayed answer settles it. Once the writing guide, pre-flight and the verifier have passed, the draft is sealed and played back to you once, through your agent's question tool: `AskUserQuestion` in Claude Code and Claude Desktop, `request_user_input` in Codex where its tools list it. Every choice sdd puts to you goes the same way, in `guided` too. Your answer is relayed verbatim, as in a guided dialogue. The agent cannot answer that step, and it cannot drop a sealed draft; `abort` is yours.

Your approval binds to the draft you saw. Any change after it, such as an `adjust`, sends the draft back through the checks and plays it back again. The entry is then yours, without `decided-by`:

```yaml
approved-by: Ada         # the participant sdd runs as
participants: [Ada]
reason: The flaky step fails one run in ten.   # optional, decisions only
reason-by: Ada           # with a reason
intent-by: Ada           # directives only
```

The agent infers a directive's intent from the dialogue and the graph and drafts it, and may draft a decision's reason the same way; no entry needs a reason. The playback shows the reason with the rest of the draft and names a directive's intent plainly: `pending` needs follow-up, `guiding` is a standing guideline, `settled` is closed when written. Your one approval adopts both as yours; there is no separate reason question. To change them, answer with an `adjust`, or resume the dialogue. No agent limit applies, since you approve every entry. An approved entry is never marked as agent reasoning and stops a chain as a person's entry does.

The agent may hold several sealed drafts and play them back together. One answer settles them all: the agent answers one draft's playback and names the others in `batch`, by the choice each takes. In "all except 3", the others take `confirm` and draft 3 takes `adjust`: it returns to the draft and waits there until the agent revises it, then is checked and played back again. A draft is listed by its instance id. Each approval still binds to its own draft.

A sealed draft you never answer waits at its playback, parked. Nothing is written. `sdd sessions` lists it as parked at `playback`, and every `assisted` session opens by naming the drafts other sessions left waiting, so you approve, batch or drop them. Resume the session in `assisted` or `guided` to answer it yourself. Resumed in `auto`, the agent confirms it as its own, with `decided-by: agent`, and a draft past the agent limit parks at `agentLimit`, named there as drafted in `assisted`.

## Wrap up sessions

At a natural stop, the agent offers to wrap up and names every thread that is still open. Wrapping up ends the session for good: an open thread left behind cannot be resumed afterwards. Finish the threads worth finishing, drop the others with a reason, and keep the session open if something should continue. Leaving without wrapping up costs nothing. A session with a move in progress, parked or left mid-step, stays resumable by its ID; one without a move in progress goes once it is untouched for `sessions.retention` ([the daily sweep](/sdd/reference/configuration/#the-daily-sweep)).

When the topic of the conversation changes, the agent starts a new session on its own, without asking, and keeps the earlier one open. When the topic returns, it resumes the earlier session by its ID, which still holds its parked moves and what it read.

## Deep verification needs Claude Code

`verify.mode: deep` has a second agent, run by Claude Code, check each draft. In Codex, a session under `deep`, or under a kind set to `deep` in `verify.kinds`, fails to start or resume and names the setting. For a project you work on in Codex, set it to `prose`, `grounding` or `off`. `verify.runner: codex` no longer exists.

The verifier reads in full only the draft's targets; every other entry of the reading comes as one line it can open, and the entries that close or supersede a target, that a target closes or supersedes, or that its ranking judge ranks as likely bearing come marked for it. In Claude Code, without an `llm_prefilter` block, the judge is Haiku 5.5 through the claude CLI, the same judge that rates the entries gather lists, scoring up to 100 entries a call and fewer where an answer breaks; it starts as soon as the draft passes `assemble` and has `verify.ranking_timeout` (45 seconds) to finish, and what it leaves unscored ranks by similarity to the draft. The reading's header names what ranked it. On a new project the judge has no threshold until it holds its first recall stage: the capture's reading then sets aside only what the judge rates no higher than entries far from the draft (the [far floor](/sdd/reference/configuration/#the-far-floor)), and lists 100 lines of the rest, then 300 by title alone. `sdd serve` calibrates the judge in the background from the graph's own references and the labels your captures add, stage by stage, and stops every scoring call when it quits, keeping the scores already returned; this spends model calls, and `verify.auto_calibrate: false` switches it off. `sdd grounding update` calibrates it at once, and `sdd grounding status` shows where it stands and whether a server is calibrating it. A judge that scores the graph's own references poorly widens the reading, and `sdd grounding status --verbose` and `sdd serve` say so. See [Ranking the deep reading](/sdd/reference/configuration/#ranking-the-deep-reading) and [Background calibration](/sdd/reference/configuration/#background-calibration).

## In the dialogue

- **Read the playback.** Confirming writes exactly what the agent showed. Ask for changes until it says what you mean.
- **Override the pre-flight check only on purpose.** A serious finding stops the write; overriding is your call alone and is recorded on the entry.
- **Say it in your own words.** The agent turns it into an entry and asks when something is unclear. You don't need to know the [entry kinds](/sdd/reference/entry-kinds/) or the [working modes](/sdd/working-modes/) by name.
