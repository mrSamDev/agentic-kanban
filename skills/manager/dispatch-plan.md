---
name: dispatch-plan
description: Read a plan file (spec, roadmap, product brief), extract tasks using your own intelligence, write a review file for user approval.
---

# Dispatch Plan (LLM-Driven)

You are the product owner. Read the plan file, extract meaningful tasks
using your own understanding of the document, and write a review file
so the user can approve before tasks are dispatched.

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
4. Write a proposal file at .kanban/tasks-proposal.md in this format:

       # Task Proposal

       1. [ ] Implement user login (p10, role: worker)
           - Sign up with email, log in/out
       2. [ ] Build task list view (p20, role: worker)
           - Filter by all/completed/pending

5. Present the proposal to the user: I've created a task proposal
   at .kanban/tasks-proposal.md. Review and run approve-plan to dispatch.

## Guidelines

- Break large features into discrete tasks (each should be 1-2 days work)
- Use your judgment — don't parse mechanically, understand the plan
- Prioritize core features first (p1-p20), enhancements later (p50+)
- Add context under each task with a bullet-point description
- If the plan has a timeline/sprint section, group related tasks

## Output

File: .kanban/tasks-proposal.md
The user reviews this file then runs approve-plan to dispatch.