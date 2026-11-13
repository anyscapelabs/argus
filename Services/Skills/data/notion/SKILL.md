---
name: notion
description: Search, read, create, and update Notion pages, databases, and comments.
---

# Notion Workflow

All Notion tools are prefixed `notion__`.

## Standard Chains
1. **Search First**: Always call `notion__search` (or `notion__API-post-search`) with the page or database title to get its exact UUID (`page_id` or `database_id`).
2. **Read Page Content**: Retrieve both page properties and child blocks (`get_block_children` / `fetch`)—page metadata alone does not include body text.
3. **Create / Append**: Create a page under a resolved `parent` (`page_id` or `database_id`), or append blocks to an existing `block_id`.

## Gotchas
- Never guess a Notion page UUID; always resolve via search first.
