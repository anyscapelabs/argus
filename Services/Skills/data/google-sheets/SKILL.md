---
name: google-sheets
description: Read and update Google Sheets spreadsheets.
---

# Google Sheets Workflow

All Google Sheets tools are prefixed `google_sheets__`.

## Standard Chains
1. **Inspect Metadata First**: Call `google_sheets__get_spreadsheet` with `spreadsheetId` to discover exact sheet tab names before querying A1 ranges.
2. **Read Range**: Call `google_sheets__get_values` with A1 notation (e.g. `Sheet1!A1:F50`).
3. **Write / Append**: Use `google_sheets__append_values` or `google_sheets__update_values` with a 2D array of rows matching existing column headers.
