package mcpapp

// Server-level instructions and the few fixed instruction snippets the shell
// itself contributes. This is knowledge tier zero — the connection handshake:
// the minimum an agent needs to not misread a read, plus the doors. It must
// survive truncation (some clients render instructions poorly), so it stays
// short and front-loads what matters. Tier one is the session shell's opening
// serve (the user-dialogue base procedure); tier two is the per-step units.

const serverInstructions = `This server hosts an SDD (Signal-Dialogue-Decision) graph: an append-only
record of project observations (signals) and commitments (decisions). It grows
through dialogue between the user and their agents.

Read it right:

- Every entry is a signal (something noticed) or a decision (something
  committed to).
- Entries are immutable. Current status is derived from the graph, never
  edited in place.
- An entry's one-line summary is a pointer, not a fact. Read entries in full
  (show) before relying on them.

Everything else is served.

Two doors open a dialogue:

- start_session opens a fresh one under a new handle. It returns the opening
  serve: your orientation, how the dialogue should feel, and the moves you can
  start.
- resume_session re-serves the current position of a session whose handle you
  present.

Project:

- start_session is the only tool that takes a project.
- One server serves every project the principal can reach. A sole project is
  inferred.
- Several exist and none was passed: the response lists them (status
  project-required) and opens no session. Choose with the user, then call
  again with project.

Session handle (the session ID):

- It is the dialogue's identity and its capability.
- It reaches you only by issuance (start_session minted it for you, or a
  dispatch carried it) or because the user handed it over.
- Presenting it is the whole authorization to continue that dialogue.
- Every other tool takes it as a required argument: the work tools
  (start_procedure, next, park, stage_attachment, bind_branch, abandon,
  sync_repos) and the reads (search, view, show, read_attachment, info,
  registry).
- A read runs in its session's home project and branch unless it names
  another project.
- A move or a read may work in a project the home declares as a dependency,
  directly or through its dependencies. Pass it as project on start_procedure
  or on the read. Every serve names the project its instance works in.
- Nothing is derived from the connection. A reconnect changes nothing. Closing
  a connection ends nothing.
- Retain the handle across context compaction.
- Reads are free: no move has to be running, and no procedure state gates
  them.

Lost your place (context compacted or summarized) but still hold the handle?
Call resume_session. It re-serves the session's position in full: every
running move at its current step, with the schema to continue it.

Lost the handle itself? Ask the user. Sessions are never listed on this
surface. A handle is never guessed.`

const autonomousResumeInstructions = `Session resumed. Step position and collected evidence persist.
open_instances carries each running instance's current serve. The session
shell (user-dialogue) is among them, carrying the open-threads block.
Continue through next with the session's handle. Lose your place again
later? resume_session with this handle re-serves this list in full.`

const resumeInstructions = `Session resumed. Step position and collected evidence persist.
open_instances carries each running instance's current serve. The session
shell (user-dialogue) is among them, carrying the open-threads block.
Brief the user on where the work stands (procedure, step, goal) before you
continue. Continue through next with the session's handle. Lose your place
again later? resume_session with this handle re-serves this list in full.`
