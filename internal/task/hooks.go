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
	hookPath := filepath.Join(hooksDir, name)
	b, err := json.Marshal(map[string]any{
		"event":   eventType,
		"payload": payload,
	})
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, hookPath)
	cmd.Stdin = bytes.NewReader(b)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "hook %s: %v\n", name, err)
	}
}
