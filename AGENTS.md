## Context Acquisition Rules

To minimize token usage and unnecessary code exploration:

1. Start with the exact file, symbol, or line range requested.
2. Read the enclosing function or type only when the requested context is insufficient to safely complete the task.
3. Read referenced symbols only when their behavior cannot be inferred from the current context.
4. Do not read entire files unless:
   - the target symbol cannot be located, or
   - the change affects multiple symbols throughout the file.
5. Do not perform repository-wide searches unless:
   - explicitly requested, or
   - required to determine the impact of a change.
6. Prefer symbol-based reads over file-based reads.
7. Prefer targeted line ranges over full-file reads.
8. Gather the minimum context necessary to make a safe change.
9. Before expanding context, explain why the currently available context is insufficient.
10. Avoid exploratory reading that is not directly related to the current task.