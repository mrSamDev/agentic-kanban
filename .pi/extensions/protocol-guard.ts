import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";
import { existsSync } from "node:fs";
import { relative, resolve } from "node:path";

// Files that are pure CLI ergonomics / visual polish — NOT protocol.
// Edits to these trigger a warning when the LLM touches them.
const CLI_ONLY_PATHS = [
  // CLI command layer — not the protocol engine
  "cmd/kanban/prune.go",
  "cmd/kanban/upgrade.go",
  "cmd/kanban/init.go",
  "cmd/kanban/update.go",
  "cmd/kanban/output.go",
  "cmd/kanban/skill_cmd.go",

  // Markdown docs — not protocol
  "README.md",
  "CONTRIBUTING.md",
  "CODE_OF_CONDUCT.md",
  "SECURITY.md",
  "ROADMAP.md",
  "AGENTS.md",
  "plan.md",
  "planv2.md",

  // Extension code for visual/interactive features
  // (kanban.ts at .pi/extensions IS protocol — tool wrappers for agents)
  // But gap.ts, caveman, etc are not kanban protocol
];

// Protocol core files — where real coordination lives.
// Edits here are always allowed (but still get the reminder).
const PROTOCOL_CORE = [
  "internal/storage/schema.sql",
  "internal/storage/sqlite.go",
  "internal/task/model.go",
  "internal/task/service.go",
  "internal/task/claim.go",
  "internal/task/review.go",
  "internal/task/queries.go",
  "internal/task/helpers.go",
  "internal/task/events.go",
  "internal/task/hooks.go",
  "internal/task/batch.go",
  "internal/task/lint.go",

  // Skill files — the agent runtime (protocol!)
  "embed/skills/",
  "bootstrap/embed/skills/",
];

function isCLIOnly(filePath: string, projectRoot: string): boolean {
  const rel = relative(projectRoot, filePath);
  for (const p of CLI_ONLY_PATHS) {
    if (rel === p || rel.startsWith(p)) return true;
  }
  return false;
}

function isProtocolCore(filePath: string, projectRoot: string): boolean {
  const rel = relative(projectRoot, filePath);
  for (const p of PROTOCOL_CORE) {
    if (rel === p || rel.startsWith(p)) return true;
  }
  return false;
}

export default function (pi: ExtensionAPI) {
  // ── Proactive: inject rule into system prompt every turn ──
  pi.on("before_agent_start", async (event, _ctx) => {
    return {
      systemPrompt:
        event.systemPrompt +
        `

── Protocol-First Decision Rule ──

Every change to the kanban codebase must pass this test:

"Does this make the coordination protocol more reliable or more
useful for multi-agent coordination?"

If the answer is "CLI ergonomics" or "visual polish" → DO NOT BUILD IT.

The product is a coordination protocol. Not a CLI tool. Not a dashboard.
Not a progress bar. Not a config wizard. Not a better --help output.

The protocol = SQLite DB schema + state transitions (claim/complete/block/
approve/reject/dispatch) + lease model + dependency model + event log.
Plus the skill files that teach agents how to use the protocol.

What NOT to spend time on:
- Better table formatting
- Colorized output
- Spinners and progress bars
- Prompts/wizards for humans
- README rewrites
- install.sh formatting
- Version check UI
- Any /kanban command visual polish

What IS worth time:
- Fixing bugs in dispatch/claim/complete state transitions
- Adding batch operations (batch claim, batch complete)
- Adding dependency enforcement
- Adding cross-agent review gate
- Adding new protocol skills (embed/skills/)
- Improving SQLite schema or migration
- Adding event types or hook payloads
- Status output in JSON (agents need it; humans don't)

When you catch yourself writing CLI polish, stop and ask:
"Can an agent read this programmatically? If not, who benefits?"
The answer should always be: the agent.`,
    };
  });

  // ── Reactive: warn after every edit/write to CLI-only files ──
  pi.on("tool_result", async (event, ctx) => {
    if (event.toolName !== "edit" && event.toolName !== "write") return;

    const input = event.input as Record<string, unknown> | undefined;
    const filePath = input?.path as string | undefined;
    if (!filePath) return;

    const projectRoot = ctx.cwd;
    if (!existsSync(resolve(projectRoot, ".kanban"))) return; // not a kanban project

    if (isProtocolCore(filePath, projectRoot)) return; // protocol changes always OK

    if (isCLIOnly(filePath, projectRoot)) {
      ctx.ui.notify(
        `⚠ Protocol guard: ${relative(projectRoot, filePath)} is CLI polish. Does this pass "make protocol better for agents"?`,
        "warning",
      );

      return {
        content: [
          ...event.content,
          {
            type: "text" as const,
            text: `\n\n⚠ PROTOCOL GUARD WARNING: You edited ${relative(projectRoot, filePath)}, which is a CLI-ergonomics or visual-polish file. Before continuing, confirm this change makes the coordination protocol more reliable or useful for multi-agent coordination. If not, revert it.`,
          },
        ],
      };
    }

    // File outside both categories — unknown. Flag it.
    const rel = relative(projectRoot, filePath);
    if (!rel.startsWith("..") && !rel.startsWith("node_modules") && !rel.startsWith(".kanban")) {
      return {
        content: [
          ...event.content,
          {
            type: "text" as const,
            text: `\n\n⚠ PROTOCOL GUARD: Change to ${rel}. Does this serve the protocol or is it incidental? Verify alignment before committing.`,
          },
        ],
      };
    }
  });
}