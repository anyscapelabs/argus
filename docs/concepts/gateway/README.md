# Gateway Architecture

The Argus backend is a Wails v3 desktop app. Three Wails services share a single `Shared.Core`:

- `Services/Provider` — provider connect/disconnect, API keys, provider views.
- `Services/Models` — model resolution, picker assembly, slots, selections, custom models.
- `Services/Catalog` — catalog status and manual refresh.

`Shared.Core` owns the provider overlay, SQLite storage, and the catalog store, and is the only integration point between services.

## Package layout

| Package | Responsibility |
|---|---|
| `Provider/` | Embedded `overlay.json`: 18 providers with adapter, base URL, models path, headers, auth flag, recommended model IDs, hide/live-exclude patterns. |
| `Models/` | Domain model (`Model`, `Capabilities`, `Limits`, `Cost`), ref helpers (`MakeRef`/`SplitRef`), text/deprecated helpers. |
| `Catalog/` | Builds a `Catalog` from models.dev JSON: filtering, normalization, recommendations, embedded snapshot, refresh store. |
| `Storage/` | SQLite database (`provider_configs`, `model_slots`, `custom_models`, `provider_selected_models`). |
| `Keyring/` | OS keyring access for API keys. |
| `Services/Shared/` | `Core`, DTOs/views, events, errors, live fetching. |

## Core bootstrap

`Shared.Core.Bootstrap(ctx)` runs in order:

1. `Provider.Load()` overlay.
2. `Storage.Open(dbPath)` + migrate.
3. `Catalog.NewStore(...)` — build from embedded snapshot, override with cache if newer.
4. `Catalog.StartBackground(...)` — 24h refresh with retry backoff.

`Ready()` guards every service call until boot completes.

## Events

- `catalog:updated`
- `providers:changed`
- `slots:changed`
- `selections:changed`

## Concurrency

- `Store` uses `atomic.Pointer` for current catalog/status; `catalogMu` serializes refresh.
- Per-provider mutexes (`lockCS`) and `liveMu` protect the live fetch cache.
- SQLite access via `database/sql`.

See [providers.md](./providers.md), [models.md](./models.md), [catalog.md](./catalog.md).