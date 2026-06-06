package main

import (
	"encoding/json"
	"fmt"
	"os"
)

func writeJSON(v any) {
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		writeStderr(fmt.Sprintf("marshal error: %v", err))
		os.Exit(1)
	}
	fmt.Println(string(out))
}

func writeStderr(msg string) {
	// os.Stderr.Write failure is deliberately ignored — nothing useful to do.
	if err := json.NewEncoder(os.Stderr).Encode(map[string]string{"error": msg}); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, msg)
	}
}
