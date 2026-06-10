# Ralph Agent: Agent-Kanban Implementation Checker

## Step 0: Verify State Against loop-plan.md

Before doing ANYTHING else, read `loop-plan.md` and verify every checked item is actually implemented.

### Verification Checklist

Run ALL checks. Report PASS/FAIL for each. Do not proceed until all pass.

```bash
# === v0.4 Safety Release ===
# Step 1: depends_on schema + claim guard
grep -q "depends_on" internal/task/model.go && echo "STEP1: depends_on field EXISTS" || echo "STEP1 FAIL: missing depends_on"
go test ./internal/task -run TestClaimGuard -v -count=1 | grep -q "PASS" && echo "STEP1: TestClaimGuard PASS" || echo "STEP1 FAIL: tests fail"

# Step 2: extend-lease command
grep -q "extend-lease\|ExtendLease\|extend_lease" cmd/kanban/*.go internal/task/*.go && echo "STEP2: extend-lease EXISTS" || echo "STEP2 FAIL: missing extend-lease"
go test ./internal/task -run TestLease -v -count=1 | grep -q "PASS" && echo "STEP2: lease tests PASS" || echo "STEP2 FAIL: tests fail"

# Step 3: Cross-agent review gate
grep -q "KANBAN_ALLOW_SELF_REVIEW\|submitted_by\|approved_by" internal/task/*.go && echo "STEP3: review gate EXISTS" || echo "STEP3 FAIL: missing review gate"
go test ./internal/task -run TestReview -v -count=1 | grep -q "PASS" && echo "STEP3: review tests PASS" || echo "STEP3 FAIL: tests fail"

# === v0.5 Speed Release ===
# Step 4: claim-next --count N + concurrent tests
grep -q "\-\-count\|count.*flag\|ClaimBatch" cmd/kanban/claim.go internal/task/service.go && echo "STEP4: --count flag EXISTS" || echo "STEP4 FAIL: missing --count"
go test ./internal/task -run "TestConcurrentClaimBatchNoDoubleClaim|TestBatchClaimRespectsDepsInConcurrent" -v -count=1 | grep -q "PASS" && echo "STEP4: concurrent batch tests PASS" || echo "STEP4 FAIL: concurrent tests fail"

# Step 5: Subagent delegation
grep -q "subagent\|Subagent\|delegate\|Delegate" cmd/kanban/*.go internal/task/*.go && echo "STEP5: delegation EXISTS" || echo "STEP5 FAIL: missing delegation"
go test ./internal/task -run TestDelegation -v -count=1 | grep -q "PASS" && echo "STEP5: delegation tests PASS" || echo "STEP5 FAIL: tests fail"

# Step 6: Project env auto-detection
grep -q "KANBAN_DB_PATH\|DB_PATH\|auto.*detect\|walk.*dir" cmd/kanban/*.go internal/storage/*.go && echo "STEP6: env auto-detection EXISTS" || echo "STEP6 FAIL: missing auto-detection"

# === v0.6 Maturity Release ===
# Step 7: Burndown stats
grep -q "Burndown\|burndown" cmd/kanban/status.go internal/task/*.go && echo "STEP7: burndown EXISTS" || echo "STEP7 FAIL: missing burndown"

# Step 8: Plan lint
grep -q "lint\|Lint\|cycle\|Cycle" cmd/kanban/plan.go internal/task/lint.go && echo "STEP8: lint EXISTS" || echo "STEP8 FAIL: missing lint"

# Step 9: approve-plan --all
grep -q "\-\-all\|ApproveAll\|approve.*all" cmd/kanban/*.go embed/skills/manager/*.md && echo "STEP9: batch approve EXISTS" || echo "STEP9 FAIL: missing batch approve"

# === Core Tests ===
go test ./... -count=1 | grep -q "ok\|FAIL" && echo "ALL TESTS: pass summary received" || echo "ALL TESTS: no output"
```

### If All Checks PASS

Read `loop-plan.md` → extract the next UNCHECKED item → implement it → run tests → update `loop-plan.md` → commit.

### If Any Check FAILS

Report which items are missing. Do not proceed until you either:
1. Implement the missing item, OR
2. Update `loop-plan.md` to mark the item as not-in-scope

## Ralph Principles

1. **Verify before act** — check state first, always
2. **One task per loop** — implement one feature at a time
3. **Backpressure** — run tests after each change
4. **Don't assume** — search codebase before implementing
5. **Update loop-plan.md** — mark completed items
6. **Commit often** — after each successful feature

## Process

1. Run verification checklist above
2. Read loop-plan.md + relevant source files
3. Implement the feature
4. Run tests (`go test ./...`)
5. Update loop-plan.md progress
6. Commit + push

Start by running the verification checklist.