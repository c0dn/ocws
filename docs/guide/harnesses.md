# Harnesses

## Destination tokens

Pack destinations can use these tokens, which resolve per harness:

| Token | OpenCode | Claude Code | Codex CLI |
| --- | --- | --- | --- |
| `{agents}` | `.opencode/agents` | `.claude/agents` | `.codex/agents` |
| `{skills}` | `.opencode/skills` | `.claude/skills` | `.agents/skills` |
| `{commands}` | `.opencode/commands` | `.claude/commands` | not available |
| `{tools}` | `.opencode/tools` | not available | not available |
| `{mcp}` | `opencode.json` | `.mcp.json` | `.codex/config.toml` |
| `{config}` | `opencode.json` | `.claude/settings.json` | `.codex/config.toml` |
| `{dir}` | `.opencode` | `.claude` | `.codex` |

A file whose destination uses a token the harness lacks is an error, so put
such files under `targets` for the harnesses that support them.

## Instructions

ocws writes `AGENTS.md` from the profile's guide. For Claude Code it also
writes a `CLAUDE.md` that imports it with `@AGENTS.md`, so all three
harnesses share one instruction file.

## Detecting harnesses in use

When `--harness` is not given, ocws uses `default_harnesses` from the config,
then the harnesses already present in the workspace, then `opencode`:

| Harness | Detected by |
| --- | --- |
| OpenCode | `opencode.json`, `opencode.jsonc`, `.opencode/` |
| Claude Code | `CLAUDE.md`, `.claude/`, `.mcp.json` |
| Codex CLI | `.codex/`, `.agents/skills/`, `AGENTS.override.md` |

## Harness notes

- **Claude Code** asks you to approve project MCP servers from `.mcp.json` on
  first use. Commands are best shipped as skills with
  `disable-model-invocation: true`.
- **Codex CLI** only loads `.codex/config.toml` and `.codex/agents/` after you
  trust the project. Skills live in `.agents/skills/`; add
  `agents/openai.yaml` with `allow_implicit_invocation: false` for
  command-style skills.
- **OpenCode** custom tools (`.ts` files in `.opencode/tools`) have no
  equivalent elsewhere, so tool packs are OpenCode-only and are reported as
  skipped for other harnesses.
- Permissions are not translated between harnesses. Each harness gets the
  header or config you write for it.
