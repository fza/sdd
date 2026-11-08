# Reference implementation B: a draft verifier as a Claude Code subagent

A project that records its decisions in sdd built a verifier the drafting agent runs as a subagent. This report states how it works and what it measured, as far as it bears on sdd.

## 1. Start

1. The drafting agent spawns a Claude Code subagent defined in `.claude/agents/<verifier>.md`: model sonnet, effort high, tools Read, Grep, Glob, Bash, Write. No URLs, no web search.
2. Bash is limited to `sdd search`, `sdd show`, `sdd view`, a hash command, `cp` of the draft to the proposal, a prose linter (Vale, line output) on the proposal, and read-only git and `ls`.
3. Writes only to `.tmp/drafts/<name>.proposal.md` and `.tmp/drafts/<name>.verified`.
4. Scope: only directives, plans and contracts on the tactical or operational layer get verified.
5. One verifier per topic, not per draft. A launch hook gates the start.

## 2. Pause and resume

1. A subagent cannot wait inside a run. Each round ends with its report.
2. The drafter continues the same agent through Claude Code's `SendMessage` with its agent ID, so the transcript is the context. Answers, a handed-back draft, or the next draft of the topic travel the same way.
3. The drafter keeps topic name and agent ID in its own notes.
4. When the verifier's context runs short, it says so, and the drafter briefs a fresh one with the drafts as they stand.
5. A briefing holds facts only, never conclusions: topic, draft paths, branch or worktree per changed file, full IDs of refined or built-on entries, the owner's settled decisions quoted, chains already fetched.

## 3. Formats

No severity field. Instead `check` names one of five checks (1 missing information, 2 open questions, 3 traps or blockers, 4 contradictions, 5 the enclosing work item), and `owner` says whether only the owner can settle it (`yes` gets no fix).

```
PASS <draft> <hash>
FINDINGS <draft> round <n>
<number> check=<1-5> owner=<yes|no>
  quote: "<draft passage>"
  problem: <one sentence>
  evidence: <file:line, entry ID or quoted command output>
  lines: <proposal lines the fix changes, or none>
```

Contest answers, once per finding: `<n>: accepted` or `<n>: contested, <evidence>`; the evidence must be a `file:line`, an entry ID or quoted command output. The verifier opens what a contest cites. A holding contest drops the finding and its change; a failing one keeps it with a one-sentence reason.

## 4. Proposal

1. The verifier copies the draft as it stands to `<name>.proposal.md`, writes every fix into that copy, and runs the prose linter on it until clean. Fix text never travels in the report.
2. No named or numbered hunks: the pointer is the `lines:` field (proposal line numbers) plus the `quote:` of the draft passage.
3. The drafter reviews all changes once as `git diff --no-index --word-diff=plain <draft> <proposal>`.
4. Adopt: with no finding open, the verifier writes the verdict, and the drafter moves the proposal over the draft, so no proposal outlives its round.
5. The draft is never edited while a proposal waits.

## 5. Rounds

1. Counted per draft by the verifier.
2. Agreement: no finding left open (all accepted, or contested and dropped).
3. After three rounds with findings on one draft, the verifier stops and lists its open findings, each with evidence and why it stays open. The drafter puts them to the owner.
4. Rounds after the first check only changed passages and the findings they answer.
5. Verdict: the SHA-256 of the body after the second `---`, written as `<name>.verified`. A capture hook refuses the playback confirm while the body hash matches no verdict, and refuses partial body patches.

## 6. Measured

1. No counters, cost or wall time recorded.
2. Evaluation on a trial draft: the verifier caught all planted flaws plus further real traps.
3. Failure modes found, all fixed: the capture hook failing open on a crash; seeded entries wrongly refused; continuation through `SendMessage` undocumented, and the prompt contradicting itself on writes.
4. About nine verified drafts so far. The proposal-copy and word-diff flow has not yet run on a real draft.

## 7. What its maintainer would change

1. Stable finding IDs across rounds, so a reopened finding is traceable.
2. Hunk labels in the proposal tied to finding numbers, so `lines:` survives edits.
3. A harness-neutral continue mechanism instead of Claude Code's `SendMessage`.
4. Record findings, accept and contest counts, rounds and time per draft.
