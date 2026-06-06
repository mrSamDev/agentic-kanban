# Roadmap

## v0.4 — Safe multi-agent execution

- [ ] `depends_on` column + claim guard — parallel workers can't claim blocked tasks
- [ ] `extend-lease` command — long-running workers renew leases before expiry
- [ ] Cross-agent review gate (default on, configurable off) — workers can't approve their own tasks

## v0.5 — Fast multi-agent execution

- [ ] `claim-next --count N` — claim up to N tasks atomically in one call
- [ ] Optional subagent delegation — `manager_mode = serial | parallel` in config
- [ ] Project env auto-detection — walk up from CWD to find `.kanban/` DB

## v0.6 — Operational maturity

- [ ] `kanban status` with `--burndown` and `--json` flags
- [ ] `kanban plan lint` — catch cycles, unknown deps, missing roles before execution
- [ ] `approve-plan --all` flag — dispatch all tasks regardless of checkbox state

## v0.7 — Worker pools and scheduling

- [ ] Worker pools with load balancing
- [ ] Automatic task assignment
- [ ] Scheduler / coordinator process

---

## Phase 1 — Skills First-Class CLI

Goal: make skills visible and operable from the CLI, not just files on disk.

- [ ] `kanban skill list` — show all installed skills with file path
- [ ] `kanban skill list --role manager` — list skills for a role
- [ ] `kanban skill view <name>` — print rendered skill markdown + metadata
- [ ] `kanban skill validate` — schema-check every skill file:
  - Required sections present (Description, Usage, etc.)
  - Referenced commands exist in `kanban` CLI
  - Valid YAML frontmatter (role, type)
- [ ] `kanban skill validate <name>` — single skill check

## Phase 2 — Skill Metadata (Role Index + Protocol/Custom Tagging)

Goal: agents can distinguish "this skill is for managers" and "this is a coordination skill" from the file itself, without directory layout.

- [ ] Add YAML frontmatter to embedded skill templates:
  ```yaml
  ---
  role: manager
  type: protocol
  ---
  ```
- [ ] Single `.skills-index.json` schema written at init time:
  ```json
  {
    "claim-next-task.md": {"role": "worker", "type": "protocol", "hash": "sha256..."},
    ...
  }
  ```
- [ ] `writeFlatSkills()` writes `.skills-index.json` alongside flat dir instead of losing role info
- [ ] `kanban skill list` reads metadata from frontmatter + index, not filesystem paths
  - `--protocol` filter to coordination skills (shipped)
  - `--custom` filter to user-added skills
- [ ] `kanban skill add <path>` — import a custom skill, tags it `type: custom`
- [ ] `kanban skill remove <name>` — unregister a custom skill
- [ ] Protocol skills are read-only (no remove, no edit)
- [ ] Pi/Claude/Generic harnesses write `.skills-index.json` with role mapping

## Phase 3 — Skills Upgrade

Goal: sync embedded skill updates to existing projects without manual copy.

- [ ] `kanban skill upgrade` — compare embedded vs installed skills, update stale ones
- [ ] `kanban skill upgrade --harness pi` — scoped to a harness
- [ ] `kanban skill upgrade --dry-run` — show what would change
- [ ] Conflict detection via content hash in `.skills-index.json`:
  - Embedded hash matches installed hash → update (no local changes)
  - Hashes differ → flag as "locally modified, skipping" → user must `--force`
- [ ] Custom skills are never overwritten on upgrade
- [ ] Upgrade logs a summary: `3 updated, 2 unchanged, 0 custom skipped, 1 locally modified (use --force to overwrite)`

## Phase 4 — Skill Validation Hardening

Goal: catch skill errors before runtime, not when agent reads them.

- [ ] Schema: validate frontmatter has `role` (enum: manager/worker/reviewer) and `type` (enum: protocol/custom)
- [ ] Schema: validate all referenced kanban commands exist (`kanban <subcommand>`)
- [ ] Schema: validate all referenced flags exist on those commands
- [ ] Schema: validate no bare markdown (empty sections, unresolved placeholders)
- [ ] `kanban skill validate` runs on all skills, fails fast
- [ ] `kanban init` runs validation on scaffolded skills automatically

## Future

- [ ] Skill dependencies — one skill declares `requires: [claim-next-task]`
- [ ] Skill templates — `kanban skill init --name <name>` scaffolds a custom skill
- [ ] Skill versioning — frontmatter `version: 1` for upgrade diffing
- [ ] Skill marketplace — pull coordination skills from registry