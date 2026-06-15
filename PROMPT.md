# Agentic Kanban Deep Evaluation Prompt

You are a Staff+ Software Architect, Distributed Systems Engineer, OSS Maintainer, Product Strategist, and Technical Due Diligence Reviewer.

Your task is to perform a complete evaluation of this codebase.

## Mission

Determine whether this project truly delivers on its core promise:

> "Give every agent a clear next task. No servers. No daemons. No queues. Durable task coordination through one SQLite file. Agents claim work atomically, report progress, survive crashes, and hand off reviews without anything in the middle."

Do not assume the README is correct.

Validate every claim against the actual implementation.

---

# Evaluation Areas

## 1. Architecture Review

Analyze:

* Overall architecture
* Package boundaries
* Separation of concerns
* Dependency direction
* Coupling
* Cohesion
* Extensibility

Answer:

* Is the architecture simple?
* Is it maintainable?
* Is it scalable?
* What architectural decisions are exceptional?
* What architectural decisions are risky?

Score: /10

---

## 2. Mission Alignment Review

Verify whether the implementation actually delivers:

### Atomic claiming

Can two agents ever receive the same task?

Inspect:

* Transactions
* Locking
* SQLite usage
* Isolation levels
* Retry behavior

Answer:

* Is atomic claiming guaranteed?
* Under what conditions could it fail?

Score: /10

---

### Crash Recovery

Verify:

* Lease implementation
* Lease expiry
* Reclaim behavior

Answer:

* Can abandoned work recover automatically?
* Are there edge cases where work becomes stuck?

Score: /10

---

### Durable Coordination

Verify:

* Persistence guarantees
* WAL configuration
* Migration safety
* Failure recovery

Answer:

* Can the database become corrupted?
* What happens during abrupt process termination?

Score: /10

---

### Review Workflow

Verify:

* Submission for review
* Approval flow
* Rejection flow
* Claim transfer flow

Answer:

* Does the workflow support real multi-agent collaboration?

Score: /10

---

## 3. Concurrency Review

Simulate mentally:

* 2 workers
* 10 workers
* 50 workers
* 100 workers

Analyze:

* Lock contention
* Race conditions
* Deadlocks
* Starvation
* Fairness

Questions:

* Where are bottlenecks?
* At what scale does SQLite become the limiting factor?
* What would break first?

Score: /10

---

## 4. SQLite Engineering Review

Inspect:

* Schema design
* Indexes
* Query patterns
* Transactions
* WAL mode
* Vacuum strategy
* Event storage

Identify:

* Missing indexes
* Expensive queries
* Full table scans
* Lock amplification risks

Score: /10

---

## 5. Code Quality Review

Evaluate:

* Readability
* Naming
* Error handling
* Logging
* Testability
* Modularity
* Complexity

Identify:

* God objects
* Long functions
* Duplicate logic
* Technical debt

Score: /10

---

## 6. Production Readiness Review

Determine whether this is:

* Prototype
* Alpha
* Beta
* Production-ready

Review:

* Error handling
* Data migration
* Upgrade safety
* Backup strategy
* Observability
* Reliability

List:

* Top 10 production risks

Score: /10

---

## 7. Security Review

Analyze:

* SQLite injection risks
* Hook execution risks
* Path traversal risks
* Local privilege escalation risks
* Unsafe shell execution
* Arbitrary code execution

Answer:

* Could a malicious agent abuse the system?
* Could hooks become an attack vector?

Score: /10

---

## 8. OSS Product Review

Assume you are:

* A YC partner
* A maintainer evaluating adoption
* An engineering manager deciding whether to use this

Answer:

### Is this actually a moat?

The project claims:

> "The coordination protocol is the moat."

Evaluate:

* Is that true?
* What is difficult to copy?
* What is easy to copy?
* What is defensible?

---

### Adoption Potential

Would:

* Claude Code users adopt this?
* Codex users adopt this?
* PI users adopt this?
* Cursor users adopt this?

Why or why not?

Score: /10

---

## 9. Competitive Analysis

Compare against:

* Taskmaster AI
* Claude Code task coordination
* OpenHands
* CrewAI
* LangGraph
* Temporal
* Celery
* Jira + AI agents

For each competitor:

* What does Agentic Kanban do better?
* What does it do worse?

Create a comparison table.

---

## 10. Missing Features

Identify:

### Critical Missing Features

Features required before serious adoption.

### Nice-to-Have Features

Features that would significantly improve the project.

Rank by impact.

---

## 11. Codebase Audit

Produce:

### Strengths

Top 10 strengths.

### Weaknesses

Top 10 weaknesses.

### Hidden Risks

Risks not obvious from the README.

### Technical Debt

Current debt that will become painful later.

---

## 12. Contributor Review

Assume you are reviewing a pull request from the project author.

Answer:

* What demonstrates senior engineering?
* What demonstrates staff-level thinking?
* What feels inexperienced?
* What would you challenge in a design review?

---

## 13. Investment Verdict

Give:

### Technical Score

/100

### Product Score

/100

### OSS Adoption Score

/100

### Engineering Quality Score

/100

### Moat Score

/100

### Final Score

/100

---

## Required Output Format

Provide:

1. Executive Summary
2. Mission Validation
3. Architecture Review
4. Concurrency Review
5. SQLite Review
6. Security Review
7. Product Review
8. Competitive Analysis
9. Missing Features
10. Technical Debt
11. Strengths
12. Weaknesses
13. Final Verdict

Be brutally honest.

Do not praise without evidence.

Challenge assumptions.

Use the actual code as the source of truth, not the README.

Append it a file loop-review.md if not there create the file else append, 
On the terminak just show the scores