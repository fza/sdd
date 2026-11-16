---
title: Connected repositories
description: Reference, search and read the entries of another project's graph, such as a shared platform graph, from your own.
---

A decision in one project often rests on one made in another. A service follows the platform team's logging directive; a product builds on a shared framework. When you connect the other repository, entries in your graph can reference its entries directly, and you and your agent can search and read its graph next to your own.

A connection runs one way. Your graph depends on the other graph, and the other graph knows nothing about yours.

## Repository identity

Every graph has a `repo_id`, a URL-shaped name such as `github.com/acme/platform`. `sdd init` derives it from the `origin` remote and writes it to `.sdd/config.yaml`. A repository without an `origin` remote has no `repo_id`, and no other graph can connect to it until it has one.

An entry in another graph is referenced by the graph's `repo_id`, a colon and the entry ID:

```text
github.com/acme/platform:20261107-100000-d-cpt-abc
```

## Connect a repository

In your project, run `sdd repo add` with the other repository's clone URL. SSH and HTTPS URLs both work, and the clone uses your git credentials.

```bash
sdd repo add https://github.com/acme/platform.git
```

The command:

1. clones the repository into an sdd-owned clone under your user data directory, `~/.local/share/sdd/repos/<repo_id>`, or `$XDG_DATA_HOME/sdd/repos/<repo_id>` when that variable is set
2. reads the `repo_id` the repository declares in its `.sdd/config.yaml` and checks it against the URL
3. records the connection in your user-global config, `~/.config/sdd/config.yaml`
4. adds the `repo_id` to `dependencies` in your project's `.sdd/config.yaml` and commits that change as `sdd: repo add <repo_id>`

`sdd repo list` shows every repository connected on your machine, across all your projects, and whether each is cloned.

A clone URL in the `ssh` or `https` form the `repo_id` derives gets both remotes, the given protocol tried first. Any other URL, a host alias or a mirror for instance, is the clone's one remote. The clone is the same kind sdd keeps for a [`graph_repo`](/sdd/reference/configuration/#a-graph-repository-sdd-clones): it belongs to sdd, and a hand edit there is lost.

## What is shared and what stays on your machine

sdd keeps two records:

- `dependencies` in `.sdd/config.yaml` is committed. It lists the graphs this graph references, like the requirements in a `go.mod` file. Everyone who clones the project gets it.
- `repos` in `~/.config/sdd/config.yaml` is yours. It records how your machine reaches each repository: its `repo_id` and the clone URL you chose.

When a colleague clones the project and runs `sdd init`, sdd names each declared dependency that is not connected on their machine and suggests a clone URL. They run `sdd repo add` with the URL they use. The dependency is already declared, so the command only connects it.

The [configuration reference](/sdd/reference/configuration/#connected-repositories) describes both keys.

## A connected graph is what was pushed

sdd never reads another repository's working tree. The clone follows the remote's default branch. An entry recorded in the platform repository reaches your project only after two things happen:

1. Someone commits and pushes it to the platform repository's default branch.
2. Your clone fetches it in its next sync round.

A sync round fetches, merges, and pushes the commits the clone holds, each operation as far as the [git limits](/sdd/reference/configuration/#sync-and-sessions) admit it. These commands start or renew the [lease](/sdd/reference/configuration/#leases) of each clone before reading it, which runs its round when the lease starts past the fetch cooldown; they wait for it at most `read_wait`:

- `sdd search` and `sdd view` with `--repo` or `--all-repos`
- `sdd show` with a full `<repo_id>:<entry-id>`
- `sdd index` with `--repo` or `--all-repos`

If a round fails, for example offline, sdd warns and reads the clone as it stands. `sdd show` with a short ID reads the clone without a round. The `sdd` server your agent works through fetches a clone only while its lease holds, renewed by the calls that work in or read it, and pushes the commits a clone holds with or without one; its `sync_repos` tool runs the round on request. To run the rounds yourself:

```bash
sdd repo sync                            # every connected repository
sdd repo sync github.com/acme/platform   # named repositories
```

`sdd repo sync` prints one line per repository and fails when any line reads `failed` or `refused`:

```text
github.com/acme/platform: synced
github.com/acme/tools: pushed
github.com/acme/infra: skipped (next 14:05)
github.com/acme/docs: failed (authentication with the remote failed, retry 14:20)
github.com/acme/other: refused (not connected)
github.com/acme/app: skipped (this project's own graph; `sdd sync` syncs it)
```

It syncs at most the host's `concurrency` clones at once. Inside a cooldown it runs the fetch or push anyway while the host has a [credit](/sdd/reference/configuration/#git-credits) left, and reports `skipped` once none is. Named, the project's own `repo_id` syncs its `graph_repo` clone, cloning it first when missing; a graph kept in the checkout reports `skipped`, since `sdd sync` syncs it.

The same holds in the other direction. For another project to reference your entries, commit and push them.

`sdd repo sync` only touches the clones sdd keeps. `sdd sync --pull` is a different command, which merges upstream changes into your own repository.

## References across graphs

You rarely type a reference yourself. Your agent drafts references when it records an entry, and you confirm them. Before the entry is written, sdd checks two rules:

- The target repository must be in `dependencies`. If it is not, sdd blocks the entry and names `sdd repo add` as the fix.
- A reference to an entry the new one builds on, such as `grounded-in` or `builds-on`, must point to an entry that exists in the clone. Only `surfaces` and `required-by` may point to an entry that does not exist yet. When the clone lacks the target, sdd runs one sync round and reads the graph again; the entry stays blocked while the target is still missing or the git limits defer the fetch.

The entry file stores the full form:

```yaml
refs:
    - id: github.com/acme/platform:20261107-100000-d-cpt-abc
      kind: grounded-in
      desc: the platform logging directive
```

A reference across graphs links two entries. It cannot close or supersede an entry in the other graph; those links work only within one graph.

`sdd show` takes the full form or a short ID. A short ID resolves when it matches exactly one entry in your graph and its dependencies; when it matches several, sdd lists them.

```bash
sdd show github.com/acme/platform:20261107-100000-d-cpt-abc
sdd show d-cpt-abc
```

## Search across graphs

`sdd search` and `sdd view` read your own graph unless you add `--repo <repo_id>`, which you can repeat, or `--all-repos`:

```bash
sdd search --term logging --all-repos
sdd search --query "how services ship their logs" --repo github.com/acme/platform
sdd view --layout='top(20)' --repo github.com/acme/platform
```

`--all-repos` reaches the dependency closure: your dependencies, their dependencies, and so on. A repository connected on your machine but outside that closure is never read, and naming it with `--repo` is an error. Each repository in the closure needs a connection on your machine; sdd skips one without a connection and warns.

`sdd search` merges the hits from all graphs into one ranked list and prefixes each hit from a dependency with its `repo_id`. `sdd view` renders the layout for your graph, then once for each selected dependency under a `repo:` heading.

Your agent's search and view tools offer the same choice, so you can ask it to look in the platform graph.

### Search by meaning across graphs

Search by meaning compares embeddings, so all graphs must be embedded with the same model. For a search across graphs, sdd embeds every graph, your own included, with the embedding settings in your user-global config. Set them there; without them, sdd uses your project's settings. The [configuration reference](/sdd/reference/configuration/#embedding) lists the keys.

The first search embeds each dependency's entries, which takes a while for a large graph. To build the indexes ahead of time:

```bash
sdd index --all-repos
```

`sdd index --repo <repo_id> --force` rebuilds a dependency's index from scratch. sdd keeps one index per repository and embedding setup on your machine, shared by every project that depends on that repository.

## Capture into a connected graph

Your agent can record an entry in another graph, such as a signal for the platform team. Both sides must allow it, and both settings are off by default:

- On your side, `connected.write`, or `connected.repos.<repo_id>.write` for one repository, in any config file:

  ```bash
  sdd config set --repo github.com/acme/platform connected.write true
  ```

- On the receiving side, `inbound.accept`, or `inbound.repos.<repo_id>.accept` for captures from one repository, in that graph's committed `.sdd/config.yaml`.

sdd checks both when the capture starts, before the dialogue, and refuses with the setting to open. The capture writes through the sdd-owned clone and pushes from there. Its references follow the receiving graph's dependencies, not yours. The [configuration reference](/sdd/reference/configuration/#captures-into-connected-graphs) lists the keys and the refusals.

## Remove a dependency

```bash
sdd repo remove github.com/acme/platform
```

This removes the `repo_id` from `dependencies` and commits the change. It refuses while any entry in your graph references the repository and lists those references. `--force` removes the dependency anyway and names each reference it leaves unresolvable. The connection in your user-global config and the clone stay in place. Once you also delete the repository from `repos` in that config, [`sdd cache prune`](/sdd/reference/configuration/#cleanup) removes the clone after `cache.retention` without a fetch or push, unless it holds unpushed commits. A clone no `sdd repo remove` dropped stays.

## When something does not resolve

| What you see | Cause | Fix |
| --- | --- | --- |
| `entry not found: <repo_id>:<entry-id>`, or a new entry blocked because its target is absent from the repository's default branch | The entry is not pushed, or your clone predates it | Push it in the other repository, then run `sdd repo sync` |
| `[unresolved: repo <repo_id>]` in `sdd show`, or a warning that a dependency is not connected on this machine | Your project declares the dependency, but your machine has no connection | `sdd repo add <clone-url>` |
| `not in the project's dependency closure` | The repository is connected on your machine, but this project does not depend on it | `sdd repo add <clone-url>` in this project |
| `declares no repo_id in .sdd/config.yaml` | The other repository has no identity yet | Add an `origin` remote there, run `sdd init`, commit and push |
| `declares repo_id …, but its clone URL derives …` | The URL points to a fork, a mirror or another repository | Use the URL of the repository that declares that `repo_id` |
| `sdd lint` reports a reference into a repository that is not a declared dependency | The dependency was removed with `--force` | Declare it again with `sdd repo add` |
| Every read warns that a pull failed with a non-fast-forward error | The other repository rewrote its history | Delete the clone directory under `~/.local/share/sdd/repos/` and run `sdd repo sync` to clone it again |
| `dependency <repo_id> has no clone on this machine yet` or `serving checkout is missing` from your agent's `sdd` server | The clone is gone and the git limits defer cloning it again | `sdd repo sync <repo_id>`. The server serves the new clone on the next call |
| `capture into a connected graph refused` | `connected.write` on your side or `inbound.accept` on the receiving side does not allow the capture | Set the key the message names; see [capture into a connected graph](#capture-into-a-connected-graph) |
