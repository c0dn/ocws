# ocws

`ocws` sets up agent-harness workspaces from a templates repository. It installs
profile packs (agents, commands, skills, tools, MCP config, starter files) for
**OpenCode**, **Claude Code**, and **Codex CLI**, generates `AGENTS.md`, and
records what it installed in `.ocws/manifest.json` so later runs can refresh
packs safely without clobbering local edits.

It replaces the old `/setup-agent` OpenCode command: the workflow is the same
(detect → choose → audit → install → record), but deterministic and interactive
instead of AI-driven.

## Install

```bash
npm i -g @c0dn/ocws        # or: bunx @c0dn/ocws, npx @c0dn/ocws
go install github.com/c0dn/ocws/cmd/ocws@latest
```

The npm package ships prebuilt binaries as per-platform optional dependencies
(`@c0dn/ocws-linux-x64`, …), so it works with install scripts disabled. A
postinstall fallback downloads the release binary (checksum-verified) only when
the platform package is missing.

## Quick start

```bash
ocws init --from git@github.com:c0dn/ocws-templates.git   # clone templates into ~/.config/ocws/templates
cd my-project
ocws                        # interactive wizard
```

Non-interactive:

```bash
ocws detect                                              # profile, harnesses in use, recommended packs
ocws plan  -p ctf --harness opencode,claude --cap ctfd   # read-only preview
ocws apply -p ctf --harness opencode,claude --cap ctfd --starter
ocws status                                              # installed packs and refresh state
ocws apply …                                             # re-run to refresh; conflicts block with exit code 2
ocws apply … --overwrite overwrite-approved --prune      # take template versions, drop removed files
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
source = "git@github.com:c0dn/ocws-templates.git"   # set by `ocws init`
default_harnesses = ["opencode", "claude"]          # optional wizard/CLI default
# templates = "~/projects/personal/ocws-templates"  # optional: use a checkout directly
```

`ocws templates update` runs `git pull --ff-only` in the templates root;
`ocws templates validate` checks every profile, pack, harness target, source and
render.

A legacy `.opencode/setup-manifest.json` (from the old `/setup-agent`) is read
automatically and migrated to `.ocws/manifest.json` on the next `apply`
(or explicitly with `ocws upgrade`). Hashes are compatible, so nothing is
recopied.

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
"ctf": {
  "displayName": "CTF",
  "detect": { "priority": 30, "paths": ["challenges/", "**/flag.txt"] },   // dir patterns end with /
  "workspaceConfig": "workspace-configs/ctf/opencode.json",               // or { "opencode": …, "codex": … }
  "guide": "guides/agents/ctf-agents.md",                                 // AGENTS.md template; first H1 is replaced
  "starterFilePack": "packs/starter-files/ctf-reference-files/manifest.json",
  "scaffold": { "dirs": ["challenges/pwn"], "files": [{ "path": "challenges/README.md", "source": "scaffolds/ctf/challenges-README.md" }] },
  "basePacks": [{ "id": "ctf-agents", "manifest": "packs/agents/ctf/manifest.json", "defaultSelected": true }],
  "capabilityPackGroups": [{ "id": "ctf-backend", "selectionMode": "zero-or-one", "packs": [
    { "id": "ctfd", "manifest": "packs/tools/ctf/ctfd/manifest.json", "recommendWhen": { "paths": ["…"] } }
  ] }]
}
```

`selectionMode`: `multi`, `zero-or-one`, `single`/`exactly-one`. A profile with
`detect.fallback: true` is chosen when nothing else matches.

### Pack manifest (schema 3)

```jsonc
{
  "schemaVersion": 3,
  "id": "ctf-agents",
  "componentType": "agent-pack",       // command-pack, agent-pack, skill-pack, tool-pack, template-pack, config-template
  "version": "3.5.1",
  "harnesses": ["opencode"],           // harnesses that get the top-level files (default: opencode)
  "files": [{ "source": "ctf-solver.md", "destination": "{agents}/ctf-solver.md" }],
  "targets": {                         // harness-specific files
    "claude": { "files": [{ "source": "ctf-solver.md", "destination": "{agents}/ctf-solver.md",
                            "render": "frontmatter", "header": "harness/claude/agents/ctf-solver.yaml" }] },
    "codex":  { "files": [{ "source": "ctf-solver.md", "destination": "{agents}/ctf-solver.toml",
                            "render": "codex-agent", "header": "harness/codex/agents/ctf-solver.toml" }] }
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
node npm/build.mjs 0.1.0 --targets linux-x64 --pack   # local npm packages in dist/npm
```

Releases: push a `v*` tag. GoReleaser publishes the GitHub release binaries
(`ocws_<os>_<arch>` plus `checksums.txt`), then the workflow publishes the
platform packages and the `@c0dn/ocws` launcher (needs the `NPM_TOKEN` secret).
