# Concepts

## Templates repository

A git repository (or plain directory) holding everything ocws can install. It
has a `profiles.json` at the root and packs underneath. ocws keeps one checkout
in `~/.config/ocws/templates`; `ocws templates update` pulls it. Start from the
[starter templates](https://github.com/c0dn/ocws-template).

## Profiles

A profile describes a kind of workspace, such as `webapp` or `data-science`.
It defines:

- **detection**: file patterns that identify the workspace type
- **guide**: the `AGENTS.md` template
- **workspace config**: settings merged into each harness's config file
- **scaffold**: directories and files created once
- **base packs** and **capability pack groups**

Detection scores each profile by how many of its `detect.paths` patterns match
files in the workspace. The highest score wins, `priority` breaks ties, and
the profile marked `fallback` is used when nothing matches. You can always pick
a profile with `-p`.

## Packs

A pack is a versioned bundle of files with a `manifest.json`: agents,
commands, skills, MCP config fragments, custom tools, or starter files. The
manifest lists each file's source and destination, and optionally different
files per harness.

## Base packs and capability packs

- **Base packs** are the core of a profile. They are selected by default;
  choose a subset with `--base`.
- **Capability packs** are optional add-ons, grouped by purpose. A group's
  `selectionMode` decides how many can be chosen: `multi` (any number),
  `zero-or-one`, or `exactly-one`. Choose them with `--cap`.

A capability pack can declare `recommendWhen.paths`. When a pattern matches,
`ocws detect` lists the pack as recommended and the wizard pre-selects it.
Non-interactive `apply` only installs what `--cap` or the group defaults
select.

## Harnesses

A harness is the agent tool being set up: OpenCode (V2 or V1), Claude Code,
Codex, Gemini CLI, Qwen Code, Copilot CLI, Cursor CLI, Factory Droid, Kiro,
Amp, Crush, Goose, Cline, Kilo Code, pi or Hermes. One workspace can target
several. Packs are written for OpenCode and the files for every other harness
are derived from them; see [Harnesses](/guide/harnesses).

## Workspace manifest

`.ocws/manifest.json` records every installed file with the hash of its
source and of what was written. That is how ocws knows, on the next run,
whether a file is untouched (safe to refresh), edited by you (blocked), or no
longer part of the plan (removable with `--prune`). See
[Workspace manifest](/reference/manifest).
