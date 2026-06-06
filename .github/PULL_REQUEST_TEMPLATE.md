name: Pull Request
description: Submit a change to the project
title: ""
labels: []
body:
  - type: markdown
    attributes:
      value: |
        Thanks for contributing. Keep PRs small and focused. One feature or fix per PR.
        See [CONTRIBUTING.md](../CONTRIBUTING.md) for guidelines.
  - type: textarea
    id: description
    attributes:
      label: Description
      description: What does this change do and why?
    validations:
      required: true
  - type: dropdown
    id: type
    attributes:
      label: Type of change
      options:
        - Bug fix
        - New feature
        - Refactor (no behavior change)
        - Documentation
        - CI / tooling
    validations:
      required: true
  - type: checkboxes
    id: checks
    attributes:
      label: Checklist
      options:
        - label: "`go build -o kanban ./cmd/kanban/` compiles cleanly"
        - label: "`go test ./...` passes"
        - label: README updated if CLI behavior changed
        - label: Skill templates updated if protocol changed
  - type: textarea
    id: testing
    attributes:
      label: How was this tested?
      description: Manual steps, test output, multi-agent scenarios, etc.
  - type: textarea
    id: related
    attributes:
      label: Related issues
      description: Closes #..., etc.