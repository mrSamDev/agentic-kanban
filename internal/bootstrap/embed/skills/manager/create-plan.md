---
name: create-plan
description: 3-pass extraction from a plan file. Extract surface tasks, implied work, then integration sequencing. Writes proposal for user approval. Does NOT dispatch.
role: manager
type: protocol
---

# Create Plan — 3-Pass Extraction

Read the plan file three times, each pass extracting a different layer
of work. Write the proposal only after all three passes. Then STOP.

**STOP after writing the proposal. Do NOT dispatch any tasks.
Do NOT call dispatch_task. Do NOT call kanban task dispatch.**

---

## Pass 1: Surface Scan

Read the plan and extract **every explicitly described deliverable**.
Be exhaustive — if the plan names a file, a component, an endpoint, a
SCSS variable section, a config value, a docker-compose, a page, a
diagram element — it gets a task.

**Do not collapse related items into one task** unless they are truly
the same unit of work (e.g. a single file with multiple variables is
one task; a directory tree with 5 files is 5 tasks).

If the plan has a "Future", "V2", "Out of Scope", or "Roadmap" section,
**do not create tasks for those**. Flag them as deferred in the proposal.

**Output:** A raw list of surface tasks. Each gets:
- Title, priority (1-100), role, project label
- Source reference (section heading or quote)

---

## Pass 2: Implied Work

Read the plan again. For **every** surface task from Pass 1, extract
the work the plan does **not** explicitly name but the implementation
requires. Use your judgment about what each task needs.

For UI tasks, always consider:
- Loading state, empty state, error state
- Hover/focus/active/disabled states
- Transitions and animations
- Keyboard nav, screen readers, focus management
- Responsive layout

For API/backend tasks, always consider:
- Error responses (400, 401, 403, 404, 500)
- Input validation
- Auth/ownership checks
- Rate limiting, logging

For data tasks, always consider:
- Migrations
- Validation schemas
- Shared types/constants

If you are unsure what a task implies, call `advisor` for guidance.

**Output:** Additional tasks for every implied concern. Reference the
surface task that implies them. Add to the proposal as first-class tasks
alongside the surface tasks — not in a separate section.

---

## Pass 3: Integration + Sequencing

Read the plan a third time. Extract the structure:

- **Dependency graph** — which tasks block others? (tokens before components, schema before API, auth before protected routes)
- **Phases / cycles** — does the plan naturally group work? If the plan has a design system, there's a natural cycle: scaffold → tokens → components → pages → auth → features → polish.
- **Cross-cutting concerns** — shared types, shared config, shared infra, design tokens
- **Risk** — which tasks have the most uncertainty or blast radius?
- **Gaps** — what does the plan miss? Testing strategy? Error handling? Deployment? CI/CD? Secrets management? .env.example?

If you are unsure about sequencing or gaps, call `advisor`.

**Output:** Dependency annotations, phase labels, a gaps section.

---

## Writing the Proposal

Combine all three passes into `.kanban/tasks-proposal.md`:

```markdown
# Task Proposal: <project>
Source: <plan-file>
Generated: <timestamp>

## Proposed Tasks

| ID | Title | Prio | Role | Depends | Phase |
|---|---|---|---|---|---|
| TASK-1 | ... | 10 | worker | — | P0 |
| TASK-2 | ... | 20 | worker | TASK-1 | P0 |

### Details (per task with source refs, implied work, risk, acceptance)

#### TASK-1: <title>
**Source:** plan.md §"Section"
**Phase:** P0
**Risk:** low/medium/high
**Implies:**
  - Implied work item 1
  - Implied work item 2
**Acceptance:** What "done" looks like

## Deferred (future / out-of-scope)

Items from Future sections — do NOT dispatch.

## ⚠ Gaps

Things the plan omits but the project needs.

## Sequencing

| Phase | Tasks | Rationale |
|---|---|---|
| P0 — Foundation | TASK-1, TASK-2 | Why these go first |

## Approval

- [ ] Reviewed by user
- [ ] Approved for dispatch

Run `approve-plan` to dispatch these tasks to the kanban board.
```

---

## Hard Rules

- **DO NOT** call `dispatch_task` (the pi tool)
- **DO NOT** call `kanban task dispatch` (the CLI)
- **DO NOT** create kanban entries of any kind
- **DO NOT** spawn subagents or start working on tasks
- **DO NOT** claim tasks or modify task state
- Writing the proposal file is the ONLY allowed output
- You MUST complete all 3 passes before writing the proposal
- You MUST include both explicit and implied tasks in one flat list
- You MUST flag deferred sections — do not create tasks for them
- If uncertain at any point, call `advisor`

## Output

File: `.kanban/tasks-proposal.md`
The user reviews this file then runs `approve-plan` to dispatch.
No tasks are created on the kanban board by this skill.