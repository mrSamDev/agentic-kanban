---
name: log-progress
description: Log a progress note and renew your lease (heartbeat) to prevent lease expiry.
---

# Log Progress

Log a progress note and renew your lease (heartbeat). Call this periodically
while working to prevent lease expiry.

Usage:

  kanban task log-progress TASK-101 \
    --agent my-agent \
    --note "Implemented the auth handler" \
    --type PROGRESS

Flags:
  --agent  (required) Your agent identifier
  --note   (required) Progress description
  --type   (optional) PROGRESS, ERROR, or DECISION

Lease renewal: this command extends lease to +15 min from now.

Exit: 0 = success, 2 = not assigned or not found.
