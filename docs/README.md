# Argus documentation

This is the documentation for the Argus desktop agent app. It is written for people
working on the codebase, and organized by what you are trying to do rather than by
which folder the code lives in.

## How the docs are organized

| Section | Answers the question | Start here if you |
|---|---|---|
| [Getting started](getting-started.md) | How do I build, run, and pair the app? | are new to the codebase |
| [Concepts](#concepts) | How is Argus designed, and why? | want to understand the system before changing it |
| [Guides](#guides) | How does feature X actually work end to end? | are implementing or debugging a feature |
| [Reference](#reference) | What exactly are the tools, events, env vars, and tables? | need a precise fact (a name, a path, a schema) |
| [Development](development.md) | How do I work on the repo day to day? | are editing, testing, or generating bindings |
| [FAQ](faq.md) | Why does it behave this way? | have a "where/why/what happens if" question |

A rule of thumb: **concepts explain, guides walk through, reference states.**
If you find the same fact stated in two places, one of them is wrong — keep the
reference copy and link to it from everywhere else.

## Concepts

- [Architecture overview](concepts/architecture.md) —
  Wails backend, React frontend, service wiring, startup, event flow, and the
  main architectural decisions.
- [Chat pipeline](concepts/chat-pipeline.md) —
  the six stages of a chat turn, what each stage owns, and what it must not do.
- [Security](concepts/security.md) —
  trust boundaries, secrets, pairing, permissions, account isolation, and local
  data boundaries.
- [Gateway architecture](concepts/gateway/README.md) —
  the provider/models/catalog stack:
  [providers](concepts/gateway/providers.md),
  [models](concepts/gateway/models.md),
  [catalog](concepts/gateway/catalog.md).

## Guides

- [MCP and integrations](guides/mcp-integrations.md) —
  connections, OAuth, per-agent bindings and access tiers, connect/reauth flows.
- [Browser automation](guides/browser-automation.md) —
  the Argus-owned Chrome session, what it can do, takeover, and safety gates.
- [Scheduler and background tasks](guides/scheduler.md) —
  durable routines and reminders, unattended runs, recovery.
- [Skills and memory](guides/skills-and-memory.md) —
  progressive skill loading and the structured memory system.
- [Releases and secrets](guides/releases-and-secrets.md) —
  where signing keys live, and how a tagged release is published.

## Reference

- [Features](reference/features.md) —
  what each major part of Argus does and the code that implements it.
- [Tools](reference/tools.md) —
  the built-in tool catalog the agent can call, with permission notes.
- [Events](reference/events.md) —
  every runtime event the backend emits, from `Services/Shared/events.go`.
- [Configuration](reference/configuration.md) —
  environment variables, build-time settings, and on-disk paths.
- [Storage](reference/storage.md) —
  the SQLite schema, keyring entries, disk layout, and cleanup behavior.

## Scope and conventions

These docs describe the codebase as it exists in this repository. If you change
behavior, storage schema, service contracts, event payloads, or security-sensitive
flows, update the affected documents in the same change — an outdated doc is worse
than a missing one.

When adding a new document, put it in the section above that matches what a reader
is trying to do, add it to this index, and cross-link it from related pages.
