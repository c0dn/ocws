---
description: Reviews code
mode: subagent
model: anthropic/claude-sonnet-4-5#high
permissions:
  - { action: read, resource: "*", effect: allow }
  - { action: shell, resource: "*", effect: ask }
  - { action: shell, resource: "git diff *", effect: allow }
---

Review the change. Paths like C:\\tmp stay intact.
