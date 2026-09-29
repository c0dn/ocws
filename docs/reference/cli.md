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
| `--harness <list>` | `opencode`, `claude`, `codex` (default: config, then detected, then `opencode`). |
| `--base <ids>` | Base packs (default: profile defaults; `none` for none). |
| `--cap <ids>` | Capability packs (default: group defaults; `none` for none). |
| `--starter` | Include the profile's starter-file pack. |
| `--overwrite <policy>` | `safe-refresh` (default), `missing-only`, or `overwrite-approved`. |
| `--prune` | Remove files earlier installs managed that the new plan drops. |
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

## `ocws templates`

| Command | Description |
| --- | --- |
| `templates path` | Print the ocws home and templates root. |
| `templates update` | `git pull --ff-only` in the templates root. |
| `templates validate` | Check profiles, packs, harness targets, sources, and renders. |
| `templates port <manifest>...` | Generate Claude Code and Codex targets for OpenCode packs. |
