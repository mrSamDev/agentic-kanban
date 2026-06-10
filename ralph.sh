#!/usr/bin/env bash
# Ralph - the infinite while loop technique for AI coding agents
# Core idea: run an AI coding agent in a loop, one task per iteration,
# with backpressure via tests and version control.

set -e

PROMPT_FILE="${PROMPT_FILE:-PROMPT.md}"
MODEL="${MODEL:-deepseek-v4-flash:cloud}"
MAX_ITERATIONS="${MAX_ITERATIONS:-30}"

AGENT=pi

echo "Ralph started at $(date)"
echo "Using prompt file: $PROMPT_FILE"
echo "Using model: $MODEL"

# The infinite loop - Ralph runs until you stop it or the TODO list is empty
MAX_ITERATIONS="${MAX_ITERATIONS:-30}"
iteration=0
while :; do
  iteration=$((iteration + 1))
  
  if [[ $iteration -gt $MAX_ITERATIONS ]]; then
    echo "Ralph reached max iterations ($MAX_ITERATIONS). Done."
    exit 0
  fi

  echo ""
  echo "=== Ralph iteration $iteration/$MAX_ITERATIONS ==="

  if [[ ! -f "$PROMPT_FILE" ]]; then
    echo "No $PROMPT_FILE found. Create it with your task instructions."
    echo "Ralph sleeps for 5s..."
    sleep 5
    continue
  fi

  # Feed the prompt to pi (non-interactive mode)
  pi -p "$(cat "$PROMPT_FILE")" --model "$MODEL"

  echo "Ralph loop complete. Sleeping 2s before next..."
  sleep 2
done