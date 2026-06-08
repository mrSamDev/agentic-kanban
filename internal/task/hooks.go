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
	"time"
)

// hookSem caps concurrent .d/ hook goroutines to prevent unbounded goroutine
// spawns on burst events. Blocks the caller when at capacity — prefer
// backpressure over silent drops.
var hookSem = make(chan struct{}, 20)

func runHook(hooksDir, eventType string, payload any) {
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
		execHook(filepath.Join(dirPath, e.Name()), b, name+".d/"+e.Name())
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
