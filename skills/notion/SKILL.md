---
{"id": "notion", "name": "Notion", "description": "Search pages/databases, read content, create or update Notion pages and comments.", "version": "1.0.0", "connectors": ["notion"], "allowed_tools": [], "auto_enable": true, "min_app_version": "", "channel": "stable", "publisher": "argus"}
---

# Notion Workflow

All Notion tools are prefixed `notion__`.

## Standard Chains
1. **Search First**: Always call `notion__search` (or `notion__API-post-search`) with the page or database title to get its exact UUID (`page_id` or `database_id`).
2. **Read Page Content**: Retrieve both page properties and child blocks (`get_block_children` / `fetch`)—page metadata alone does not include body text.
3. **Create / Append**: Create a page under a resolved `parent` (`page_id` or `database_id`), or append blocks to an existing `block_id`.

## Gotchas
- Never guess a Notion page UUID; always resolve via search first.
