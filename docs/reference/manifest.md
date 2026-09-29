# Workspace manifest

`.ocws/manifest.json` records what ocws installed in a workspace. Commit it
with the harness files so everyone refreshes from the same baseline.

```json
{
  "schemaVersion": 3,
  "generatedBy": "ocws 0.3.0",
  "generatedAt": "2026-09-29T12:00:00Z",
  "projectType": "webapp",
  "harnesses": ["opencode", "claude"],
  "components": [
    {
      "harness": "claude",
      "componentType": "agent-pack",
      "id": "reviewer",
      "sourceRoot": "packs/agents/reviewer",
      "sourceManifest": "packs/agents/reviewer/manifest.json",
      "version": "1.0.0",
      "profileId": "webapp",
      "files": [
        {
          "source": "reviewer.md",
          "destination": ".claude/agents/reviewer.md",
          "sourceSha256": "…",
          "installedSha256": "…",
          "managed": true,
          "render": "frontmatter",
          "header": "harness/claude/reviewer.yaml"
        }
      ]
    }
  ]
}
```

- Source paths are relative to the templates root, so the manifest works on
  any machine with the same templates.
- `sourceSha256` is the template output at install time (after rendering);
  `installedSha256` is what was written to the workspace.
- Comparing those with the current template and the current workspace file
  gives each file's state.

## Component refresh states

| State | Meaning |
| --- | --- |
| `current` | Installed files match the templates. |
| `refresh-available` | The template changed and your copy is untouched. `apply` updates it. |
| `locally-modified` | You edited installed files; the template has not changed. |
| `refresh-with-local-conflicts` | The template changed and you edited the file. `apply` blocks until you choose. |
| `incomplete-install` | Some installed files are missing from the workspace. |
| `source-missing` | The pack or some of its files no longer exist in the templates. |

Do not edit the manifest by hand. If it becomes unreadable, re-run with
`--rebuild-invalid-manifest`.
