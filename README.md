# Argus Agent (desktop)

AI agent desktop app: chat with durable history, artifacts, voice, MCP tools
and browser automation. Wails v3 (Go) backend + React 18 frontend.

## Develop

Requirements: Go 1.25+, Node 24+, [`wails3`](https://v3.wails.io/).

```sh
# frontend
cd frontend && npm install

# run the app with hot reload
wails3 dev -config ./build/config.yml -port 9245
```

## Build / test

```sh
wails3 build                     # production binary for the host OS
go test $(go list ./... | grep -v build/ios)
cd frontend && npm test          # vitest suite (also: task test:frontend)
```

## Architecture

- `frontend/` — React UI. Pairs with the backend over generated Wails
  bindings (`frontend/bindings`, regenerated after Go signature changes with
  `wails3 generate bindings`).
- `Storage/` — SQLite store (WAL; one writer). Message history, artifacts,
  memories, scheduled tasks, seq counters, etc.
- `Services/` — domain services: chat (history builder, streaming), models,
  account (pairing/entitlements), MCP, integrations, uploads, scheduler.
- `Provider/` — the single embedded provider overlay (`argus-router`,
  OpenAI-compatible) targeted at the Anyscape Gateway.

## Pairing the agent

1. Create a service-account key in the Anyscape Gateway console
   (Settings → Service Accounts).
2. Paste it into the app's sign-in screen. The key is verified against the
   gateway and stored in the OS keyring — never on disk in plaintext.
3. The app works offline for up to 7 days on the last synced entitlement.

## Configuration

- `frontend/.env`: `VITE_ANYSCAPE_GATEWAY_URL` — gateway origin the desktop
  pairs with (default `http://localhost:8080`). `VITE_ARGUS_API_URL` is a
  deprecated alias. **Build-time** — model traffic follows the gateway the
  key paired against (persisted as the provider-config base-URL override at
  pair time), not this file alone.
- `ARGUS_LOAD_DOTENV=1` — opt-in: also loads `.env` files for local OAuth
  client ids during development. Off in packaged builds.

## Security model

- Keys never leave the OS keyring; the frontend only sees presence/status.
- The gateway is the only model origin; per-plan allowlists are enforced
  server-side (and fail closed).
- Sign-out clears entitlements + provider override; pairing a *different*
  account wipes account-scoped local data (chats, memories, artifacts) —
  the previous tenant never leaks into the next.
