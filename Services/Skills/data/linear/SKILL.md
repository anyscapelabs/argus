---
name: linear
description: Find, create, and update Linear issues and projects.
---

# Linear Workflow

All Linear tools are prefixed `linear__`.

## Standard Chains
1. **Discover Team / State IDs First**: Before creating or moving an issue, list teams or search existing issues to obtain valid `teamId`, `stateId`, and `assigneeId` UUIDs.
2. **Search Issues**: Call `linear__list_issues` or `linear__search_issues` with query text or identifier (e.g. `ENG-123`).
3. **Create / Update**: Pass the resolved `teamId` and `title` to `linear__create_issue`, or `id` to `linear__update_issue`.

## Gotchas
- Linear mutations require UUIDs for `teamId` and workflow `stateId`, not human display names.
