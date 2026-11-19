# Configuration reference

All runtime configuration Argus reads from the environment, plus the on-disk
locations it uses. This is the single source of truth for configuration facts;
concept docs link here instead of restating them.

## Environment variables

| Variable | Where it applies | What it does | Default |
|---|---|---|---|
| `VITE_ANYSCAPE_GATEWAY_URL` | `frontend/.env`, build time | Gateway origin the desktop app is built to pair with. | dev default varies per checkout |
| `VITE_ARGUS_API_URL` | `frontend/.env`, build time | Deprecated alias of `VITE_ANYSCAPE_GATEWAY_URL`. | — |
| `ARGUS_LOAD_DOTENV` | backend, development only | When set to `1`, MCP credential loading may read `.env`-style files to pick up local OAuth client IDs/secrets (`Services/MCP/service.go`). Off by default in packaged builds. | unset |
| `ARGUS_BROWSER_PROFILE_DIR` | backend | Overrides the base directory for the Argus browser automation profile (the app uses a `browser` subdirectory under it). | unset |
| `ARGUS_UPDATE_REPO` | backend, desktop builds | `owner/repo` the self-updater checks for releases. Useful for staging or a fork. | `anyscapelabs/argus` |

Note the split between build-time and runtime: `VITE_*` values are compiled into
the frontend bundle, but model traffic at runtime follows the gateway the key
paired against (persisted as a provider base URL override), not the build-time
variable.

## Command-line flags

| Flag | What it does |
|---|---|
| `--background` | Start with the window hidden in the system tray. The login item passes it; it is not normally typed by hand. |

## On-disk locations

Base config directory: `os.UserConfigDir()/argus` (platform-specific).

| Path | Purpose |
|---|---|
| `argus.db` | SQLite database (WAL, foreign keys on, single writer connection) |
| `uploads/<attachment-id><ext>` | uploaded file bytes, restrictive permissions |
| `decks/<id>.pptx` | generated slide deck exports |
| `browser/` | Argus-owned Chrome profile for automation (or under `ARGUS_BROWSER_PROFILE_DIR`) |

Base cache directory: `os.UserCacheDir()/argus` — catalog store and background
refresh data.

Development-only credential inputs: with `ARGUS_LOAD_DOTENV=1`, `.env` files;
MCP credential resolution may also read `config/<name>.json`,
`.config/argus/<name>.json`, or `$HOME/.config/argus/<name>.json` depending on
the connector. The repo's `config/google.json` exists for development/config
scenarios.

Secrets (account key, MCP tokens/sessions) live in the OS keyring, never in
these files. See the [storage reference](storage.md) for keyring refs.

## Toolchain

- **Go** — see `go.mod` for module/toolchain version (README notes Go 1.25+).
- **Node.js/npm** — frontend tooling (README notes Node 24+).
- **Wails v3 CLI** — <https://v3.wails.io/>; build/dev configuration lives in
  `build/config.yml` and the platform Taskfiles under `build/`.

## Where to change what

- Adding or changing a persisted field → `Storage/migrations.go` + storage
  helpers, and check account purge coverage (see [storage](storage.md)).
- Adding a runtime event → `Services/Shared/events.go`, then update
  [events reference](events.md) and regenerate bindings if exposed to the UI.
- Adding an environment variable → document it in this table in the same change.
