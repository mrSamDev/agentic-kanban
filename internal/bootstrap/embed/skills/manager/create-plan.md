---
name: create-plan
description: Read a plan file (spec, roadmap, product brief), extract tasks, write a proposal file for user approval. Does NOT dispatch — user must run approve-plan.
role: manager
type: protocol
---

# Create Plan

Read the plan file, extract meaningful tasks using your own
understanding of the document, and write a proposal at
`.kanban/tasks-proposal.md` for user review.

**STOP after writing the proposal. Do NOT dispatch any tasks.
Do NOT call dispatch_task. Do NOT call kanban task dispatch.**

## Workflow

1. Read the plan file (plan.md, spec.md, PRD, etc.)
2. Analyze it — identify every actionable work item the plan describes:
   - Features, components, pages, API endpoints, DB changes
   - Bug fixes, tests, deployment steps
   - Research spikes, documentation tasks
3. For each task determine:
   - Title (clear, actionable)
   - Priority (1-100, lower = more urgent)
   - Role (worker, reviewer, etc.) — defaults to worker
   - Project scope label
   - Dependencies on other tasks
4. Write `.kanban/tasks-proposal.md`:

       # Task Proposal: <project>
       Source: <plan-file>
       Generated: <timestamp>

       ## Proposed Tasks

       | ID | Title | Priority | Role | Depends On |
       |---|---|---|---|---|
       | TASK-1 | Init monorepo scaffold | 10 | worker | — |
       | TASK-2 | Design system tokens | 20 | worker | TASK-1 |

       ## Notes

       - Add any grouping, sprint assignments, or sequencing notes here

       ## Approval

       - [ ] Reviewed by user
       - [ ] Approved for dispatch

       Run `approve-plan` to dispatch these tasks to the kanban board.

5. Present the proposal to the user:
   "I've created a task proposal at .kanban/tasks-proposal.md.
   Review it, then run approve-plan to dispatch."

## Hard Rules

- **DO NOT** call `dispatch_task` (the pi tool)
- **DO NOT** call `kanban task dispatch` (the CLI)
- **DO NOT** create kanban entries of any kind
- **DO NOT** spawn subagents or start working on tasks
- **DO NOT** claim tasks or modify task state
- Writing the proposal file is the ONLY allowed output

## Guidelines

- Break large features into discrete tasks (each should be 1-2 days work)
- Use your judgment — don't parse mechanically, understand the plan
- Prioritize core features first (p1-p20), enhancements later (p50+)
- Add context under each task with a description bullet
- If the plan has a timeline/sprint section, group related tasks
- Detect dependencies between tasks and list them

## Output

File: `.kanban/tasks-proposal.md`
The user reviews this file then runs `approve-plan` to dispatch.
No tasks are created on the kanban board by this skill.