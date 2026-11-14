---
name: eval-codex
description: Evaluate an SDD flow in real agent use by driving a Codex agent through `codex app-server` against a scratch repository, playing the user turn by turn, and checking what the sdd tools served against what the agent did. Invoke when a change to procedures, served instructions or the MCP surface needs validation beyond tests, or when the user asks for a Codex evaluation run.
---

# Evaluate an SDD flow with a real Codex agent

Tests prove the mechanism. This skill checks the experience: whether an agent that only reads what the sdd tools serve does the right work, in the right order, on the right branch, and whether the run demands anything the work does not need (20260923-185316-d-cpt-5gk). You play the user; Codex is the agent under test.

## Before a batch

- `codex login` done, or an Opper key for `--provider opper`; `git` and Python 3.11+ on `PATH`.
- Build the sdd under test: `devbox run build` in the checkout whose code you evaluate. The driver uses that checkout's `bin/sdd` when it lives in it; pass `--sdd <path>` to `setup` and `start` otherwise.
- Pick a batch directory in your scratchpad, never inside the sdd repository. Each scenario gets its own subdirectory, `<batch>/<scenario>/repo`, because the sandbox makes the repo's parent writable for sibling worktrees.

## The driver

`driver.py` next to this file, stdlib Python:

```
python3 driver.py setup  <repo> [--task TEXT] [--summary TEXT]   fresh repo: git, sdd init for Codex, a code module, the task as a pending directive
python3 driver.py start  <repo> "<first user message>" [--model M] [--effort E] [--provider opper --env-file F] [--idle-timeout SECONDS]
python3 driver.py say    <repo> "<next user message>"
python3 driver.py stop   <repo>                                   end the run: app-server and sdd server
python3 driver.py log    <repo> [--turn N] [--max CHARS]          everything the sdd tools returned, per call
python3 driver.py status <repo>                                   branches, worktrees, history, WIP markers per branch
```

`setup` prints the anchor directive's ID. `start` and `say` run one turn each and print the sdd tool calls with the served position (procedure, step, missing fields), the shell commands with exit codes, and the agent's final message. The full transcript lands in `<repo>.eval/`.

What the driver fixes for every run:

- **One app-server per run.** `start` spawns a daemon that keeps the app-server, and with it `sdd serve`, up between turns, as a real Codex session does, so verifier rounds and other background work survive the turn that began them. Run `stop` when a run is finished; the daemon also stops itself after `--idle-timeout` seconds without a turn (default 2 h). `say` after that fails: `start` a new run. Its log is `<repo>.eval/daemon.log`.
- **Own Codex home.** `<repo>.eval/codex-home` links your `~/.codex/auth.json` and carries your model settings, nothing else: no plugins, apps, other MCP servers or notify hook, and trust entries and threads stay out of your Codex config. If it warns that `auth.json` is no longer a symlink, Codex refreshed the token into the copy; log in again if your own Codex then complains.
- **Provider.** `--provider opper` runs `gpt-5.3-codex` through Opper's Responses route instead of your login: no `auth.json` link, the key from `OPPER_API_KEY` or the `KEY=VALUE` file `--env-file` names, passed on to the sdd server too. Codex has no metadata for the model under that provider and warns that it falls back to defaults; the run's Codex home sets reasoning effort `medium` unless your settings or `--effort` set one. Runs on Opper compare with runs on your login only roughly.
- **Only this sdd.** The run's single MCP server is the chosen `sdd serve`, started in the repo, its tools pre-approved.
- **Sandbox.** `workspace-write` with no approval prompts; the repo's `.git` and its parent are writable so branches, commits and worktrees work.
- **Codex uses skills, not slash commands.** Start SDD work with `$sdd` in the message, the way a Codex user would.

## Claude Code instead of Codex

`claude_driver.py` next to this file drives Claude Code the same way, one headless `claude -p` per turn, resumed by session id:

```
python3 claude_driver.py setup  <repo> [--task TEXT] [--summary TEXT] [--verify-mode deep] [--verify-runner subagent] [--dialogue-mode M]
python3 claude_driver.py start  <repo> "<first user message>" [--model sonnet|opus] [--effort E] [--permission-mode acceptEdits] [--timeout SECONDS]
python3 claude_driver.py say    <repo> "<next user message>"
python3 claude_driver.py log    <repo> [--turn N] [--max CHARS]  what the sdd tools returned, subagent calls marked (sub)
python3 claude_driver.py transcripts <repo>                      the session's transcript, then its subagents' transcripts
python3 claude_driver.py status <repo>
```

- **Own config directory.** `<repo>.eval/claude-config` is the run's `CLAUDE_CONFIG_DIR`: transcripts, project list and memory land there, not in `~/.claude`. Your login is shared, never copied: `CLAUDE_SECURESTORAGE_CONFIG_DIR` points the credential lookup at your own config. Only project settings load, so the repo's `.claude/` skills and the `sdd_delegate` agent apply, your CLAUDE.md, hooks and plugins do not.
- **Only this sdd.** The single MCP server is the chosen `sdd serve`; its tools, `Bash` and skills run without prompts, edits are accepted inside the repo and its parent, anything else is denied. `AskUserQuestion` is off, so the agent asks in its final message.
- **No daemon.** Background work the agent starts ends with its turn's process.
- **Slash commands.** Start SDD work with `/sdd` in the message, the way a Claude Code user would.
- **Measuring.** Pass each path `transcripts` prints to a script that reads Claude Code session files.

## Playing the user

You are the user, not a coach. The run is only worth something if the agent gets its guidance from what sdd serves.

- Say what a user would say, in plain words, and only what the agent asked for. Give real decisions (which mode, whether the draft is right, whether to merge) when the agent asks for them.
- Never name tools, steps, fields or SDD mechanics, never tell the agent how to proceed, and never pre-approve ("and confirm whatever comes").
- When the agent asks for something the scenario does not cover, answer as a reasonable user would and note it.
- When the agent asks for a report, a Git operation or a confirmation the work did not need, answer it and record it as a finding — that is exactly what the evaluation looks for.
- Stop after about 25 turns without progress, or when the agent is stuck on an error, and report where it stuck.

After each turn read the summary. Use `log` when you judge whether the agent followed what was served, and `status` at the moments that matter: after setup, after the marker should exist, after the done, after the merge.

## Scenario card

Give each run a card:

- **Goal** — what the run shows.
- **Opening message** — the first user message.
- **The user's part** — the decisions the user makes and their words for them.
- **Checks** — what must hold, as observable facts in `status`, `log` or the turn summaries.

The cards for the implementation procedure's run modes are in [references/implementation-modes.md](references/implementation-modes.md).

## Running a batch

Scenarios are independent: dispatch one subagent per card, in parallel. Give each subagent this skill's path, its card, the driver path, its scenario directory and the sdd binary, and have it return the report below. Keep the batch's results in the scratchpad.

## Report

Per scenario:

1. **Outcome** — completed, stuck (where), or aborted.
2. **Timeline** — one line per turn: what the user said, what the agent did (sdd calls, Git commands).
3. **Checks** — each check from the card, held or not, with the evidence: turn number, the served text, the Git state.
4. **Findings** — instructions that were missing, wrong or ignored; demands the work did not need; anything that surprised you. Quote the served text next to what the agent did.

Report what happened; do not fix code, specs or the scratch repo mid-run. What the findings mean is settled with the user afterwards.

Run `driver.py stop <repo>` for each scenario, then remove the batch directory once the findings are recorded.
