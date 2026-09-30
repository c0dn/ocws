# profiles.json

```json
{
  "$schema": "https://c0dn.github.io/ocws/schema/profiles.schema.json",
  "schemaVersion": 3,
  "profiles": {
    "webapp": {
      "displayName": "Web app",
      "description": "JavaScript/TypeScript web applications.",
      "detect": { "priority": 20, "paths": ["package.json", "vite.config.*"] },
      "workspaceConfig": {
        "opencode": "workspace-configs/default/opencode.json",
        "claude": "workspace-configs/default/settings.json",
        "codex": "workspace-configs/default/config.toml"
      },
      "guide": "guides/webapp.md",
      "starterFilePack": "packs/starter/webapp/manifest.json",
      "scaffold": {
        "dirs": ["docs/decisions"],
        "files": [{ "path": "docs/decisions/README.md", "source": "scaffolds/webapp/decisions-README.md" }]
      },
      "basePacks": [
        { "id": "reviewer", "manifest": "packs/agents/reviewer/manifest.json" }
      ],
      "capabilityPackGroups": [
        {
          "id": "browser",
          "displayName": "Browser automation",
          "selectionMode": "zero-or-one",
          "packs": [
            {
              "id": "playwright",
              "manifest": "packs/mcp/playwright/manifest.json",
              "recommendWhen": { "paths": ["playwright.config.*"] }
            }
          ]
        }
      ]
    },
    "general": { "detect": { "fallback": true }, "guide": "guides/general.md" }
  }
}
```

Profile ids are the keys of `profiles`. Their order is the order the wizard
lists them in.

## Profile fields

| Field | Description |
| --- | --- |
| `displayName`, `description` | Shown in the wizard and in plans. |
| `detect` | Detection rules, below. Without it the profile can only be chosen with `-p` or in the wizard. |
| `guide` | `AGENTS.md` template. Its first `# ` heading is replaced with the project name. |
| `workspaceConfig` | Config template merged into each harness's config file. A plain string means the OpenCode config. |
| `starterFilePack` | Pack manifest installed only when `--starter` is passed. |
| `scaffold.dirs`, `scaffold.files` | Created once if missing; never refreshed or pruned. |
| `basePacks` | Core packs, selected by default. |
| `capabilityPackGroups` | Optional packs, grouped. |

## Detection

```json
"detect": { "priority": 20, "paths": ["package.json", "src/routes/", "**/*.vue"] }
```

- Each pattern in `paths` that matches at least one workspace path adds 1 to
  the score. Patterns are globs relative to the workspace root and support
  `*`, `?`, `[...]`, `{a,b}`, and `**`.
- A pattern ending in `/` only matches directories.
- The scan skips `.git`, `node_modules`, `.venv`, `venv`, `vendor`, `target`,
  `dist`, `build`, `__pycache__`, `.next`, `.cache`, and `.ocws`, and stops at
  6 levels deep.
- The profile with the highest score wins; `priority` breaks ties.
- `"fallback": true` marks the profile used when no profile scores above 0.

## Base packs

```json
{ "id": "reviewer", "manifest": "packs/agents/reviewer/manifest.json", "defaultSelected": true }
```

`defaultSelected` defaults to `true`. `--base a,b` selects exactly those,
`--base none` selects none.

## Capability pack groups

| Field | Description |
| --- | --- |
| `id`, `displayName`, `description` | Group identity, shown as one question in the wizard. |
| `selectionMode` | `multi` (default), `zero-or-one`, or `single` / `exactly-one`. |
| `defaultSelected` | Pack ids selected by default; overrides each pack's `defaultSelected`. |
| `packs[].id`, `packs[].manifest` | Pack id used with `--cap`, and its manifest. |
| `packs[].defaultSelected` | Default `false`. |
| `packs[].recommendWhen.paths` | Globs (same rules as detection). A match makes `ocws detect` recommend the pack and the wizard pre-select it. |

Without `--cap`, `apply` installs each group's defaults. `--cap a,b` selects
exactly those packs and is checked against each group's selection mode;
`--cap none` selects none.

## Workspace config

```json
"workspaceConfig": {
  "opencode": "workspace-configs/default/opencode.json",
  "claude": "workspace-configs/default/settings.json",
  "codex": "workspace-configs/default/config.toml"
}
```

Each file is merged into the harness's `{config}` destination
(`opencode.json`, `.claude/settings.json`, `.codex/config.toml`). OpenCode V1
and Kilo Code fall back to the `opencode` template, lowered to the V1 format. Existing
values in the workspace win. Configure the behaviour with
`apply --config merge|replace|skip`.

## Guide

The guide is Markdown used to build `AGENTS.md`:

1. The first `# ` heading becomes `# <project name>` (`--name`, default: the
   directory name). Text before that heading is dropped.
2. `--description` is inserted under the heading.
3. If the guide has no `## Commands` section, ocws adds one with build and
   test commands detected from `package.json` scripts (npm, bun, pnpm, or
   yarn), `go.mod`, `Cargo.toml`, `pyproject.toml`, `pubspec.yaml`, and .NET
   solutions.
4. Each `--convention` becomes a bullet under `## Conventions`.

`AGENTS.md` is only created when missing unless `--agents replace` is passed.
