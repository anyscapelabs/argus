---
{"id": "context7", "name": "Context7", "description": "Resolve library IDs and fetch up-to-date API documentation and code examples.", "version": "1.0.0", "connectors": ["context7"], "allowed_tools": [], "auto_enable": true, "min_app_version": "", "channel": "stable", "publisher": "argus"}
---

# Context7 Workflow

All Context7 tools are prefixed `context7__`.

## Standard Chains
1. **Resolve Library ID First**: Always call `context7__resolve-library-id` with the package/framework name (e.g. `"tanstack router"`, `"wails"`, `"axum"`) to obtain the exact Context7 `libraryId` (e.g. `/tanstack/router`).
2. **Query Docs**: Call `context7__get-library-docs` (or `query-docs`) with that `libraryId` and a focused `topic` query.

## Gotchas
- Never invent a `libraryId` string without calling `resolve-library-id` first.
