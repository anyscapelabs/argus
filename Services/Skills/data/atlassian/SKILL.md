---
{"id": "atlassian", "name": "Atlassian Jira & Confluence", "description": "Search Jira issues with JQL, inspect tickets, and search/read Confluence wiki pages.", "version": "1.0.0", "connectors": ["atlassian"], "allowed_tools": [], "auto_enable": true, "min_app_version": "", "channel": "stable", "publisher": "argus"}
---

# Atlassian (Jira & Confluence) Workflow

All Atlassian tools are prefixed `atlassian__`.

## Standard Chains
1. **Resolve Cloud ID**: Check accessible resources (`getAccessibleAtlassianResources`) first if the tool schema requires `cloudId`.
2. **Jira Search**: Use JQL via `searchJiraIssuesUsingJql` (e.g. `assignee = currentUser() AND status != Done ORDER BY updated DESC`) or fetch a known key (`PROJ-42`).
3. **Confluence Search**: Use CQL or text search (`searchConfluenceUsingCql`) to find `pageId`, then fetch page body content.
