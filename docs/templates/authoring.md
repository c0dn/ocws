# Writing your own templates

## Start from the starter

1. Open [c0dn/ocws-template](https://github.com/c0dn/ocws-template) and
   click **Use this template**. A private repository works.
2. Point ocws at your copy:

   ```bash
   ocws init --force --from git@github.com:you/agent-templates.git
   ```

   `--force` moves the existing templates to `templates.bak`.

3. Work in a checkout and point ocws at it while you iterate:

   ```bash
   git clone git@github.com:you/agent-templates.git ~/src/agent-templates
   export OCWS_TEMPLATES=~/src/agent-templates
   ```

## Add a pack

1. Create `packs/<kind>/<name>/manifest.json` and the source files. See
   [Pack manifests](/templates/packs).
2. Reference it from a profile's `basePacks` or a capability group in
   `profiles.json`.
3. Validate and try it in a scratch directory:

   ```bash
   ocws templates validate --templates .
   mkdir -p /tmp/try && ocws plan -C /tmp/try -p webapp --harness opencode,claude,codex
   ```

4. Bump the pack `version` when you change it, so `ocws status` shows an
   update in existing workspaces.

## Generate Claude and Codex targets

If you already have OpenCode-style agent, command, skill, or MCP packs,
`templates port` writes the `targets` and header stubs for you:

```bash
ocws templates port packs/**/manifest.json --harness claude,codex
```

- Agents become Claude subagents and Codex agent TOML.
- Commands become skills that only run when invoked explicitly.
- Skills are copied as they are.
- `opencode.json` MCP fragments become `.mcp.json` and `[mcp_servers.*]`.

OpenCode permissions are not translated. Review the generated headers under
`harness/` and set `tools` or `permissionMode` (Claude) and `sandbox_mode`
(Codex) yourself.

## Check in CI

```yaml
# .github/workflows/validate.yml
name: validate
on: [push, pull_request]
jobs:
  validate:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-node@v4
        with:
          node-version: 22
      - run: npx -y @c0dn/ocws templates validate --templates .
```

## Publish to your machines

Push to the repository, then on each machine:

```bash
ocws templates update
```
