package task

import (
	"encoding/json"
	"testing"
)

func TestListEvents(t *testing.T) {
	s := newTestService(t)

	// No events yet
	events, err := s.ListEvents(t.Context(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatalf("expected 0 events, got %d", len(events))
	}

	// Dispatch → task.created
	s.Dispatch(t.Context(), "test", "", "worker", "default", 1, nil)
	events, err = s.ListEvents(t.Context(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event after dispatch, got %d", len(events))
	}
	if events[0].EventType != "task.created" {
		t.Fatalf("expected task.created, got %s", events[0].EventType)
	}

	// Claim → task.claimed
	s.ClaimNext(t.Context(), "alice", "worker", "")
	events, err = s.ListEvents(t.Context(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events after claim, got %d", len(events))
	}
	if events[1].EventType != "task.claimed" {
		t.Fatalf("expected task.claimed, got %s", events[1].EventType)
	}

	// Complete → task.completed
	s.Complete(t.Context(), "TASK-1", "alice", false)
	events, err = s.ListEvents(t.Context(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 {
		t.Fatalf("expected 3 events after complete, got %d", len(events))
	}
	if events[2].EventType != "task.completed" {
		t.Fatalf("expected task.completed, got %s", events[2].EventType)
	}

	// Verify payloads are inline JSON, not double-encoded strings
	var payload map[string]string
	if err := json.Unmarshal(events[0].Payload, &payload); err != nil {
		t.Fatalf("unmarshal task.created payload: %v", err)
	}
	if payload["task_id"] != "TASK-1" {
		t.Fatalf("expected task_id TASK-1, got %s", payload["task_id"])
	}

	// Limit test
	events, err = s.ListEvents(t.Context(), 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events with limit=2, got %d", len(events))
	}
}


func TestListEventsReviewPath(t *testing.T) {
	s := newTestService(t)

	// Dispatch + claim + complete with --review → task.submitted_for_review, not task.completed
	s.Dispatch(t.Context(), "needs review", "", "worker", "default", 1, nil)
	s.ClaimNext(t.Context(), "alice", "worker", "")
	s.Complete(t.Context(), "TASK-1", "alice", true)

	events, err := s.ListEvents(t.Context(), 0)
	if err != nil {
		t.Fatal(err)
	}

	// Should have task.created + task.claimed + task.submitted_for_review
	if len(events) != 3 {
		t.Fatalf("expected 3 events (task.created, task.claimed, task.submitted_for_review), got %d", len(events))
	}
	if events[2].EventType != "task.submitted_for_review" {
		t.Fatalf("expected task.submitted_for_review, got %s", events[2].EventType)
	}

	// Approve → review.approved
	s.ReviewApprove(t.Context(), "TASK-1", "dave")
	events, err = s.ListEvents(t.Context(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 4 {
		t.Fatalf("expected 4 events after approve, got %d", len(events))
	}
	if events[3].EventType != "review.approved" {
		t.Fatalf("expected review.approved, got %s", events[3].EventType)
	}
}


func TestListEventsBlockAndReject(t *testing.T) {
	s := newTestService(t)

	s.Dispatch(t.Context(), "task", "", "worker", "default", 1, nil)
	s.ClaimNext(t.Context(), "alice", "worker", "")

	// Block → task.blocked
	s.Block(t.Context(), "TASK-1", "alice", "blocked")
	events, err := s.ListEvents(t.Context(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 {
		t.Fatalf("expected 3 events after block, got %d", len(events))
	}
	if events[2].EventType != "task.blocked" {
		t.Fatalf("expected task.blocked, got %s", events[2].EventType)
	}

	// Dispatch a separate task for the review+reject path
	s.Dispatch(t.Context(), "needs review", "", "worker", "default", 1, nil)
	s.ClaimNext(t.Context(), "bob", "worker", "")
	s.Complete(t.Context(), "TASK-2", "bob", true)
	s.ReviewReject(t.Context(), "TASK-2", "dave", "needs work")
	events, err = s.ListEvents(t.Context(), 0)
	if err != nil {
		t.Fatal(err)
	}
	// Events: task.created(TASK-1), task.claimed(TASK-1), task.blocked(TASK-1),
	//         task.created(TASK-2), task.claimed(TASK-2),
	//         task.submitted_for_review(TASK-2), review.rejected(TASK-2)
	if len(events) != 7 {
		t.Fatalf("expected 7 events total, got %d", len(events))
	}
	if events[6].EventType != "review.rejected" {
		t.Fatalf("expected review.rejected, got %s", events[6].EventType)
	}
}


func TestMaxEventID(t *testing.T) {
	s := newTestService(t)

	// Empty table → 0
	id, err := s.MaxEventID(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if id != 0 {
		t.Fatalf("expected 0 for empty events, got %d", id)
	}

	// After dispatch → max ID should be 1
	s.Dispatch(t.Context(), "test", "", "worker", "default", 1, nil)
	id, err = s.MaxEventID(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if id != 1 {
		t.Fatalf("expected 1 after dispatch, got %d", id)
	}

	// After claim → max ID should be 2
	s.ClaimNext(t.Context(), "alice", "worker", "")
	id, err = s.MaxEventID(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if id != 2 {
		t.Fatalf("expected 2 after claim, got %d", id)
	}
}


func TestPollEvents(t *testing.T) {
	s := newTestService(t)

	// No events → empty result
	events, err := s.PollEvents(t.Context(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatalf("expected 0 events, got %d", len(events))
	}

	// Dispatch + claim
	s.Dispatch(t.Context(), "test", "", "worker", "default", 1, nil)
	s.ClaimNext(t.Context(), "alice", "worker", "")

	// Poll from 0 → both events
	events, err = s.PollEvents(t.Context(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].EventType != "task.created" {
		t.Fatalf("expected task.created first, got %s", events[0].EventType)
	}
	if events[1].EventType != "task.claimed" {
		t.Fatalf("expected task.claimed second, got %s", events[1].EventType)
	}

	// Poll from 1 → only the second event
	events, err = s.PollEvents(t.Context(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event after ID 1, got %d", len(events))
	}
	if events[0].EventType != "task.claimed" {
		t.Fatalf("expected task.claimed, got %s", events[0].EventType)
	}

	// Poll from 2 → no new events
	events, err = s.PollEvents(t.Context(), 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatalf("expected 0 events after ID 2, got %d", len(events))
	}
}

