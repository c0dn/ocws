import { defineConfig } from "vitepress";

export default defineConfig({
  title: "ocws",
  description: "Set up OpenCode, Claude Code, and Codex workspaces from templates",
  base: "/ocws/",
  cleanUrls: true,
  lastUpdated: true,
  head: [["meta", { name: "theme-color", content: "#3c8772" }]],
  themeConfig: {
    nav: [
      { text: "Guide", link: "/guide/getting-started" },
      { text: "Templates", link: "/templates/" },
      { text: "Reference", link: "/reference/cli" },
      { text: "Starter templates", link: "https://github.com/c0dn/ocws-template" },
    ],
    sidebar: [
      {
        text: "Guide",
        items: [
          { text: "Getting started", link: "/guide/getting-started" },
          { text: "Concepts", link: "/guide/concepts" },
          { text: "Updating workspaces", link: "/guide/updating" },
          { text: "Harnesses", link: "/guide/harnesses" },
        ],
      },
      {
        text: "Templates",
        items: [
          { text: "Format overview", link: "/templates/" },
          { text: "profiles.json", link: "/templates/profiles" },
          { text: "Pack manifests", link: "/templates/packs" },
          { text: "Writing your own", link: "/templates/authoring" },
        ],
      },
      {
        text: "Reference",
        items: [
          { text: "CLI", link: "/reference/cli" },
          { text: "Configuration", link: "/reference/configuration" },
          { text: "Workspace manifest", link: "/reference/manifest" },
        ],
      },
    ],
    socialLinks: [{ icon: "github", link: "https://github.com/c0dn/ocws" }],
    editLink: { pattern: "https://github.com/c0dn/ocws/edit/main/docs/:path" },
    search: { provider: "local" },
    footer: { message: "Released under the MIT License." },
  },
});
