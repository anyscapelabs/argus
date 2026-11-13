---
name: cloudflare
description: Search Cloudflare docs and inspect authorized Cloudflare resources.
---

# Cloudflare Workflow

All Cloudflare tools are prefixed `cloudflare__`.

## Standard Chains
1. **Set Active Account**: Call `cloudflare__accounts_list` and `cloudflare__set_active_account` if multiple accounts exist before querying Workers, KV, D1, R2, or Zones.
2. **Inspect Resources**: List zones/workers/databases to obtain exact IDs/names before reading logs, bindings, or DNS records.
3. **Docs Lookup**: Use `cloudflare__search_cloudflare_documentation` for Workers/Wrangler configuration questions.
