# Template format

A templates repository is a directory, usually a git repository, laid out like
this:

```text
profiles.json              profile registry (required)
guides/                    AGENTS.md templates
workspace-configs/         per-harness workspace config templates
scaffolds/                 files created once by profile scaffolds
packs/<kind>/<name>/
  manifest.json            pack manifest
  ...                      the pack's source files
  harness/<harness>/       per-harness headers and config fragments
```

Only `profiles.json` has a fixed name and location. Every other path is
referenced from it, relative to the repository root, so organise the rest as
you like.

- [profiles.json](/templates/profiles): profiles, detection, base and capability packs
- [Pack manifests](/templates/packs): files, destinations, per-harness targets, rendering, merging
- [Writing your own](/templates/authoring): workflow, validation, and CI

## JSON Schemas

Reference the schemas for completion and inline errors in editors that
support JSON Schema:

```json
{ "$schema": "https://c0dn.github.io/ocws/schema/profiles.schema.json" }
```

```json
{ "$schema": "https://c0dn.github.io/ocws/schema/pack.schema.json" }
```

`ocws templates validate` is the authoritative check. It also verifies that
every referenced file exists and that every pack renders for each harness it
claims to support.

## Versioning

Both files use `"schemaVersion": 3`. ocws rejects other versions.
