# ocws

`ocws` sets up **OpenCode**, **Claude Code**, and **Codex CLI** workspaces from
a templates repository. It installs agents, commands, skills, MCP servers, and
config for the harnesses you use, writes `AGENTS.md`, and records what it
installed so later runs refresh your setup without overwriting your edits.

**Documentation: <https://c0dn.github.io/ocws/>**

## Install

```bash
npm i -g @c0dn/ocws        # or: bun add -g @c0dn/ocws, npx @c0dn/ocws
go install github.com/c0dn/ocws/cmd/ocws@latest
```

The npm package is a small launcher. On first run it downloads the binary for
your platform from the matching GitHub release and verifies its checksum.

## Quick start

```bash
ocws init          # clone the starter templates into ~/.config/ocws/templates
cd your-project
ocws               # interactive wizard
```

Or without prompts:

```bash
ocws detect                                               # detected profile and recommended packs
ocws plan  -p webapp --harness opencode,claude            # read-only preview
ocws apply -p webapp --harness opencode,claude --cap context7
ocws status                                               # installed packs and refresh state
```

## Templates

A templates repository holds **profiles** (workspace types with detection
rules, an `AGENTS.md` guide, and config) and **packs** (agents, commands,
skills, MCP config). A single pack source renders for every harness.

- Start from [c0dn/ocws-template](https://github.com/c0dn/ocws-template)
  (**Use this template**), then `ocws init --force --from <your repo>`.
- Format reference: <https://c0dn.github.io/ocws/templates/>
- JSON Schemas for editors: `https://c0dn.github.io/ocws/schema/profiles.schema.json`
  and `.../pack.schema.json`

## Development

```bash
go test ./...
go run ./cmd/ocws --templates testdata/templates plan -C /tmp/x -p dev
cd docs && bun install && bun run dev      # docs site
node npm/build.mjs 0.0.0-dev --pack        # local npm package in dist/npm
```

Work lands on `dev`. Pushing to `main` runs CI, and when code or the npm
launcher changed it also releases: the version is derived from conventional
commits since the last tag, GoReleaser publishes the binaries, and the
workflow publishes `@c0dn/ocws` to npm with trusted publishing. Docs changes on
`main` redeploy the site.

## License

MIT
