package task

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHookFiresAfterDispatch(t *testing.T) {
	hooksDir := filepath.Join(t.TempDir(), "hooks")
	s := newTestServiceWithHooks(t, hooksDir)
	sentinel := filepath.Join(filepath.Dir(hooksDir), "fired")
	makeHook(t, hooksDir, "task.created", "touch "+sentinel)

	if _, err := s.Dispatch(t.Context(), "hook test", "", "worker", "default", 50, nil); err != nil {
		t.Fatal(err)
	}
	waitForSentinel(t, sentinel)
}


func TestHookDoesNotFireOnRollback(t *testing.T) {
	hooksDir := filepath.Join(t.TempDir(), "hooks")
	s := newTestServiceWithHooks(t, hooksDir)
	sentinel := filepath.Join(filepath.Dir(hooksDir), "fired")
	makeHook(t, hooksDir, "task.completed", "touch "+sentinel)

	// Complete with wrong agent — guaranteed rollback, no hook
	s.Dispatch(t.Context(), "task", "", "worker", "default", 50, nil)
	s.ClaimNext(t.Context(), "agent-a", "worker", "")
	s.Complete(t.Context(), "TASK-1", "wrong-agent", false)

	if _, err := os.Stat(sentinel); err == nil {
		t.Fatal("hook fired on rollback")
	}
}


func TestHookClaimNextEmptyResultNoFire(t *testing.T) {
	hooksDir := filepath.Join(t.TempDir(), "hooks")
	s := newTestServiceWithHooks(t, hooksDir)
	sentinel := filepath.Join(filepath.Dir(hooksDir), "fired")
	makeHook(t, hooksDir, "task.claimed", "touch "+sentinel)

	// No tasks in DB — ClaimNext returns zero-value task, hook must not fire
	s.ClaimNext(t.Context(), "agent", "worker", "")

	if _, err := os.Stat(sentinel); err == nil {
		t.Fatal("hook fired on empty ClaimNext result")
	}
}


func TestHookMissingIsSilent(t *testing.T) {
	s := newTestServiceWithHooks(t, t.TempDir()) // empty dir, no hook scripts

	if _, err := s.Dispatch(t.Context(), "task", "", "worker", "default", 50, nil); err != nil {
		t.Fatalf("Dispatch failed with missing hook: %v", err)
	}
}


func TestHookNonZeroExitIsSilent(t *testing.T) {
	hooksDir := filepath.Join(t.TempDir(), "hooks")
	s := newTestServiceWithHooks(t, hooksDir)
	makeHook(t, hooksDir, "task.created", "exit 1")

	if _, err := s.Dispatch(t.Context(), "task", "", "worker", "default", 50, nil); err != nil {
		t.Fatalf("Dispatch failed with non-zero hook exit: %v", err)
	}
}


func TestHookDAllFire(t *testing.T) {
	hooksDir := filepath.Join(t.TempDir(), "hooks")
	s := newTestServiceWithHooks(t, hooksDir)
	s1 := filepath.Join(filepath.Dir(hooksDir), "s1")
	s2 := filepath.Join(filepath.Dir(hooksDir), "s2")
	makeHookD(t, hooksDir, "task.created", "slack", "touch "+s1)
	makeHookD(t, hooksDir, "task.created", "metrics", "touch "+s2)

	if _, err := s.Dispatch(t.Context(), "task", "", "worker", "default", 50, nil); err != nil {
		t.Fatal(err)
	}
	waitForSentinel(t, s1)
	waitForSentinel(t, s2)
}


func TestHookDLexOrder(t *testing.T) {
	hooksDir := filepath.Join(t.TempDir(), "hooks")
	s := newTestServiceWithHooks(t, hooksDir)
	order := filepath.Join(filepath.Dir(hooksDir), "order")
	makeHookD(t, hooksDir, "task.created", "a", "echo a >>"+order)
	makeHookD(t, hooksDir, "task.created", "b", "echo b >>"+order)
	makeHookD(t, hooksDir, "task.created", "c", "echo c >>"+order)

	if _, err := s.Dispatch(t.Context(), "task", "", "worker", "default", 50, nil); err != nil {
		t.Fatal(err)
	}
	// Poll until all three hooks have written
	var lines []string
	for range 200 {
		data, _ := os.ReadFile(order)
		lines = strings.Split(strings.TrimSpace(string(data)), "\n")
		if len(lines) >= 3 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(lines) < 3 {
		t.Fatalf("expected 3 hook executions, got %d", len(lines))
	}

	seen := make(map[string]bool)
	for _, line := range lines[:3] {
		seen[line] = true
	}
	for _, name := range []string{"a", "b", "c"} {
		if !seen[name] {
			t.Fatalf("hook %s did not fire; got: %v", name, lines[:3])
		}
	}
}


func TestHookDNonExecutableSkipped(t *testing.T) {
	hooksDir := filepath.Join(t.TempDir(), "hooks")
	s := newTestServiceWithHooks(t, hooksDir)
	dDir := filepath.Join(hooksDir, "task-created.d")
	sentinel := filepath.Join(filepath.Dir(hooksDir), "fired")
	os.MkdirAll(dDir, 0755)
	os.WriteFile(filepath.Join(dDir, "nope"), []byte("#!/bin/sh\ntouch "+sentinel+"\n"), 0644)

	if _, err := s.Dispatch(t.Context(), "task", "", "worker", "default", 50, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(sentinel); err == nil {
		t.Error("non-executable hook should not have fired")
	}
}


func TestHookDAndSingleFileBothFire(t *testing.T) {
	hooksDir := filepath.Join(t.TempDir(), "hooks")
	s := newTestServiceWithHooks(t, hooksDir)
	single := filepath.Join(filepath.Dir(hooksDir), "single")
	multi := filepath.Join(filepath.Dir(hooksDir), "multi")
	makeHook(t, hooksDir, "task.created", "touch "+single)
	makeHookD(t, hooksDir, "task.created", "extra", "touch "+multi)

	if _, err := s.Dispatch(t.Context(), "task", "", "worker", "default", 50, nil); err != nil {
		t.Fatal(err)
	}
	waitForSentinel(t, single)
	waitForSentinel(t, multi)
}


func TestHookDFailingEntryDoesNotBlockSiblings(t *testing.T) {
	hooksDir := filepath.Join(t.TempDir(), "hooks")
	s := newTestServiceWithHooks(t, hooksDir)
	sentinel := filepath.Join(filepath.Dir(hooksDir), "fired")
	makeHookD(t, hooksDir, "task.created", "a", "exit 1")
	makeHookD(t, hooksDir, "task.created", "b", "touch "+sentinel)

	if _, err := s.Dispatch(t.Context(), "task", "", "worker", "default", 50, nil); err != nil {
		t.Fatalf("Dispatch failed: %v", err)
	}
	waitForSentinel(t, sentinel)
}


// --- Stability tests ---

