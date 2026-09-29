---
layout: home
hero:
  name: ocws
  text: Agent workspaces from templates
  tagline: Install agents, commands, skills, and MCP servers into OpenCode, Claude Code, and Codex projects from one templates repository, and keep them up to date.
  actions:
    - theme: brand
      text: Get started
      link: /guide/getting-started
    - theme: alt
      text: Template format
      link: /templates/
    - theme: alt
      text: Starter templates
      link: https://github.com/c0dn/ocws-template
features:
  - title: One source, three harnesses
    details: Write an agent or command once. ocws renders it as an OpenCode agent, a Claude Code subagent or skill, and a Codex agent or skill.
  - title: Detects the project
    details: Profiles match on files in the workspace, and optional packs are recommended from what the project already uses.
  - title: Safe refreshes
    details: Every installed file is hashed in .ocws/manifest.json. Re-running updates what you have not touched and blocks on local edits.
  - title: Config merging
    details: MCP servers and settings merge into opencode.json, .mcp.json, and .codex/config.toml without dropping your own keys.
---
