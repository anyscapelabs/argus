---
name: google-drive
description: Search, read, and organize files in Google Drive.
---

# Google Drive Workflow

All Google Drive tools are prefixed `google_drive__`.

## Standard Chains
1. **Locate File**: Call `google_drive__search_files` or `google_drive__list_recent_files` to get the exact `fileId` and `mimeType`.
2. **Read Content**: Use `google_drive__read_file` or `google_drive__get_file` with `fileId`. If `mimeType` is `application/vnd.google-apps.document` or `spreadsheet`, prefer the `google-docs` or `google-sheets` skill for structured edits.

## Gotchas
- Drive file names are not unique; confirm `modifiedTime` or parent folder when multiple matches appear.
