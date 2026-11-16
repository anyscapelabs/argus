# Development guide

This guide covers local development, builds, tests, configuration, and common workflows for the Argus codebase.

## Prerequisites

- **Go** (see `go.mod` for the module/toolchain version)
- **Node.js** and npm (for the frontend)
- **Wails v3 CLI**: <https://v3.wails.io/>

The root `README.md` notes the expected baseline (Go 1.25+, Node 24+). Use versions compatible with the current `go.mod` and frontend tooling.

## Repository layout

- `frontend/` — React + TypeScript + Vite UI
- `frontend/bindings/` — generated Wails bindings (do not hand-edit as source of truth)
- `Services/` — backend domain services
- `Storage/` — SQLite storage, migrations, and data access
- `Provider/`, `Catalog/`, `Router/`, `Protocol/` — model/provider/catalog/routing stack
- `Keyring/` — OS keyring access
- `docs/` — project documentation
- `build/` — Wails/platform build configuration and tasks
- `Taskfile.yml` — task runner entry points

## Frontend setup

```sh
cd frontend
npm install
```

### Frontend scripts

From `frontend/package.json`:

- `npm run dev` — Vite dev server
- `npm run build:dev` — TypeScript + Vite development build (unminified)
- `npm run build` — TypeScript + Vite production build
- `npm run preview` — preview built frontend
- `npm test` — run Vitest suite

## Running the app in development

From the repository root:

```sh
wails3 dev -config ./build/config.yml -port 9245
```

Or via Task:

```sh
task dev
```

`Taskfile.yml` forwards to `wails3 dev` with the configured Vite port (`VITE_PORT`, default 9245).

## Building

```sh
wails3 build
```

Or:

```sh
task build
```

Platform-specific packaging/build behavior is defined under `build/` and the included Taskfiles (`build/windows`, `build/darwin`, `build/linux`, `build/ios`, `build/android`).

## Tests

Run both suites from the repo root:

```sh
task test
```

This runs:

```sh
go test $(go list ./... | grep -v build/ios)
cd frontend && npm test
```

You can run frontend tests alone with:

```sh
task test:frontend
# or
cd frontend && npm test
```

## Wails bindings

The frontend calls backend services through generated bindings in `frontend/bindings/`.

After changing exported Go service methods, DTOs, or signatures used by the frontend, regenerate bindings:

```sh
wails3 generate bindings
```

Commit regenerated binding changes together with the backend/frontend updates that require them.

## Configuration

### Frontend build-time environment

- `frontend/.env`
  - `VITE_ANYSCAPE_GATEWAY_URL` — gateway origin the desktop app is built to pair with (default in local/dev setups may vary; see repo/env configuration).
  - `VITE_ARGUS_API_URL` — deprecated alias.

These are build-time values. At runtime, model traffic follows the gateway/backend the key paired against (persisted as a provider base URL override).

### Backend/development environment

- `ARGUS_LOAD_DOTENV=1` — opt-in. Allows loading `.env` files for local OAuth client IDs/secrets during development (for MCP connectors). Keep this off in packaged builds.
- `ARGUS_BROWSER_PROFILE_DIR` — optional base directory override for the Argus browser profile (the app uses a `browser` subdirectory under it).

## Local data locations

During development, Argus uses the same user-scoped locations as normal runs (platform-dependent):

- SQLite DB: `os.UserConfigDir()/argus/argus.db`
- Uploads: `os.UserConfigDir()/argus/uploads/`
- Generated deck files: `os.UserConfigDir()/argus/decks/`
- Browser profile: `os.UserConfigDir()/argus/browser/` (or override)
- Catalog cache: `os.UserCacheDir()/argus/`

Secrets (account key, MCP tokens/sessions) are stored in the OS keyring, not in the database.

## Working on backend services

- Keep service responsibilities scoped to their domain in `Services/`.
- Shared cross-cutting types/events/helpers live in `Services/Shared/`.
- If you add or change persisted data, update `Storage/migrations.go` and related storage helpers, and consider account purge/cleanup behavior for account-scoped data.
- If you add runtime events or DTOs consumed by the frontend, update shared definitions and regenerate bindings.

## Working on the frontend

- Page-level UI lives in `frontend/src/pages/`.
- Reusable UI lives in `frontend/src/components/`.
- Domain logic (bindings wrappers, React Query hooks, event handling) lives in `frontend/src/domains/`.
- Follow existing styling patterns and theme/color tokens used by the app.

## Documentation

Project docs live in `docs/`. When you change architecture, storage, security behavior, or major features, update the relevant docs (and `docs/README.md` index if you add new documents).

## Commits

- Write clear commit messages that describe the change.
- Do not add co-author trailers.
