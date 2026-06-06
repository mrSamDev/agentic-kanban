# Security Policy

## Supported Versions

This project is **alpha** software. Only the latest commit on `main` receives security updates. There are no LTS or backport releases.

| Version | Supported |
|---|---|
| main (HEAD) | ✅ |
| Older tags | ❌ |

## Reporting a Vulnerability

**Do not open a public GitHub issue.** Report privately via [GitHub Security Advisories](https://github.com/mrSamDev/agentic-kanban/security/advisories/new).

You should receive an initial response within 5 business days. If you don't, follow up by opening an issue with the `security` label — the project is alpha and response may be delayed.

### What to include

- A description of the vulnerability and the potential impact
- Steps to reproduce (preferably a minimal proof of concept)
- Affected versions
- Any proposed fix (optional)

## Disclosure

We follow a 90-day disclosure window. After a fix is released, the vulnerability will be publicly disclosed. Exceptions are made for issues that require protocol changes or ecosystem-wide coordination.

## Scope

SQLite-level or filesystem-level attacks are considered out of scope — the kanban database is only shared between trusted agents on the same machine or shared filesystem. The threat model assumes no malicious access to the `.db` file itself.

## No Bug Bounty

This is an alpha project with no bug bounty program. Vulnerability reports are appreciated and credited in release notes when the reporter consents.