---
name: atlassian
description: Search Jira work and Confluence knowledge.
---

# Atlassian (Jira & Confluence) Workflow

All Atlassian tools are prefixed `atlassian__`.

## Standard Chains
1. **Resolve Cloud ID**: Check accessible resources (`getAccessibleAtlassianResources`) first if the tool schema requires `cloudId`.
2. **Jira Search**: Use JQL via `searchJiraIssuesUsingJql` (e.g. `assignee = currentUser() AND status != Done ORDER BY updated DESC`) or fetch a known key (`PROJ-42`).
3. **Confluence Search**: Use CQL or text search (`searchConfluenceUsingCql`) to find `pageId`, then fetch page body content.
