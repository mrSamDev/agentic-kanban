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

func runHook(hooksDir, eventType string, payload any) {
	if hooksDir == "" {
		return
	}
	name := strings.ReplaceAll(eventType, ".", "-")
	b, err := json.Marshal(map[string]any{"event": eventType, "payload": payload})
	if err != nil {
		return
	}

	// Single-file hook runs synchronously (primary action).
	execHook(filepath.Join(hooksDir, name), b, name)

	// .d/ hooks run concurrently so slow secondary hooks don't block the caller.
	// Each has its own 30s timeout via execHook.
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
		go execHook(filepath.Join(dirPath, e.Name()), b, name+".d/"+e.Name())
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
