# Contributing

Thanks for your interest in agentic-kanban.

## Before you start

- Read the [README](README.md) to understand what this project is and isn't.
- Check the [ROADMAP](ROADMAP.md) to see what's planned.
- Look at open issues before starting work — someone may already be on it.

## Pull requests

- PRs against `main`. Keep them small and focused.
- One feature or fix per PR. Do not bundle unrelated changes.
- Keep diffs minimal — see [AGENTS.md](AGENTS.md) for the project's change philosophy.
- Write tests for new functionality. Run the full suite with:

```bash
go test ./...
```

- Ensure the build compiles cleanly:

```bash
go build -o kanban ./cmd/kanban/
```

- Update `README.md` if you add or change CLI commands.
- Update skill templates under `internal/bootstrap/embed/skills/` if your change affects agent protocol steps.

## Development setup

Requires Go 1.24 or later.

```bash
git clone https://github.com/mrSamDev/agentic-kanban.git
cd agentic-kanban
go build -o kanban ./cmd/kanban/
```

The `kanban` binary is self-contained. Use `--debug` to trace database ops:

```bash
./kanban --debug init --harness pi
./kanban --debug task dispatch --title "test" --role worker
```

### Skills

Coordination skills ship embedded in the binary at `internal/bootstrap/embed/skills/`. Each role gets its own directory (`manager/`, `worker/`, `reviewer/`). Custom skills can be added at `./kanban/hooks/` — see the README for details.

## Code of conduct

This project follows a [Code of Conduct](CODE_OF_CONDUCT.md). All participants are expected to uphold it.

## No CLA

There is no Contributor License Agreement. By submitting a pull request, you agree to license your contribution under the same [MIT License](LICENSE) as the project.

## Questions?

Open a [Discussion](https://github.com/mrSamDev/agentic-kanban/discussions) or an issue.