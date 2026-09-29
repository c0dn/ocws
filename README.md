# ocws

`ocws` sets up agent-harness workspaces from a templates repository. It installs
profile packs (agents, commands, skills, tools, MCP config, starter files) for
**OpenCode**, **Claude Code**, and **Codex CLI**, generates `AGENTS.md`, and
records what it installed in `.ocws/manifest.json` so later runs can refresh
packs safely without clobbering local edits.

The workflow is detect → choose → audit → install → record. It is deterministic,
works as an interactive wizard or a scriptable CLI, and never runs an AI model.

## Concepts

- **Templates repo**: a git repository (public or private) containing profiles
  and packs. `ocws` clones it once and pulls updates on demand.
- **Profile**: a workspace type such as `webapp` or `data-science`. It has
  detection rules, an `AGENTS.md` guide, a workspace config and optional
  scaffold files.
- **Pack**: a versioned bundle of agents, commands, skills, tools or config
  fragments, described by a `manifest.json`.
- **Base packs** (`--base`): the packs that make up a profile. They are selected
  by default.
- **Capability packs** (`--cap`): optional add-ons grouped by purpose, such as a
  choice of database MCP server. `ocws detect` recommends them from files in
  the workspace.
- **Harness** (`--harness`): the agent tool you are setting up: `opencode`,
  `claude` or `codex`. One workspace can target several.

## Install

```bash
npm i -g @c0dn/ocws        # or: bunx @c0dn/ocws, npx @c0dn/ocws
go install github.com/c0dn/ocws/cmd/ocws@latest
```

The npm package is a small launcher with no install scripts. On first run it
downloads the binary for your platform from the matching GitHub Release,
checks it against the release `checksums.txt`, and caches it in
`~/.cache/ocws/<version>/` (`~/Library/Caches/ocws` on macOS,
`%LOCALAPPDATA%\ocws\cache` on Windows). Overrides:

- `OCWS_BINARY`: use an existing binary and skip the download
- `OCWS_CACHE_DIR`: change the cache location
- `NODE_USE_ENV_PROXY=1` with `HTTPS_PROXY`: download through a proxy

## Quick start

```bash
ocws init --from https://github.com/you/agent-templates.git   # clone into ~/.config/ocws/templates
cd my-project
ocws                        # interactive wizard
```

Non-interactive:

```bash
ocws detect                                                   # profile, harnesses in use, recommended packs
ocws plan  -p webapp --harness opencode,claude --cap postgres # read-only preview
ocws apply -p webapp --harness opencode,claude --cap postgres --starter
ocws status                                                   # installed packs and refresh state
ocws apply …                                                  # re-run to refresh; conflicts block with exit code 2
ocws apply … --overwrite overwrite-approved --prune           # take template versions, drop removed files
```

`apply` dry-runs the install first; if any file is blocked (local edits,
unmanaged file in the way, conflicting config), nothing is written.

## Locations

| What | Default | Override |
| --- | --- | --- |
| ocws home | `$XDG_CONFIG_HOME/ocws` or `~/.config/ocws` (`%APPDATA%\ocws` on Windows) | `--home`, `$OCWS_HOME` |
| config | `<home>/config.toml` | |
| templates | `<home>/templates` (a git clone) | `--templates`, `$OCWS_TEMPLATES`, `templates =` in config |
| workspace state | `<workspace>/.ocws/manifest.json` | |

`config.toml`:

```toml
source = "https://github.com/you/agent-templates.git"  # set by `ocws init`
default_harnesses = ["opencode", "claude"]             # optional wizard/CLI default
# templates = "~/src/agent-templates"                  # optional: use a checkout directly
```

Private templates repos work with whatever git credentials you already use
(SSH keys, a credential helper, `gh auth setup-git`).

`ocws templates update` runs `git pull --ff-only` in the templates root;
`ocws templates validate` checks every profile, pack, harness target, source and
render.

## Harness mapping

| | OpenCode | Claude Code | Codex CLI |
| --- | --- | --- | --- |
| `{agents}` | `.opencode/agents` | `.claude/agents` | `.codex/agents` (TOML) |
| `{skills}` | `.opencode/skills` | `.claude/skills` | `.agents/skills` |
| `{commands}` | `.opencode/commands` | `.claude/commands` | — (ported to skills) |
| `{tools}` | `.opencode/tools` | — | — |
| `{mcp}` | `opencode.json` | `.mcp.json` | `.codex/config.toml` |
| `{config}` | `opencode.json` | `.claude/settings.json` | `.codex/config.toml` |
| `{dir}` | `.opencode` | `.claude` | `.codex` |
| instructions | `AGENTS.md` | `CLAUDE.md` importing `@AGENTS.md` | `AGENTS.md` |

Codex only loads `.codex/` after you trust the project; Claude Code asks you to
approve project MCP servers. Custom `.ts` tools have no Claude/Codex equivalent,
so tool packs stay OpenCode-only and are reported as skipped.

## Templates format

```text
templates/
├── profiles.json          # registry (schemaVersion 3; 2 is still read)
├── packs/<kind>/<name>/manifest.json
├── guides/…               # AGENTS.md templates per profile
├── workspace-configs/…    # opencode.json / settings.json / config.toml templates
└── scaffolds/…            # files created once by profile scaffolds
```

### Profile (`profiles.json`)

```jsonc
"webapp": {
  "displayName": "Web app",
  "detect": { "priority": 30, "paths": ["package.json", "src/routes/"] },  // dir patterns end with /
  "workspaceConfig": "workspace-configs/webapp/opencode.json",            // or { "opencode": …, "codex": … }
  "guide": "guides/agents/webapp-agents.md",                              // AGENTS.md template; first H1 is replaced
  "starterFilePack": "packs/starter-files/webapp-docs/manifest.json",
  "scaffold": { "dirs": ["docs/adr"], "files": [{ "path": "docs/README.md", "source": "scaffolds/webapp/docs-README.md" }] },
  "basePacks": [{ "id": "webapp-agents", "manifest": "packs/agents/webapp/manifest.json", "defaultSelected": true }],
  "capabilityPackGroups": [{ "id": "database", "selectionMode": "zero-or-one", "packs": [
    { "id": "postgres", "manifest": "packs/mcp/postgres/manifest.json", "recommendWhen": { "paths": ["**/*.sql"] } },
    { "id": "sqlite",   "manifest": "packs/mcp/sqlite/manifest.json" }
  ] }]
}
```

`selectionMode`: `multi`, `zero-or-one`, `single`/`exactly-one`. A profile with
`detect.fallback: true` is chosen when nothing else matches.

### Pack manifest (schema 3)

```jsonc
{
  "schemaVersion": 3,
  "id": "webapp-agents",
  "componentType": "agent-pack",       // command-pack, agent-pack, skill-pack, tool-pack, template-pack, config-template
  "version": "1.2.0",
  "harnesses": ["opencode"],           // harnesses that get the top-level files (default: opencode)
  "files": [{ "source": "reviewer.md", "destination": "{agents}/reviewer.md" }],
  "targets": {                         // harness-specific files
    "claude": { "files": [{ "source": "reviewer.md", "destination": "{agents}/reviewer.md",
                            "render": "frontmatter", "header": "harness/claude/agents/reviewer.yaml" }] },
    "codex":  { "files": [{ "source": "reviewer.md", "destination": "{agents}/reviewer.toml",
                            "render": "codex-agent", "header": "harness/codex/agents/reviewer.toml" }] }
  }
}
```

File fields:

- `render: "frontmatter"` swaps the source's frontmatter for the YAML `header`,
  keeping one shared body.
- `render: "codex-agent"` emits the TOML `header` and adds
  `developer_instructions` from the body.
- `installMode: "merge"` merges a JSON fragment at `jsonPointers`, preserving key
  order. `/permissions` rule lists get pack rules first, so workspace rules still
  win.
- `installMode: "toml-merge"` appends a TOML fragment when its tables are absent,
  is a no-op when they are identical, and blocks on differences. Comments are
  preserved.
- `managed: false` records the file but never refreshes it.

Schema-2 manifests (no `targets`) are OpenCode-only and install unchanged.

### Porting OpenCode packs

```bash
ocws templates port packs/**/manifest.json --harness claude,codex
```

`templates port` generates `targets` and header stubs under `<pack>/harness/`:

- Agents become Claude subagents and Codex agent TOML.
- Commands become skills: `disable-model-invocation: true` for Claude,
  `allow_implicit_invocation: false` for Codex.
- Skills are copied as they are.
- `opencode.json` MCP fragments become `.mcp.json` and `[mcp_servers.*]`.

OpenCode permissions are **not** translated. Review the generated headers and
narrow them with `tools`/`permissionMode` (Claude) or `sandbox_mode` (Codex).

## Development

```bash
go test ./...                                   # unit + end-to-end tests (hash parity uses bun/node when present)
go run ./cmd/ocws --templates testdata/templates plan -C /tmp/x -p dev
node npm/build.mjs 0.1.0 --pack                      # local npm package in dist/npm
```

Releases: push a `v*` tag. GoReleaser publishes the GitHub release binaries
(`ocws_<os>_<arch>` plus `checksums.txt`), then the workflow publishes
`@c0dn/ocws` via npm trusted publishing (OIDC, no token).
