# Harnesses

ocws supports 17 agent harnesses. Templates are written once for OpenCode (V2);
every other harness gets files **derived** from them when a pack is planned, so
packs need no per-harness copies. A pack can still override any harness with an
explicit `targets.<harness>` entry (see [Packs](/templates/packs)).

ocws only writes project files. Harnesses that keep MCP servers in global
config (Goose, Cline, Hermes) get skills, agents and commands only.

## Support matrix

| Harness | `--harness` | Agents | Commands | Skills | MCP | Instructions |
| --- | --- | --- | --- | --- | --- | --- |
| OpenCode (V2) | `opencode` | `.opencode/agents` | `.opencode/commands` | `.opencode/skills` | `opencode.json` `mcp` | `AGENTS.md` |
| OpenCode V1 | `opencode-v1` | `.opencode/agents/*.md` | `.opencode/commands/*.md` | `.opencode/skills` | `opencode.json` `mcp` | `AGENTS.md` |
| Claude Code | `claude` | `.claude/agents/*.md` | as skills | `.claude/skills` | `.mcp.json` `mcpServers` | `CLAUDE.md` → `AGENTS.md` |
| Codex CLI | `codex` | `.codex/agents/*.toml` | as skills | `.agents/skills` | `.codex/config.toml` `[mcp_servers]` | `AGENTS.md` |
| Gemini CLI | `gemini` | `.gemini/agents/*.md` | `.gemini/commands/*.toml` | `.agents/skills` | `.gemini/settings.json` `mcpServers` | `GEMINI.md` → `AGENTS.md` |
| Qwen Code | `qwen` | `.qwen/agents/*.md` | `.qwen/commands/*.md` | `.qwen/skills` | `.qwen/settings.json` `mcpServers` | `AGENTS.md` |
| GitHub Copilot CLI | `copilot` | `.github/agents/*.agent.md` | as skills | `.agents/skills` | `.mcp.json` `mcpServers` | `AGENTS.md` |
| Cursor CLI | `cursor` | `.cursor/agents/*.md` ¹ | as skills | `.agents/skills` | `.cursor/mcp.json` `mcpServers` | `AGENTS.md` |
| Factory Droid | `droid` | `.factory/droids/*.md` | `.factory/commands/*.md` | `.agents/skills` | `.factory/mcp.json` `mcpServers` | `AGENTS.md` |
| Kiro CLI | `kiro` | `.kiro/agents/*.md` | `.kiro/prompts/*.md` | `.kiro/skills` | `.kiro/settings/mcp.json` `mcpServers` | `AGENTS.md` |
| Amp | `amp` | — | as skills | `.agents/skills` | `.amp/settings.json` `amp.mcpServers` | `AGENTS.md` |
| Crush | `crush` | — | `.crush/commands/*.md` | `.agents/skills` | `.crushrc` `mcp add` | `AGENTS.md` |
| Goose | `goose` | `.agents/agents/*.md` | as skills | `.agents/skills` | — | `AGENTS.md` |
| Cline CLI | `cline` | — | as skills | `.agents/skills` | — | `AGENTS.md` |
| Kilo Code CLI | `kilo` | `.kilo/agents/*.md` | `.kilo/commands/*.md` | `.kilo/skills` | `kilo.json` `mcp` | `AGENTS.md` |
| pi | `pi` | — | `.pi/prompts/*.md` | `.agents/skills` | `.pi/mcp.json` `mcpServers` | `AGENTS.md` |
| Hermes Agent | `hermes` | — | as skills | `.agents/skills` | — | `AGENTS.md` |

¹ Cursor also loads `.claude/agents` and `.codex/agents`, so its own agent
copies are skipped when Claude Code or Codex is selected too.

"as skills" means OpenCode commands become user-invoked skills
(`<skills>/<name>/SKILL.md`). A `—` means the harness has no project-level
location for that kind of file; those files are skipped with a warning.

## How files are derived

| OpenCode source | Derived as |
| --- | --- |
| Agent (`.opencode/agents/x.md`) | Markdown agent with `name` and `description` frontmatter and the same body; Codex gets TOML with `developer_instructions`. OpenCode `permissions` are not translated. |
| Command (`.opencode/commands/x.md`) | Markdown or TOML command, or a skill. `$ARGUMENTS` becomes `{{args}}` for Gemini and Qwen. |
| Skill directory | Copied to the harness skills directory. |
| MCP fragment (`opencode.json`, `/mcp/servers/<name>`) | The harness's MCP file and key, with env references (`{env:X}`) rewritten to the harness syntax. Other `opencode.json` keys (permissions, agents) are OpenCode-only. |
| Other `.opencode/` files | The harness config directory. |
| Custom tools (`.opencode/tools/*.ts`) | OpenCode V1 loads them as-is; OpenCode V2 gets a generated plugin per file (below). The pack is skipped for other harnesses. |

Harnesses that read `.agents/skills` share one copy. Each harness still records
it in the manifest, so removing one harness keeps the files for the others.
Claude Code and Copilot CLI share `.mcp.json` the same way.

## OpenCode V2 and V1

`opencode` is OpenCode V2 and is the authoring format. `opencode-v1` installs
the same packs lowered to the V1 format, following the OpenCode team's
compatibility table:

| V2 | V1 |
| --- | --- |
| `permissions: [{action, resource, effect}]` | `permission` map (`shell` → `bash`, `subagent` → `task`; resources become patterns) |
| `mcp.servers.<name>` with `disabled` | `mcp.<name>` with `enabled` |
| `model: provider/model#variant` | `model` plus `variant` |
| `agents`, `commands`, `plugins`, `snapshots` | `agent`, `command`, `plugin`, `snapshot` |
| `skills: [paths and URLs]` | `skills: {paths, urls}` |

The profile's `opencode.json` template is lowered too. V2 reads V1-shaped
files, but V1 rejects V2 `permissions`, so pick `opencode-v1` for any
workspace still used with V1. The two cannot be selected together because
they write the same files. Kilo Code, an OpenCode V1 fork, uses the same
lowering into `.kilo/` and `kilo.json`.

### Custom tools on V2

Custom tools (`.opencode/tools/*.ts` using `tool()` from `@opencode-ai/plugin`)
are a V1 API, and OpenCode V2 no longer loads that directory. For `opencode`,
ocws installs the tool files unchanged and adds one generated plugin per file,
`.opencode/plugins/ocws-tool-<file>.ts`, which:

- installs `@opencode-ai/plugin` into `.opencode/node_modules` on first load if
  it is missing (V1 did this automatically; V2 does not) and adds the same
  `.opencode/.gitignore` V1 writes;
- imports the tool file and registers each tool with the V2 plugin API, under
  V1's names (`<file>` for a default export, `<file>_<export>` otherwise), so
  permission rules keep matching;
- converts the zod `args` to JSON Schema, applies their defaults, and maps the
  V1 tool context (`directory`, `worktree`, `abort`, `metadata`).

V2 exposes plugin tools through its Code Mode catalog (the `execute` tool)
rather than as top-level tools. The first load needs `npm` on `PATH`. Edit the
tool files, not the generated plugins.

V1 tools that explicitly call `context.ask()` fail closed on V2: the bridge
cannot safely translate that approval request yet. Use `opencode-v1` for those
tools. Other custom tools continue to run through V2's normal tool permissions.

## Destination tokens

Explicit pack files can use these tokens, which resolve per harness:

| Token | Meaning |
| --- | --- |
| `{agents}` | Agent directory (table above) |
| `{skills}` | Skills directory |
| `{commands}` | Command or prompt directory |
| `{tools}` | OpenCode custom tools (`.opencode/tools`) |
| `{mcp}` | MCP config file |
| `{config}` | Workspace config file |
| `{dir}` | Harness config directory (`.opencode`, `.claude`, `.gemini`, ...) |

A file whose destination uses a token the harness lacks is an error, so put
such files under `targets` for the harnesses that support them.

## Instructions

ocws writes `AGENTS.md` from the profile's guide. Claude Code and Gemini CLI
also get a `CLAUDE.md` / `GEMINI.md` that imports it, so every harness reads
the same instructions. An existing shim without the import is left alone and
reported.

## Detecting harnesses in use

When `--harness` is not given, ocws uses `default_harnesses` from the config,
then the harnesses recorded in the manifest or present in the workspace, then
`opencode`. OpenCode V1 and V2 share marker files; the installed binary
decides (`opencode2` on `PATH`, or the major version from `opencode --version`).

| Harness | Detected by |
| --- | --- |
| OpenCode (V2) / V1 | `opencode.json`, `opencode.jsonc`, `.opencode` |
| Claude Code | `CLAUDE.md`, `.claude`, `.mcp.json` |
| Codex CLI | `.codex`, `AGENTS.override.md` |
| Gemini CLI | `GEMINI.md`, `.gemini` |
| Qwen Code | `QWEN.md`, `.qwen` |
| GitHub Copilot CLI | `.github/agents`, `.github/copilot-instructions.md`, `.github/skills` |
| Cursor CLI | `.cursor` |
| Factory Droid | `.factory` |
| Kiro CLI | `.kiro` |
| Amp | `.amp` |
| Crush | `.crush`, `.crushrc`, `crushrc`, `.crush.json`, `crush.json`, `CRUSH.md` |
| Goose | `.goosehints`, `.goose` |
| Cline CLI | `.clinerules`, `.cline` |
| Kilo Code CLI | `kilo.json`, `kilo.jsonc`, `.kilo`, `.kilocode` |
| pi | `.pi` |
| Hermes Agent | `.hermes`, `.hermes.md`, `HERMES.md` |

## Trust and approval

Several harnesses ignore project files until you approve them. ocws prints
these as notes after installing:

| Harness | What to do |
| --- | --- |
| Claude Code | Approve project MCP servers from `.mcp.json` on first use. |
| Codex CLI | Trust the project; until then `.codex/config.toml` and `.codex/agents/` are ignored. |
| Gemini CLI | Trust the folder (or `GEMINI_CLI_TRUST_WORKSPACE=true`); untrusted folders skip settings, MCP and commands. |
| Copilot CLI | Answer the folder-trust prompt. |
| Cursor CLI | Approve MCP servers (`agent mcp enable <name>` or `--approve-mcps`). |
| Kiro CLI | Trust the workspace before workspace agents load. |
| Amp | `amp mcp approve <name>` for workspace MCP servers. |
| pi | Trust the project (`pi -a` or the prompt). |
| Hermes Agent | `hermes skills trust` to load project skills. |
| Crush | Nothing, but note that `.crushrc` is Bash that Crush runs at startup; ocws only appends `mcp add` lines and removes exactly those. |

Permissions are not translated between harnesses (except OpenCode V2 → V1).
Tighten derived agents with an explicit target if a harness needs it.
