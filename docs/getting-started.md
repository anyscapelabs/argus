# Getting started

This page gets a new contributor from clone to a running app. For day-to-day
workflows (bindings, packaging, server mode, conventions), see the
[development guide](development.md).

## Prerequisites

- **Go** 1.25+ (check `go.mod` for the exact toolchain)
- **Node.js** 24+ and npm
- **Wails v3 CLI**: <https://v3.wails.io/>
- (Linux) the usual desktop/webkit build dependencies for Wails

## Install and run

```sh
# install frontend dependencies
cd frontend && npm install
cd ..

# run the app with hot reload
wails3 dev -config ./build/config.yml -port 9245

# or via Task
task dev
```

## Pair the app

Argus pairs against the Anyscape Gateway:

1. Create a service-account key in the Gateway console (Settings → Service
   Accounts).
2. Paste it into the app's sign-in screen. The key is verified against the
   gateway and stored in the OS keyring.
3. Entitlements are cached locally so the app works offline within the
   supported grace period.

Model traffic follows the gateway the key paired against, not the build-time
`VITE_ANYSCAPE_GATEWAY_URL` (see [configuration](reference/configuration.md)).

## Build and test

```sh
# production build
wails3 build        # or: task build

# both test suites (Go + frontend Vitest)
task test
```

Frontend tests alone: `task test:frontend` or `cd frontend && npm test`.

## Repo orientation

| Directory | What it is |
|---|---|
| `frontend/` | React 18 + TypeScript UI; `frontend/bindings/` holds generated Wails bindings (do not hand-edit) |
| `Services/` | Backend domain services (chat, account, permissions, MCP, browser, scheduler, artifacts, uploads, skills, ...) |
| `Storage/` | SQLite database, migrations, persistence helpers |
| `Provider/`, `Catalog/`, `Router/`, `Protocol/` | Provider overlay, catalog, routing, model protocol adapters |
| `Keyring/` | OS keyring access |
| `build/`, `Taskfile.yml` | Wails/platform build configuration and tasks |

## What to read next

1. [Architecture overview](concepts/architecture.md) — how the pieces fit.
2. [Chat pipeline](concepts/chat-pipeline.md) — the core workflow.
3. [Features](reference/features.md) — what each service does and where it lives.
4. [Development guide](development.md) — bindings regeneration, testing, conventions.
