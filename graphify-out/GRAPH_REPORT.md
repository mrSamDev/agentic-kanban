# Graph Report - .  (2026-06-06)

## Corpus Check
- Corpus is ~15,526 words - fits in a single context window. You may not need a graph.

## Summary
- 130 nodes · 180 edges · 14 communities (12 shown, 2 thin omitted)
- Extraction: 84% EXTRACTED · 16% INFERRED · 0% AMBIGUOUS · INFERRED: 28 edges (avg confidence: 0.87)
- Token cost: 0 input · 0 output

## Community Hubs (Navigation)
- [[_COMMUNITY_Task Service Tests|Task Service Tests]]
- [[_COMMUNITY_Bootstrap & Init|Bootstrap & Init]]
- [[_COMMUNITY_Agent Skills Documentation|Agent Skills Documentation]]
- [[_COMMUNITY_Plan Parsing|Plan Parsing]]
- [[_COMMUNITY_Pi Subagents Integration|Pi Subagents Integration]]
- [[_COMMUNITY_Skills Documentation|Skills Documentation]]
- [[_COMMUNITY_Pi Code Review Extension|Pi Code Review Extension]]
- [[_COMMUNITY_Task Queries & View|Task Queries & View]]
- [[_COMMUNITY_Task Service Commands|Task Service Commands]]
- [[_COMMUNITY_Task Data Models|Task Data Models]]
- [[_COMMUNITY_Install Script|Install Script]]
- [[_COMMUNITY_Task Search Types|Task Search Types]]

## God Nodes (most connected - your core abstractions)
1. `newTestService()` - 22 edges
2. `agentic-kanban` - 10 edges
3. `goreleaser` - 10 edges
4. `pi-subagents` - 10 edges
5. `claude-code-subagents` - 10 edges
6. `ParsePlan()` - 7 edges
7. `Init()` - 7 edges
8. `Service` - 7 edges
9. `Service` - 6 edges
10. `install.sh script` - 5 edges

## Surprising Connections (you probably didn't know these)
- `TestInitCreatesDBAndSkills()` --calls--> `Init()`  [INFERRED]
  internal/bootstrap/bootstrap_test.go → internal/bootstrap/bootstrap.go
- `dispatchPlan()` --calls--> `ParsePlan()`  [INFERRED]
  internal/bootstrap/bootstrap.go → internal/bootstrap/plan.go
- `TestParsePlanEmptyFile()` --calls--> `ParsePlan()`  [INFERRED]
  internal/bootstrap/bootstrap_test.go → internal/bootstrap/plan.go
- `TestParsePlanJSON()` --calls--> `ParsePlan()`  [INFERRED]
  internal/bootstrap/bootstrap_test.go → internal/bootstrap/plan.go
- `TestParsePlanMarkdownHeadings()` --calls--> `ParsePlan()`  [INFERRED]
  internal/bootstrap/bootstrap_test.go → internal/bootstrap/plan.go

## Communities (14 total, 2 thin omitted)

### Community 0 - "Task Service Tests"
Cohesion: 0.18
Nodes (21): newTestDB(), newTestService(), TestBlock(), TestClaimNext(), TestClaimNextAtomic(), TestClaimNextNoWork(), TestClaimNextOnlyForRole(), TestComplete() (+13 more)

### Community 1 - "Bootstrap & Init"
Cohesion: 0.16
Nodes (13): dispatchPlan(), harnessBase(), Init(), promptHarness(), scaffoldHarness(), TestInitWithPlanDispatchesTasks(), Harness, InitOptions (+5 more)

### Community 2 - "Agent Skills Documentation"
Cohesion: 0.30
Nodes (11): dispatch-task, review-backlog, view-task, approve-task, claim-review, reject-task, block-task, claim-next-task (+3 more)

### Community 3 - "Plan Parsing"
Cohesion: 0.27
Nodes (9): TestInitCreatesDBAndSkills(), TestParsePlanEmptyFile(), TestParsePlanJSON(), TestParsePlanMarkdownHeadings(), extractPriority(), parseJSONPlan(), parseMarkdownPlan(), ParsePlan() (+1 more)

### Community 4 - "Pi Subagents Integration"
Cohesion: 0.18
Nodes (11): pi-subagents, dispatch-task, review-backlog, view-task, approve-task, claim-review, reject-task, block-task (+3 more)

### Community 5 - "Skills Documentation"
Cohesion: 0.18
Nodes (11): claude-code-subagents, dispatch-task, review-backlog, view-task, approve-task, claim-review, reject-task, block-task (+3 more)

### Community 6 - "Pi Code Review Extension"
Cohesion: 0.20
Nodes (7): dirScopes, fileScopes, filtered, message, parts, scopes, trimmed

### Community 7 - "Task Queries & View"
Cohesion: 0.31
Nodes (3): Service, scanTask(), NullableStringFromDB()

### Community 9 - "Task Data Models"
Cohesion: 0.29
Nodes (5): HistoryEntry, Note, Task, TaskDetail, TaskStatus

### Community 10 - "Install Script"
Cohesion: 0.80
Nodes (5): add_to_gitignore(), build_from_source(), download_binary(), say(), install.sh script

## Knowledge Gaps
- **37 isolated node(s):** `PlanTask`, `InitOptions`, `SearchParams`, `TaskStats`, `TaskStatus` (+32 more)
  These have ≤1 connection - possible missing edges or undocumented components.
- **2 thin communities (<3 nodes) omitted from report** — run `graphify query` to explore isolated nodes.

## Suggested Questions
_Questions this graph is uniquely positioned to answer:_

- **Why does `NewService()` connect `Bootstrap & Init` to `Task Service Tests`?**
  _High betweenness centrality (0.269) - this node is a cross-community bridge._
- **Why does `scanTask()` connect `Task Queries & View` to `Bootstrap & Init`?**
  _High betweenness centrality (0.190) - this node is a cross-community bridge._
- **Why does `newTestService()` connect `Task Service Tests` to `Bootstrap & Init`?**
  _High betweenness centrality (0.166) - this node is a cross-community bridge._
- **Are the 10 inferred relationships involving `goreleaser` (e.g. with `dispatch-task` and `review-backlog`) actually correct?**
  _`goreleaser` has 10 INFERRED edges - model-reasoned connections that need verification._
- **What connects `PlanTask`, `InitOptions`, `SearchParams` to the rest of the system?**
  _37 weakly-connected nodes found - possible documentation gaps or missing edges._