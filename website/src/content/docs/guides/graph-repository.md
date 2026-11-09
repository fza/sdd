---
title: A graph in its own repository
description: Keep the graph in a repository of its own, beside the repository where the work happens.
---

`graph_dir` may be an absolute path, which puts the graph in a checkout beside the project instead of inside it. The project's history then carries only the work, and every `sdd: …` capture commit lands in the graph's own repository.

```yaml
# project-repo/.sdd/config.yaml
graph_dir: /home/you/src/project-sdd/graph
```

```text
project-repo/          # only work commits
  .sdd/config.yaml
  .claude/skills/      # sdd init commits these here
  src/

project-sdd/           # only sdd: capture commits
  graph/2026/09/...
```

Git operations follow the paths they act on: entry, summary and WIP-marker commits run in the graph's repository, while `sdd init` commits skills and metadata in the project, and `sdd wip start` branches the project. `sdd sync --pull` syncs the graph's repository, and the background sync check reads its upstream. Grooming keeps reading the project's own history for closure evidence, since it runs where the work is.

Two limits are worth knowing. `sdd serve` refuses to start against a graph in another repository: the engine's write path acquires a worktree of the served checkout and commits the graph inside it, so it would write the entry to disk and commit it nowhere. And `repo_id` is derived from the project's remote, so a graph others reference across repos is still announced under the project's identity rather than the graph repository's.
