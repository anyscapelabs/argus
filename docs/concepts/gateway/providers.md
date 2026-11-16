# Providers

## Overlay

`Provider/overlay.json` describes 18 providers. Each `OverlayProvider` carries:

- `id`, `display_name`, `adapter` (`openai_compat`, `anthropic`, `gemini`)
- `base_url`, `models_path`, `extra_headers`
- `no_auth` (e.g. Ollama)
- `key_hint`, `key_help_url`, `doc_url`
- `order`, `logo`
- `recommended` (main/cheap model IDs)
- `hide`, `live_exclude_patterns`, `default_exclude_patterns`

`Provider.Load()` reads and validates the embedded overlay. `Resolved()` merges overlay entries with catalog provider-data flags (`InCatalogData`).

## Key storage

- `Keyring` stores secrets in the OS keyring under service `argus`, ref `provider/<id>/api_key`.
- `Storage.ProviderConfig` stores non-secret metadata: `secret_ref`, `key_last4`, `validated_at`, `base_url_override`.

## Connect flow (`ValidateAndSaveKey`)

1. Normalize key (trim quotes/Bearer, whitespace checks).
2. Store key in keyring.
3. `FetchLive` validates the key by listing models.
4. On failure: delete keyring secret, return error.
5. On success: save `ProviderConfig` (key_last4, validated_at, base_url_override), drop live cache, build picker via `PickerHook`, emit `providers:changed`.

## Disconnect flow (`RemoveKey`)

1. Delete keyring secret (unless `no_auth`).
2. Remove provider slots, clear provider selection, remove provider config.
3. Drop live cache.
4. Emit `providers:changed` (+ `slots:changed` if slots were cleared).

## Provider views

`Service.ProviderView` builds a `Shared.ProviderView`:

- identity, adapter metadata, no-auth flag
- `configured`, `key_last4`, `validated_at`
- `model_count` (total catalog models for provider)
- `selected_model_count` (from `provider_selected_models`)

`ListProviders` returns views for all overlay providers.

## Live fetching

`Shared.Core.FetchLive`:

- per-provider mutex + 15 min TTL cache
- reads key from keyring unless `no_auth`
- dispatches by adapter:
  - OpenAI-compat: envelope or bare array
  - Anthropic: paginated
  - Gemini: paginated, filtered to `generateContent`
- on failure returns stale cached results if present

## Events

- `providers:changed` on connect/disconnect.