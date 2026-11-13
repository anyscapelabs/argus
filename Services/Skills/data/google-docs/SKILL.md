---
name: google-docs
description: Read and edit Google Docs documents.
---

# Google Docs Workflow

All Google Docs tools are prefixed `google_docs__`.

## Standard Chains
1. **Find Document ID**: If you only have a title, use `google_drive__search_files` (if connected) or ask/extract the document URL ID (`/document/d/<ID>/`).
2. **Read Before Editing**: Always call `google_docs__read_doc` (or `get_document`) first to inspect existing structure and indices.
3. **Create or Append/Update**: Use `google_docs__create_doc` for new docs or the unlocked update/append tool with the exact `documentId`.
