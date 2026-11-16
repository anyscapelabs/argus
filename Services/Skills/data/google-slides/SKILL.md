---
name: google-slides
description: Read and summarize Google Slides presentations.
---

# Google Slides Workflow

All Google Slides tools are prefixed `google_slides__`.

## Standard Chains
1. **Read Deck**: Extract `presentationId` from URL or Drive search, then call `google_slides__read_presentation` (or `get_presentation`).
2. **Summarize**: Group findings slide-by-slide with speaker notes if present. Note: creating local slide decks uses `create_slides`, not Google Slides MCP.
