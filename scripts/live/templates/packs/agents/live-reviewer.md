---
description: Live-test reviewer agent. Reviews diffs for correctness.
mode: subagent
model: anthropic/claude-sonnet-4-5#high
permissions:
  - { action: read, resource: "*", effect: allow }
  - { action: shell, resource: "git diff *", effect: allow }
---

You review the current diff for correctness and missing tests.
