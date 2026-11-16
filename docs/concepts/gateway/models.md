# Models

## Domain model

`Models.Model` carries ref, provider ID, ID, name, family, released/updated, knowledge, capabilities, caps-known flag, limits, cost, status, source (`catalog`/`live`/`custom`), open-weights, modalities.

Refs are `provider/model` (`MakeRef`/`SplitRef`).

## Picker (`Models.Picker`)

For a configured provider:

1. Fetch live models (if configured).
2. Merge:
   - catalog models present live → `available`
   - live-only models (excluding patterns) → `other`
   - custom models
3. `RecommendFrom` picks main (first tools-capable stable) and cheap (blended-price heuristic).
4. Apply search / tools-only / beta filters.
5. Sections: `recommended`, `available`, `custom`, `other`.

For unconfigured providers, catalog models are shown as `unverified`.

## Model resolution (`resolveRef`)

Order: catalog → custom → live (configured only, not excluded).

## Availability labels

`available`, `unverified`, `live_only`, `custom`.

## Selections

`SetProviderSelection` validates each ref resolves and belongs to the provider, then stores ordered refs in `provider_selected_models`. Emits `selections:changed`.

`ChatModels` resolves selected refs across configured providers into `ChatModel` list for chat use.

## Slots

Slots: `main`, `cheap`, `fallback`. `SetSlot` validates provider configured and eligibility (main needs tools; cheap needs text). `GetSlots` resolves each slot ref to view + status.

## Custom models

Stored in `custom_models` with caps, price, context, modalities. Availability `custom`; eligible for main iff tools.

## Live-only exclusions

Glob patterns (`path.Match`) against model id/name, combining overlay defaults + per-provider patterns (`live_exclude_patterns`).

## Events

- `selections:changed` on selection save.
- `slots:changed` on slot change.