# Getting started

## Install

::: code-group

```bash [npm]
npm i -g @c0dn/ocws
```

```bash [bun]
bun add -g @c0dn/ocws
```

```bash [go]
go install github.com/c0dn/ocws/cmd/ocws@latest
```

:::

The npm package is a small launcher. On first run it downloads the binary for
your platform from the matching [GitHub release](https://github.com/c0dn/ocws/releases),
checks it against the release `checksums.txt`, and caches it. See
[Configuration](/reference/configuration#npm-launcher) for overrides.

## Fetch templates

```bash
ocws init
```

Without arguments this clones the public
[starter templates](https://github.com/c0dn/ocws-template) into
`~/.config/ocws/templates`. To use your own repository instead:

```bash
ocws init --from git@github.com:you/agent-templates.git
```

Private repositories work with whatever git credentials you already use.

## Set up a project

```bash
cd your-project
ocws
```

The wizard shows the detected profile, the harnesses to set up, and the packs
to install, then previews every change before writing anything.

The same thing without prompts:

```bash
ocws detect                                          # what ocws would pick, and why
ocws plan  -p webapp --harness opencode,claude       # read-only preview
ocws apply -p webapp --harness opencode,claude --cap context7
```

After `apply` the workspace contains the harness files (for example
`.opencode/agents/`, `.claude/skills/`, `.mcp.json`), an `AGENTS.md`, and
`.ocws/manifest.json`, which records what was installed. Commit them if your
team should share the setup.

## Keep it current

```bash
ocws templates update   # pull template changes
ocws status             # what can be refreshed
ocws apply              # refresh; files you edited are never overwritten silently
```

See [Updating workspaces](/guide/updating).
