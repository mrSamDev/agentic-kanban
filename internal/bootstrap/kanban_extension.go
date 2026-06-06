package bootstrap

const KanbanExtension = `import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";
import { Type } from "typebox";
import { Text, matchesKey, Key } from "@earendil-works/pi-tui";
import { existsSync } from "node:fs";
import { join } from "node:path";
import { execSync } from "node:child_process";

export default function (pi: ExtensionAPI) {
  // --- auto-detect kanban binary ---
  function resolveKanban(cwd: string): string {
    const local = join(cwd, "kanban");
    if (existsSync(local)) return local;
    try {
      const out = execSync("which kanban 2>/dev/null", { encoding: "utf8" }).trim();
      if (out) return out;
    } catch {
      // not in PATH
    }
    return "";
  }

  let kanbanBin = "";

  function kanbanReady(): boolean {
    return kanbanBin !== "" && existsSync(kanbanBin);
  }

  // --- helpers ---
  function getDBPath(cwd: string): string {
    let dir = cwd;
    for (let i = 0; i < 10; i++) {
      const candidate = join(dir, ".kanban", "kanban.db");
      if (existsSync(candidate)) return candidate;
      const parent = join(dir, "..");
      if (parent === dir) break;
      dir = parent;
    }
    return join(cwd, ".kanban", "kanban.db");
  }

  function dbFlag(ctx: { cwd: string }): string[] {
    const db = getDBPath(ctx.cwd);
    if (existsSync(db)) return ["--db", db];
    return [];
  }

  async function execKanban(
    args: string[],
    ctx: { cwd: string },
  ): Promise<{ content: { type: "text"; text: string }[]; isError?: boolean }> {
    if (!kanbanBin) kanbanBin = resolveKanban(ctx.cwd);
    if (!kanbanReady()) {
      return {
        content: [{ type: "text", text: "kanban binary not found in PATH or project root" }],
        isError: true,
      };
    }
    try {
      const result = await pi.exec(kanbanBin, [...dbFlag(ctx), ...args], { timeout: 30_000 });
      if (result.code === 0) {
        return { content: [{ type: "text", text: result.stdout || "(empty)" }] };
      }
      return {
        content: [{ type: "text", text: result.stderr || "(error)" }],
        isError: true,
      };
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      return {
        content: [{ type: "text", text: "kanban exec failed: " + msg }],
        isError: true,
      };
    }
  }

  // --- session_start: resolve binary, set footer ---
  pi.on("session_start", async (_event, ctx) => {
    kanbanBin = resolveKanban(ctx.cwd);
    if (!kanbanReady()) {
      ctx.ui.setStatus("kanban", "kanban: not found");
      return;
    }
    ctx.ui.setStatus("kanban", "kanban idle");

    // Quick status check in background
    try {
      const result = await pi.exec(kanbanBin, [
        ...dbFlag(ctx),
        "task",
        "search",
      ], { timeout: 5_000 });
      if (result.code === 0 && result.stdout) {
        const parsed = JSON.parse(result.stdout) as { status?: string }[];
        if (Array.isArray(parsed) && parsed.length > 0) {
          const counts: Record<string, number> = {};
          for (const t of parsed) {
            const s = t.status ?? "UNKNOWN";
            counts[s] = (counts[s] || 0) + 1;
          }
          const parts = Object.entries(counts)
            .map(([k, v]) => k + ":" + v)
            .join(" ");
          ctx.ui.setStatus("kanban", "kanban " + parts);
        } else {
          ctx.ui.setStatus("kanban", "kanban 0 tasks");
        }
      }
    } catch {
      ctx.ui.setStatus("kanban", "kanban err");
    }
  });

  // --- register tools ---

  pi.registerTool({
    name: "claim_next_task",
    label: "Claim Next Task",
    description: "Claim the highest-priority unclaimed task for a role. Returns empty {} if no work. Use this instead of bash kanban claim-next.",
    promptSnippet: "Claim the highest-priority unclaimed task for a role",
    promptGuidelines: [
      "Use claim_next_task when the user asks to pick up the next available work item — do not use bash kanban claim-next.",
      "claim_next_task automatically reclaims tasks whose lease expired (15 min without heartbeat).",
    ],
    parameters: Type.Object({
      agent: Type.String({ description: "Your agent identifier (required)" }),
      role: Type.String({ description: "Role: worker, reviewer, etc. (required)" }),
    }),
    async execute(_toolCallId, params, _signal, _onUpdate, ctx) {
      return execKanban(["task", "claim-next", "--agent", params.agent, "--role", params.role], ctx);
    },
  });

  pi.registerTool({
    name: "dispatch_task",
    label: "Dispatch Task",
    description: "Create a new task on the kanban board. Tasks start as TODO and are picked up by workers.",
    promptSnippet: "Create a new kanban task",
    promptGuidelines: [
      "Use dispatch_task to create new work items — do not use bash kanban task dispatch.",
    ],
    parameters: Type.Object({
      title: Type.String({ description: "Task title (required)" }),
      role: Type.Optional(Type.String({ description: "Role boundary: worker, reviewer, etc. Default: worker" })),
      priority: Type.Optional(Type.Number({ description: "Priority: lower = more urgent. Default: 100" })),
      project: Type.Optional(Type.String({ description: "Project/scope label. Default: default" })),
    }),
    async execute(_toolCallId, params, _signal, _onUpdate, ctx) {
      const args = ["task", "dispatch", "--title", params.title];
      if (params.role) args.push("--role", params.role);
      if (params.priority !== undefined) args.push("--priority", String(params.priority));
      if (params.project) args.push("--project", params.project);
      return execKanban(args, ctx);
    },
  });

  pi.registerTool({
    name: "log_progress",
    label: "Log Progress",
    description: "Log a progress note and renew lease (heartbeat) to prevent lease expiry on a claimed task.",
    promptSnippet: "Log progress and renew lease on a claimed task",
    promptGuidelines: [
      "Use log_progress to periodically report progress on a claimed task — this renews the 15-minute lease and prevents other agents from reclaiming it.",
    ],
    parameters: Type.Object({
      task_id: Type.String({ description: "Task ID (e.g. TASK-1)" }),
      agent: Type.String({ description: "Your agent identifier" }),
      note: Type.String({ description: "Progress description" }),
      type: Type.Optional(Type.String({ description: "Note type: PROGRESS, ERROR, DECISION, REVIEW, SYSTEM. Default: PROGRESS" })),
    }),
    async execute(_toolCallId, params, _signal, _onUpdate, ctx) {
      const args = ["task", "log-progress", params.task_id, "--agent", params.agent, "--note", params.note];
      if (params.type) args.push("--type", params.type);
      return execKanban(args, ctx);
    },
  });

  pi.registerTool({
    name: "block_task",
    label: "Block Task",
    description: "Mark a claimed task as blocked with an explanation and clear the lease so other agents know the task is stuck.",
    promptSnippet: "Mark a claimed task as blocked",
    promptGuidelines: [
      "Use block_task when you cannot make progress on a task — provide a clear reason so a manager can unblock or reassign.",
    ],
    parameters: Type.Object({
      task_id: Type.String({ description: "Task ID (e.g. TASK-1)" }),
      agent: Type.String({ description: "Your agent identifier" }),
      reason: Type.String({ description: "Why the task is blocked" }),
    }),
    async execute(_toolCallId, params, _signal, _onUpdate, ctx) {
      return execKanban(["task", "block", params.task_id, "--agent", params.agent, "--reason", params.reason], ctx);
    },
  });

  pi.registerTool({
    name: "complete_task",
    label: "Complete Task",
    description: "Mark a claimed task as done. Optionally submit for review instead of marking DONE directly.",
    promptSnippet: "Mark a task done or submit for review",
    promptGuidelines: [
      "Use complete_task when you finish working on a claimed task. Pass --review to submit for human or reviewer approval.",
    ],
    parameters: Type.Object({
      task_id: Type.String({ description: "Task ID (e.g. TASK-1)" }),
      agent: Type.String({ description: "Your agent identifier" }),
      review: Type.Optional(Type.Boolean({ description: "Submit for review instead of marking done. Default: false" })),
    }),
    async execute(_toolCallId, params, _signal, _onUpdate, ctx) {
      const args = ["task", "complete", params.task_id, "--agent", params.agent];
      if (params.review) args.push("--review");
      return execKanban(args, ctx);
    },
  });

  pi.registerTool({
    name: "approve_task",
    label: "Approve Task",
    description: "Approve a task in IN_REVIEW state, marking it DONE. Any reviewer can approve.",
    promptSnippet: "Approve an IN_REVIEW task as DONE",
    promptGuidelines: [
      "Use approve_task when you have reviewed a task submitted for review and it meets the requirements.",
    ],
    parameters: Type.Object({
      task_id: Type.String({ description: "Task ID (e.g. TASK-1)" }),
      agent: Type.String({ description: "Reviewer agent identifier" }),
    }),
    async execute(_toolCallId, params, _signal, _onUpdate, ctx) {
      return execKanban(["task", "approve", params.task_id, "--agent", params.agent], ctx);
    },
  });

  pi.registerTool({
    name: "reject_task",
    label: "Reject Task",
    description: "Reject a task in IN_REVIEW state, sending it back to TODO for rework.",
    promptSnippet: "Reject an IN_REVIEW task back to TODO",
    promptGuidelines: [
      "Use reject_task when a submitted task does not meet requirements — provide a clear reason so the worker knows what to fix.",
    ],
    parameters: Type.Object({
      task_id: Type.String({ description: "Task ID (e.g. TASK-1)" }),
      agent: Type.String({ description: "Reviewer agent identifier" }),
      reason: Type.String({ description: "Why the task was rejected" }),
    }),
    async execute(_toolCallId, params, _signal, _onUpdate, ctx) {
      return execKanban(["task", "reject", params.task_id, "--agent", params.agent, "--reason", params.reason], ctx);
    },
  });

  pi.registerTool({
    name: "review_backlog",
    label: "Review Backlog",
    description: "Search tasks by status, role, agent, or project to see what is available, blocked, or done.",
    promptSnippet: "Search and review the task backlog",
    promptGuidelines: [
      "Use review_backlog to inspect the task board — filter by status (TODO, IN_PROGRESS, BLOCKED, IN_REVIEW, DONE), role, or agent.",
    ],
    parameters: Type.Object({
      status: Type.Optional(Type.String({ description: "Filter: TODO, IN_PROGRESS, BLOCKED, IN_REVIEW, DONE" })),
      role: Type.Optional(Type.String({ description: "Filter by role boundary: worker, reviewer" })),
      agent: Type.Optional(Type.String({ description: "Filter by assigned agent" })),
      project: Type.Optional(Type.String({ description: "Filter by project/scope" })),
    }),
    async execute(_toolCallId, params, _signal, _onUpdate, ctx) {
      const args = ["task", "search"];
      if (params.status) args.push("--status", params.status);
      if (params.role) args.push("--role", params.role);
      if (params.agent) args.push("--agent", params.agent);
      if (params.project) args.push("--project", params.project);
      return execKanban(args, ctx);
    },
  });

  pi.registerTool({
    name: "view_task",
    label: "View Task",
    description: "View full task details including notes and history. Useful when you receive a task ID and need context.",
    promptSnippet: "View full task details including notes and history",
    promptGuidelines: [
      "Use view_task before starting work on a claimed task to see notes and history from previous workers.",
    ],
    parameters: Type.Object({
      task_id: Type.String({ description: "Task ID (e.g. TASK-1)" }),
    }),
    async execute(_toolCallId, params, _signal, _onUpdate, ctx) {
      return execKanban(["task", "view", params.task_id], ctx);
    },
  });

  // --- /kanban command ---
  pi.registerCommand("kanban", {
    description: "Show kanban board status (task counts by status). Pass no args for board view.",
    handler: async (args, ctx) => {
      if (args.trim().length > 0) {
        ctx.ui.notify("kanban: /kanban takes no arguments. Use kanban init --plan <file> to dispatch a plan.", "warning");
      }
      if (!kanbanReady()) {
        ctx.ui.notify("kanban: binary not found", "error");
        return;
      }
      try {
        const result = await pi.exec(kanbanBin, [...dbFlag(ctx), "task", "search"], { timeout: 10_000 });
        if (result.code === 0 && result.stdout) {
          const tasks = JSON.parse(result.stdout) as { id: string; title: string; status: string; priority: number }[];
          if (!Array.isArray(tasks) || tasks.length === 0) {
            ctx.ui.notify("kanban: no tasks", "info");
            return;
          }
          const byStatus: Record<string, typeof tasks> = {};
          for (const t of tasks) {
            const s = t.status ?? "UNKNOWN";
            (byStatus[s] ??= []).push(t);
          }

          const lines: string[] = [];
          lines.push("── kanban board ──");
          for (const [status, items] of Object.entries(byStatus)) {
            lines.push(status + " (" + items.length + "):");
            for (const t of items.slice(0, 10)) {
              lines.push("  " + t.id + " " + t.title + " [p" + t.priority + "]");
            }
            if (items.length > 10) lines.push("  ... +" + (items.length - 10) + " more");
          }
          lines.push("");
          lines.push("Press escape, enter, or q to close");

          await ctx.ui.custom((_tui, _theme, _kb, done) => {
            const text = new Text(lines.join("\n"), 2, 1);
            return {
              render(width: number): string[] {
                return text.render(width);
              },
              invalidate(): void {
                text.invalidate();
              },
              handleInput(data: string): void {
                if (matchesKey(data, Key.escape) || matchesKey(data, Key.enter) || data === "q") {
                  done(undefined);
                }
              },
            };
          });
        } else {
          ctx.ui.notify("kanban: no tasks or error", "warning");
        }
      } catch {
        ctx.ui.notify("kanban: not found or not initialized", "error");
      }
    },
  });
}
`

const KanbanExtensionName = "kanban.ts"