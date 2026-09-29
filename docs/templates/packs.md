# Pack manifests

Each pack is a directory with a `manifest.json`. Sources are relative to that
directory.

```json
{
  "$schema": "https://c0dn.github.io/ocws/schema/pack.schema.json",
  "schemaVersion": 3,
  "id": "reviewer",
  "displayName": "Reviewer agent",
  "componentType": "agent-pack",
  "version": "1.0.0",
  "description": "A read-only code review subagent.",
  "harnesses": ["opencode"],
  "files": [{ "source": "reviewer.md", "destination": "{agents}/reviewer.md" }],
  "targets": {
    "claude": {
      "files": [{ "source": "reviewer.md", "destination": "{agents}/reviewer.md",
                  "render": "frontmatter", "header": "harness/claude/reviewer.yaml" }]
    },
    "codex": {
      "files": [{ "source": "reviewer.md", "destination": "{agents}/reviewer.toml",
                  "render": "codex-agent", "header": "harness/codex/reviewer.toml" }]
    }
  }
}
```

## Manifest fields

| Field | Description |
| --- | --- |
| `schemaVersion` | Must be `3`. |
| `id` | Pack id. Shown in plans and recorded in the workspace manifest. |
| `version` | Shown by `ocws status` next to the installed version. Bump it when the pack changes. |
| `displayName`, `description` | Shown in the wizard. |
| `componentType` | Informational: `agent-pack`, `command-pack`, `skill-pack`, `tool-pack`, `template-pack`, or `config-template`. |
| `harnesses` | Harnesses that receive the top-level `files`. Default: `["opencode"]` when `files` is non-empty. |
| `files` | Files installed for every harness in `harnesses`. |
| `targets.<harness>.files` | Extra files installed only for that harness. |

A pack supports a harness if the harness is in `harnesses` or has a
`targets` entry. Selecting a pack for a harness it does not support skips it
with a note.

## File fields

| Field | Description |
| --- | --- |
| `source` | File or directory, relative to the manifest. Directories are copied recursively. |
| `destination` | Workspace-relative path. May use [harness tokens](/guide/harnesses#destination-tokens) such as `{agents}`, `{skills}`, `{mcp}`. Must stay inside the workspace. |
| `installMode` | `copy` (default), `merge`, or `toml-merge`. |
| `jsonPointers` | For `merge`: the JSON Pointers to take from the fragment. |
| `render` | `frontmatter` or `codex-agent`; requires `header`. |
| `header` | Header file used by `render`, relative to the manifest. |
| `managed` | Default `true`. `false` installs the file once and never refreshes or prunes it. |
| `role` | Free-form label recorded in the workspace manifest. |

## Rendering: one source, several harnesses

Write the body once and swap only the metadata per harness.

`render: "frontmatter"` removes the source's YAML frontmatter and writes the
YAML `header` in its place:

::: code-group

```markdown [reviewer.md (source)]
---
description: Reviews the current change. Read-only.
mode: subagent
permission:
  edit: deny
---

You review code changes. Do not edit files.
```

```yaml [harness/claude/reviewer.yaml]
name: reviewer
description: Reviews the current change. Read-only.
tools: Read, Grep, Glob, Bash
```

```markdown [.claude/agents/reviewer.md (result)]
---
name: reviewer
description: Reviews the current change. Read-only.
tools: Read, Grep, Glob, Bash
---

You review code changes. Do not edit files.
```

:::

`render: "codex-agent"` writes the TOML `header` followed by
`developer_instructions` containing the source body, which is the Codex agent
format:

```toml
name = "reviewer"
description = "Reviews the current change. Read-only."
sandbox_mode = "read-only"

developer_instructions = '''
You review code changes. Do not edit files.
'''
```

## Commands as skills

Claude Code and Codex run reusable prompts as skills. To ship a command
everywhere, install the same Markdown as an OpenCode command and as a skill:

```json
"files": [{ "source": "commit.md", "destination": "{commands}/commit.md" }],
"targets": {
  "claude": { "files": [
    { "source": "commit.md", "destination": "{skills}/commit/SKILL.md",
      "render": "frontmatter", "header": "harness/claude/commit.yaml" }
  ] },
  "codex": { "files": [
    { "source": "commit.md", "destination": "{skills}/commit/SKILL.md",
      "render": "frontmatter", "header": "harness/codex/commit.yaml" },
    { "source": "harness/codex/openai.yaml", "destination": "{skills}/commit/agents/openai.yaml" }
  ] }
}
```

- The Claude header adds `disable-model-invocation: true` so the skill only
  runs when you type `/commit`.
- For Codex, `agents/openai.yaml` with
  `policy: { allow_implicit_invocation: false }` does the same.

## Merging config: MCP servers

`installMode: "merge"` merges a JSON fragment into the destination instead of
replacing it. Only the keys at `jsonPointers` are taken from the fragment;
the rest of the destination file, including key order, is preserved.

```json
"files": [{ "source": "opencode.json", "destination": "{mcp}",
            "installMode": "merge", "jsonPointers": ["/mcp/playwright"] }],
"targets": {
  "claude": { "files": [{ "source": "harness/claude/mcp.json", "destination": "{mcp}",
                          "installMode": "merge", "jsonPointers": ["/mcpServers/playwright"] }] },
  "codex":  { "files": [{ "source": "harness/codex/mcp.toml", "destination": "{mcp}",
                          "installMode": "toml-merge" }] }
}
```

::: code-group

```json [opencode.json]
{ "mcp": { "playwright": { "type": "local", "command": ["npx", "-y", "@playwright/mcp@latest"] } } }
```

```json [harness/claude/mcp.json]
{ "mcpServers": { "playwright": { "command": "npx", "args": ["-y", "@playwright/mcp@latest"] } } }
```

```toml [harness/codex/mcp.toml]
[mcp_servers.playwright]
command = "npx"
args = ["-y", "@playwright/mcp@latest"]
```

:::

- For `merge` into `/permission` rule lists, pack rules are placed first so
  rules already in the workspace still take precedence.
- `toml-merge` appends the fragment's tables when they are absent, does
  nothing when they are identical, and blocks when they differ. Comments in
  the destination are kept.

## Harness-only files

Files that only make sense for one harness, such as OpenCode custom tools,
go in `files` with `"harnesses": ["opencode"]` and no other targets. The pack
is then skipped for other harnesses.
