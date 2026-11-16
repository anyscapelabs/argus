# Argus

Argus is a desktop AI agent app with durable chat history, artifacts, voice, MCP integrations, browser automation, and scheduled work. It uses a Wails v3 (Go) backend and a React 18 frontend.

## Documentation

Start with the docs index: [docs/README.md](docs/README.md)

- [Architecture overview](docs/concepts/architecture.md)
- [Security](docs/concepts/security.md)
- [Data and storage](docs/reference/storage.md)
- [Services and features](docs/reference/features.md)
- [Development guide](docs/development.md)
- [Chat pipeline](docs/concepts/chat-pipeline.md)
- [Gateway architecture](docs/concepts/gateway/README.md)
  - [Providers](docs/concepts/gateway/providers.md)
  - [Models](docs/concepts/gateway/models.md)
  - [Catalog](docs/concepts/gateway/catalog.md)

## Quick start

Requirements: Go (see `go.mod`), Node.js/npm, and the [Wails v3 CLI](https://v3.wails.io/).

```sh
# install frontend dependencies
cd frontend && npm install

# run the app with hot reload
cd ..
wails3 dev -config ./build/config.yml -port 9245
```

## Build and test

```sh
# production build
wails3 build

# tests
go test $(go list ./... | grep -v build/ios)
cd frontend && npm test
```

You can also use Task targets from `Taskfile.yml` (for example `task dev`, `task build`, `task test`).

## Project layout

- `frontend/` — React UI, domain hooks/API wrappers, and generated Wails bindings (`frontend/bindings/`)
- `Services/` — backend domains (chat, account, permissions, MCP, browser, scheduler, artifacts, uploads, skills, etc.)
- `Storage/` — SQLite database, migrations, and persistence helpers
- `Provider/`, `Catalog/`, `Router/`, `Protocol/` — provider overlay, catalog, routing, and model protocol adapters
- `Keyring/` — OS keyring integration for secrets
- `docs/` — architecture, security, storage, and development documentation

## Pairing and configuration

1. Create a service-account key in the Anyscape Gateway console (Settings → Service Accounts).
2. Paste it into the app’s sign-in screen. The key is verified against the gateway and stored in the OS keyring.
3. Entitlements are cached locally so the app can work offline within the supported grace period.

Key configuration points:

- `frontend/.env`: `VITE_ANYSCAPE_GATEWAY_URL` (build-time gateway origin; `VITE_ARGUS_API_URL` is a deprecated alias)
- `ARGUS_LOAD_DOTENV=1`: opt-in dev loading of `.env` files for local MCP OAuth client credentials
- `ARGUS_BROWSER_PROFILE_DIR`: optional override for the Argus browser profile location

## Security at a glance

- Secrets (account key, MCP tokens) stay in the OS keyring; the frontend does not receive raw secret values.
- Model traffic is routed through the paired gateway; allowlists and authorization are enforced server-side.
- Sensitive tool actions go through permission checks; unattended runs refuse interactive approvals.
- Signing out clears pairing/entitlement state. Pairing a different account wipes account-scoped local data to prevent cross-tenant leakage.

For details, see [docs/concepts/security.md](docs/concepts/security.md) and [docs/reference/storage.md](docs/reference/storage.md).
