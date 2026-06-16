package task

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// HookRunner manages lifecycle of concurrent hook goroutines.
// Use Wait() before process exit to prevent goroutine killing.
type HookRunner struct {
	wg sync.WaitGroup
}

func NewHookRunner() *HookRunner {
	return &HookRunner{}
}

// Wait blocks until all .d/ hooks finish, with a timeout.
// Must exceed execHook's 30s context timeout (35s recommended).
func (r *HookRunner) Wait(timeout time.Duration) {
	done := make(chan struct{})
	go func() {
		r.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(timeout):
		fmt.Fprintf(os.Stderr, "hook runner timeout after %v\n", timeout)
	}
}

func runHook(runner *HookRunner, hooksDir, eventType string, payload any) {
	if hooksDir == "" {
		return
	}
	name := strings.ReplaceAll(eventType, ".", "-")
	b, err := json.Marshal(map[string]any{"event": eventType, "payload": payload})
	if err != nil {
		return
	}

	execHook(filepath.Join(hooksDir, name), b, name)

	dirPath := filepath.Join(hooksDir, name+".d")
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil || info.Mode()&0111 == 0 {
			continue
		}
		runner.wg.Add(1)
		go func(path, label string, payload []byte) {
			defer runner.wg.Done()
			execHook(path, payload, label)
		}(filepath.Join(dirPath, e.Name()), name+".d/"+e.Name(), b)
	}
}

func execHook(hookPath string, payload []byte, label string) {
	if _, err := os.Stat(hookPath); err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, hookPath)
	cmd.Stdin = bytes.NewReader(payload)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "hook %s: %v\n", label, err)
	}
}