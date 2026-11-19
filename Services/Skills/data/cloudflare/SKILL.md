---
{"id": "cloudflare", "name": "Cloudflare", "description": "Search Cloudflare documentation and inspect accounts, zones, Workers, KV, D1, and R2 resources.", "version": "1.0.0", "connectors": ["cloudflare"], "allowed_tools": [], "auto_enable": true, "min_app_version": "", "channel": "stable", "publisher": "argus"}
---

# Cloudflare Workflow

All Cloudflare tools are prefixed `cloudflare__`.

## Standard Chains
1. **Set Active Account**: Call `cloudflare__accounts_list` and `cloudflare__set_active_account` if multiple accounts exist before querying Workers, KV, D1, R2, or Zones.
2. **Inspect Resources**: List zones/workers/databases to obtain exact IDs/names before reading logs, bindings, or DNS records.
3. **Docs Lookup**: Use `cloudflare__search_cloudflare_documentation` for Workers/Wrangler configuration questions.
