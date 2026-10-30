# Catalog

## Build

`Catalog.Build` ingests models.dev JSON:

- filters non-text output types, deprecated models, and overlay `hide` lists
- normalizes status, capabilities, cost, limits
- groups models by provider
- runs recommendations (`EligibleMain`: text + tools; `EligibleCheap`: text)

`SortModels` sorts by release date (desc), name, id.

## Store (`Catalog.Store`)

- holds current `Catalog` and `Status` via `atomic.Pointer`
- `Build`: embedded snapshot → cache override if newer
- `Refresh`: network fetch with ETag, validation, atomic publish
- `StartBackground`: 24h refresh with retry backoff

## Refresh validation

Rejects refresh when:

- no providers with data
- zero models
- model count < half of previous

On success: write cache atomically, publish new catalog, emit `catalog:updated` (manual refresh).

## Status

`StatusView` reports source (`embedded`/`cache`/`network`), fetched_at, last_attempt, last_error, provider_count, model_count, stale, placeholder.

## Snapshot

Embedded `snapshot.json` + `meta.json` ship a baseline catalog for offline startup. Cache overrides when newer.

## Recommendations

`RecommendFrom(candidates, overlayRecommended)`:

1. Use overlay recommended IDs if present and eligible.
2. Fallback main = first tools-capable stable model.
3. Fallback cheap = blended-price heuristic within 24 months.

## Events

- `catalog:updated` on manual refresh completion.