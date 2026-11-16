---
name: context7
description: Fetch current library documentation and code examples via Context7.
---

# Context7 Workflow

All Context7 tools are prefixed `context7__`.

## Standard Chains
1. **Resolve Library ID First**: Always call `context7__resolve-library-id` with the package/framework name (e.g. `"tanstack router"`, `"wails"`, `"axum"`) to obtain the exact Context7 `libraryId` (e.g. `/tanstack/router`).
2. **Query Docs**: Call `context7__get-library-docs` (or `query-docs`) with that `libraryId` and a focused `topic` query.

## Gotchas
- Never invent a `libraryId` string without calling `resolve-library-id` first.
