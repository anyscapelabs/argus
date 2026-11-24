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

## Code signing and notarization

`.github/workflows/build.yml` signs and notarizes when the repository secrets
below are set; without them it still produces unsigned artifacts, so forks and
pull requests build fine.

| Secret | Platform | Purpose |
|---|---|---|
| `MACOS_CERTIFICATE_BASE64` | macOS | Developer ID Application certificate exported as `.p12`, base64-encoded |
| `MACOS_CERTIFICATE_PASSWORD` | macOS | Password for that `.p12` |
| `MACOS_SIGNING_IDENTITY` | macOS | Full identity name, e.g. `Developer ID Application: Anyscape Labs PLC (TEAMID)` |
| `MACOS_NOTARY_APPLE_ID` | macOS | Apple ID submitted to `notarytool` |
| `MACOS_NOTARY_TEAM_ID` | macOS | Apple Developer team id |
| `MACOS_NOTARY_PASSWORD` | macOS | App-specific password for `notarytool` |
| `WINDOWS_CERTIFICATE_BASE64` | Windows | Authenticode code-signing certificate exported as `.pfx`, base64-encoded |
| `WINDOWS_CERTIFICATE_PASSWORD` | Windows | Password for that `.pfx` |

The macOS job imports the certificate into a temporary keychain, signs the
`.app` with hardened runtime and `build/darwin/entitlements.plist`, signs the
DMG, then notarizes and staples both. The Windows job signs the executable and
the NSIS installer with `signtool` from the Windows SDK.

MSIX packages need a certificate whose publisher CN matches the manifest:
`task windows:package FORMAT=msix CERT_PATH=path/to.pfx PUBLISHER="CN=Anyscape Labs PLC"`.

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

- `ARGUS_LOAD_DOTENV=1` — opt-in. Allows loading `.env` files for local OAuth client IDs/secrets during development (for MCP connectors). `task dev` sets it for you; copy `.env.example` to `.env` and fill in the GitHub/Google client IDs. If you launch the app another way, export it yourself. Keep this off in packaged builds.
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

Project docs live in `docs/` and are indexed by [docs/README.md](README.md), organized into concepts, guides, and reference pages. When you change architecture, storage schema, events, tools, configuration, security behavior, or major features, update the relevant docs in the same change — the reference pages (`docs/reference/`) are the single source of truth for tools, events, configuration, and storage facts.

## Commits

- Write clear commit messages that describe the change.
- Do not add co-author trailers.
