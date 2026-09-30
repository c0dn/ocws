# Updating workspaces

Re-running ocws in a set-up workspace refreshes it from the current
templates. The wizard starts from the recorded profile. `ocws apply` without
`-p` detects the profile again, so pass `-p` if detection could pick a
different one.

## What happens to each file

| Situation | Default (`safe-refresh`) |
| --- | --- |
| File missing | Installed |
| Unchanged since the last install, template changed | Updated |
| Already identical to the template | Left alone |
| You edited it | **Blocked** |
| Exists but was never installed by ocws, and differs | **Blocked** |

`apply` always plans first. If any file is blocked, nothing is written and
the command exits with code 2, listing the conflicts.

## Resolving conflicts

- Take the template versions: `ocws apply --overwrite overwrite-approved`.
- Keep your edits: leave that pack out of the run (`--base` / `--cap`), or
  move your change into your templates so it stops being a local edit.
- Only install missing files and leave existing ones alone:
  `ocws apply --overwrite missing-only`. Files whose template changed are
  still reported as blocked.

The wizard asks which of these you want when it finds conflicts.

## Removing dropped files and packs

When a template stops shipping a file, or you deselect or delete a pack, the
old files stay until you pass `--prune` (the wizard asks). Pruning removes
files that are unchanged since ocws installed them, removes merged config
keys that still hold the value ocws wrote, and uninstalls components no
longer in the plan. Edited items block unless `--overwrite overwrite-approved`
is also passed; the wizard offers to remove them anyway.

To uninstall a pack without re-running setup, use
[`ocws remove`](/reference/cli#ocws-remove).

## Config files and AGENTS.md

- Workspace config (`opencode.json`, `.claude/settings.json`,
  `.codex/config.toml`) is merged by default and your existing values win.
  Use `--config replace` or `--config skip` to change that.
- `AGENTS.md` is created only if missing. Use `--agents replace` to
  regenerate it from the profile guide, or `--agents skip`.

## Checking state

```bash
ocws status
```

shows each installed component, its version against the template's version,
and a refresh state such as `current`, `refresh-available`,
`locally-modified`, `refresh-with-local-conflicts`, or `source-missing`.
