# CLI

Run `ocws <command> --help` for the full flag list.

## Global flags

| Flag | Description |
| --- | --- |
| `-C, --workspace <dir>` | Workspace directory (default `.`). |
| `--templates <dir>` | Templates root (default: `$OCWS_TEMPLATES`, `templates` in config, or `<home>/templates`). |
| `--home <dir>` | ocws home (default: `$OCWS_HOME`, `$XDG_CONFIG_HOME/ocws`, or `~/.config/ocws`). |
| `--json` | Machine-readable output. |

## `ocws`

With no command, starts the interactive wizard.

## `ocws init`

Creates the ocws home and fetches templates. Without `--from` it clones the
[starter templates](https://github.com/c0dn/ocws-template).

| Flag | Description |
| --- | --- |
| `--from <git-url\|path>` | Git URL, git checkout, or directory to fetch. |
| `--force` | Replace existing templates; the old copy is moved to `<root>.bak`. |

## `ocws detect`

Prints the detected profile with per-profile scores, harnesses already in
use, and recommended capability packs.

## `ocws plan` / `ocws apply`

`plan` previews; `apply` installs. They take the same flags.

| Flag | Description |
| --- | --- |
| `-p, --profile <id>` | Profile (default: detected). |
| `--harness <list>` | Any of `opencode`, `opencode-v1`, `claude`, `codex`, `gemini`, `qwen`, `copilot`, `cursor`, `droid`, `kiro`, `amp`, `crush`, `goose`, `cline`, `kilo`, `pi`, `hermes` (default: config, then detected, then `opencode`). `opencode` and `opencode-v1` are mutually exclusive. |
| `--base <ids>` | Base packs (default: profile defaults; `none` for none). |
| `--cap <ids>` | Capability packs (default: group defaults; `none` for none). |
| `--starter` | Include the profile's starter-file pack. |
| `--overwrite <policy>` | `safe-refresh` (default), `missing-only`, or `overwrite-approved`. |
| `--prune` | Remove files earlier installs managed that the new plan drops, and uninstall components no longer in the plan. |
| `--config <mode>` | Workspace config: `merge` (default, your values win), `replace`, `skip`. |
| `--agents <mode>` | `AGENTS.md`: `create` (default, only if missing), `replace`, `skip`. |
| `--name`, `--description`, `--convention` | Values used when rendering `AGENTS.md`. |
| `--scaffold` | Create missing scaffold directories and files (default `true`). |
| `--dry-run` | `apply` without writing. |
| `--rebuild-invalid-manifest` | Replace an unreadable `.ocws/manifest.json`. |

Exit codes: `0` success, `1` error, `2` blocked by conflicts (nothing was
written).

## `ocws status`

Lists installed components with installed and template versions and their
refresh state.

## `ocws remove`

`ocws remove <component>...` (aliases `uninstall`, `rm`) uninstalls
components recorded in the manifest. Name them by `id` or `harness:id` as
shown by `ocws status`; a bare id matches every harness.

It deletes the component's files, removes its merged keys from shared config
such as `opencode.json`, and drops it from the manifest. Files and keys still
claimed by another installed component are kept, and directories left empty
are removed. Appended blocks (`.codex/config.toml` TOML, Crush's `.crushrc`
lines) are removed when the file still contains them verbatim. TOML removal also
checks that managed tables are unchanged and unrelated settings retain their
scope; edited tables must be removed by hand.

| Flag | Description |
| --- | --- |
| `--overwrite <policy>` | `safe-refresh` (default) blocks on edited files; `overwrite-approved` removes them anyway. |
| `--dry-run` | Show what would be removed without changing anything. |

Exit codes match `apply`: `2` means something was blocked and nothing changed.

## `ocws templates`

| Command | Description |
| --- | --- |
| `templates path` | Print the ocws home and templates root. |
| `templates update` | `git pull --ff-only` in the templates root. |
| `templates validate` | Check profiles, packs, harness targets, sources, and renders. |
| `templates port <manifest>...` | Generate editable Claude Code and Codex targets for OpenCode packs (other harnesses are derived automatically). |
