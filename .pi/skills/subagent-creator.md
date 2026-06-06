---
name: subagent-creator
description: Creates specialized subagents on-demand and executes them. Use when a task requires multiple distinct skill sets, when you need to delegate focused work to isolated contexts, when parallel execution would speed up completion, or when you want to chain specialized agents in sequence. Enables dynamic agent creation without pre-defined skills.
model: ollama/gemma4:31b-cloud
metadata:
  tags: subagents, delegation, parallel-execution, agent-orchestration, meta-agent
---

USE MODEl >  ollama/gemma4:31b-cloud


## When to use

Use this skill when:

- A task spans multiple domains (e.g., "build API + write docs + create tests")
- You need isolated contexts for different parts of a problem
- Parallel execution would reduce total time
- You want to chain specialized reasoning in sequence
- The existing skill set doesn't cover a needed specialization
- You're orchestrating complex multi-step workflows

## Core concept

Instead of one agent doing everything, spawn focused subagents with narrow scopes. Each subagent gets:
1. A specific role and expertise
2. Clear success criteria
3. Isolated context (can't see sibling outputs unless you share them)
4. A single responsibility

You orchestrate. They execute.

## Agent creation patterns

### Single specialized agent

When you need one focused expert:

```
subagent: {
  agent: "typescript-expert",
  task: "Review types/index.ts for any `any` types and suggest strict alternatives"
}
```

Create the agent name based on the task, not a pre-existing list. The agent doesn't need to exist beforehand—you're defining its expertise through the task description.

### Parallel agents

When tasks are independent:

```
subagent: {
  parallel: [
    { agent: "api-reviewer", task: "Review all route handlers in src/routes/ for error handling" },
    { agent: "test-auditor", task: "Check test coverage for src/services/ and identify gaps" },
    { agent: "doc-writer", task: "Generate API documentation from src/routes/ JSDoc comments" }
  ]
}
```

All three run simultaneously. You get three results back.

### Chained agents

When output from one feeds into another:

```
subagent: {
  chain: [
    { agent: "code-analyzer", task: "Analyze src/ and list all public API endpoints with their request/response types" },
    { agent: "security-auditor", task: "Review the API endpoints from {previous} for authentication gaps and input validation issues" },
    { agent: "fix-planner", task: "Create a prioritized list of security fixes based on {previous} with implementation steps" }
  ]
}
```

Each agent sees the previous agent's output via `{previous}` placeholder.

### Mixed orchestration

Combine parallel and chain:

```
subagent: {
  chain: [
    {
      agent: "parallel-analyzers",
      task: "Run parallel analysis",
      parallel: [
        { agent: "perf-profiler", task: "Profile database queries in src/ and identify N+1 patterns" },
        { agent: "memory-profiler", task: "Find memory leaks and large allocations in src/" }
      ]
    },
    {
      agent: "optimization-planner",
      task: "Combine findings from {previous} and create prioritized optimization plan"
    }
  ]
}
```

## Task description quality

Bad task (vague):
```
{ agent: "reviewer", task: "Review the code" }
```

Good task (specific):
```
{ agent: "security-reviewer", task: "Audit src/auth/ for: 1) hardcoded secrets, 2) missing rate limits, 3) weak password policies, 4) session fixation vulnerabilities. List each finding with file:line and severity." }
```

The task description defines the agent's expertise. Be specific about:
- Scope (which files/directories)
- Success criteria (what output format)
- Constraints (what to ignore)
- Priority (what matters most)

## Context passing

### To subagents

You can pass context explicitly:

```
subagent: {
  agent: "test-generator",
  task: "Write tests for UserService based on this interface:\n\n{interface}"
}
```

Where `{interface}` is replaced with actual content you've read.

### From subagents

Subagent output becomes input for the next agent in a chain via `{previous}`. You control what gets passed.

## Agent scope control

Limit what subagents can do:

```
{
  agent: "readonly-analyzer",
  task: "Analyze src/ architecture. Read-only: do not modify any files. Output: markdown report."
}
```

```
{
  agent: "test-fixer",
  task: "Fix failing tests in tests/. You may only modify test files, not source code."
}
```

## Execution flow

1. **Plan**: Identify distinct subtasks that benefit from isolation
2. **Design**: Create agent names and task descriptions for each subtask
3. **Execute**: Call subagent tool with single/parallel/chain configuration
4. **Synthesize**: Combine outputs into final result
5. **Iterate**: If needed, spawn follow-up agents based on results

## Common patterns

### Code review delegation

```
subagent: {
  parallel: [
    { agent: "security-auditor", task: "Find security issues in src/" },
    { agent: "perf-reviewer", task: "Find performance issues in src/" },
    { agent: "style-checker", task: "Check code style violations in src/" }
  ]
}
```

### Feature implementation

```
subagent: {
  chain: [
    { agent: "requirements-analyzer", task: "Extract implementation requirements from {user_request}" },
    { agent: "architect", task: "Design implementation approach based on {previous}" },
    { agent: "implementer", task: "Implement the design from {previous}" },
    { agent: "tester", task: "Write tests for the implementation from {previous}" }
  ]
}
```

### Debugging session

```
subagent: {
  chain: [
    { agent: "log-analyzer", task: "Analyze error logs and identify root cause candidates" },
    { agent: "code-tracer", task: "Trace execution flow for {previous} candidates in relevant files" },
    { agent: "fix-proposer", task: "Propose fixes for {previous} with confidence levels" }
  ]
}
```

### Documentation generation

```
subagent: {
  parallel: [
    { agent: "api-doc-writer", task: "Generate API reference from src/routes/" },
    { agent: "tutorial-writer", task: "Create getting started guide from src/examples/" },
    { agent: "changelog-writer", task: "Summarize changes from git log since last release" }
  ]
}
```

## Anti-patterns

### Don't over-split

Bad:
```
parallel: [
  { agent: "line-reader-1", task: "Read lines 1-50 of file.ts" },
  { agent: "line-reader-2", task: "Read lines 51-100 of file.ts" }
]
```

This adds overhead without benefit. One agent can read the whole file.

### Don't create redundant agents

Bad:
```
{ agent: "typescript-expert", task: "Check types" }
{ agent: "type-checker", task: "Check types" }
```

Same task = same agent. No need to split.

### Don't hide context

Bad:
```
chain: [
  { agent: "analyzer", task: "Analyze src/" },
  { agent: "fixer", task: "Fix issues in src/" }  // doesn't know what issues
]
```

Good:
```
chain: [
  { agent: "analyzer", task: "List all issues in src/ with file:line" },
  { agent: "fixer", task: "Fix the issues identified in {previous}" }
]
```

## Output synthesis

After subagents complete, you synthesize:

```
// Example response structure
## Summary

Three subagents completed:
- Security audit: 2 critical, 3 medium issues
- Performance review: 1 N+1 query, 2 missing indexes
- Style check: 15 violations (mostly missing semicolons)

## Security Findings

[Consolidated from security-auditor]

## Performance Findings

[Consolidated from perf-reviewer]

## Next Steps

1. Fix critical security issues (priority)
2. Add database indexes
3. Run style formatter
```

## Error handling

If a subagent fails:
1. Check if task was too vague → make it more specific
2. Check if scope was too large → narrow it
3. Check if context was missing → provide it
4. Retry with adjusted parameters

## Integration with existing skills

Subagents can use other skills:

```
{
  agent: "doc-expert",
  task: "Use the documentation skill to create a tutorial for the authentication flow"
}
```

The subagent inherits access to all available tools and skills.

## Cost awareness

Each subagent call has overhead. Use when:
- Task complexity justifies isolation
- Parallel execution saves time
- Specialization improves quality

Don't use for trivial splits that add cost without benefit.