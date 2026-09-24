# Local changes

Changes carried on top of upstream `networkteam/sdd`. The baseline is upstream `v0.17.0` (merge base `b569dcf6`). Nothing listed here is in upstream `main`.

Section headings are local build stamps, matching the version string the binary reports (`sdd --version`).

## 0.17.0+fza2

### Added

**`--local-config <path>`, a global flag.** Points the machine-local config layer at a file outside the checkout for the whole invocation, in place of `.sdd/config.local.yaml`. A sandbox, a CI runner or a second identity can supply its own layer without writing into the repository.

The passed file keeps the same slot in the config overlay (user-global base, then the committed `.sdd/config.yaml`, then the machine-local layer, then CLI flags), parses as the same `PerRepoConfig` schema, and reports as source `local` in `sdd config` provenance output.

```bash
sdd --local-config ~/ci-local.yaml config get participant
sdd --local-config ~/ci-local.yaml config set --local participant Ada
sdd --local-config ~/ci-local.yaml init
```

Reads and writes move together. `sdd config set --local`, `sdd config unset --local`, and the participant that `sdd init` records all target the passed file, so one invocation resolves and mutates the same layer.

Two alternatives were rejected:

- Redirecting reads while leaving writes on `.sdd/config.local.yaml`. A value written with `--local` would then be invisible to the invocation that passed the flag.
- Redirecting reads and failing `--local` writes while the flag is set. This adds a special-case rule and leaves `sdd init` unable to record a participant.

Accepted cost: the flag carries write authority, and a mistyped path is not an error. It resolves as an absent layer on reads and is created with mode 0600 on first write.

Edge behavior: outside a `.sdd/` directory the passed file still overlays the user-global config, so the flag names a file rather than a location inside a checkout. With neither a `.sdd/` directory nor the flag there is no machine-local layer at all, which keeps an empty directory from reading `config.local.yaml` relative to the current working directory.

`meta.LocalConfigPath` is the single resolution point. It is threaded through `meta.ReadConfig`, `meta.ResolveConfig` and `meta.ReadConfigLayers` on the read side, through `query.EffectiveConfigQuery` and `query.UnknownConfigKeysQuery` for the provenance and unknown-key surfaces, and through `handlers.Options.LocalConfigPath` and `command.InitCmd.LocalConfigPath` on the write side. The CLI parses the root flag once into an absolute path, because config resolution runs in helpers that hold no parsed command.

Files: `internal/meta/meta.go`, `internal/query/config.go`, `internal/finders/config.go`, `internal/handlers/handler.go`, `internal/handlers/configfile.go`, `internal/handlers/handler_init.go`, `internal/command/init.go`, `cmd/sdd/main.go`, `cmd/sdd/config.go`, `cmd/sdd/repo.go`, `README.md`.

Tests: `internal/meta/localconfig_test.go`, `cmd/sdd/localconfig_test.go`, and `TestConfigSet_LocalOverrideWritesOverrideFile` in `internal/handlers/handler_config_test.go`.

## 0.17.0+fza1

### Added

**`sdd new --preflight-verified`.** Skips the pre-flight LLM call and leaves no trace: no `preflight:` frontmatter annotation and no stderr warning. It is for the real capture of an entry whose findings a `--dry-run` pass already settled.

Pre-flight is an LLM call and non-deterministic, so a second run over identical text can raise findings the dry-run loop already cleared, sending the capture through another revision round for no gain. `--skip-preflight` records the bypass in the entry, which misstates a capture that was in fact validated.

```bash
sdd new decision tactical "..." --dry-run      # settle findings
sdd new decision tactical "..." --preflight-verified
```

Mutually exclusive with `--skip-preflight`. Passing both is an error, since one records the bypass and the other does not. The flag is only correct when the dry-run covered the same text; a revised entry needs a fresh dry-run.

Files: `cmd/sdd/main.go`, `internal/command/new_entry.go`, `internal/handlers/handler_new_entry.go`, `internal/bundledskills/templates/sdd/SKILL.md.tmpl`, `internal/bundledskills/templates/sdd/references/cli-reference.md.tmpl`, and the installed skill renders under `.claude/skills/` and `.agents/skills/`.

Tests: `internal/handlers/handler_new_entry_test.go`.

## Fixes

None. Every local change is additive.
