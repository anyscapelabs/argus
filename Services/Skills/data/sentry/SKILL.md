---
name: sentry
description: Monitor errors and summarize application health in Sentry.
---

# Sentry Workflow

All Sentry tools are prefixed `sentry__`.

## Standard Chains
1. **Discover Organization & Project Slugs**: Call `sentry__list_organizations` (and `list_projects`) first to get exact `organizationSlug` and `projectSlug`.
2. **List Issues**: Call `sentry__list_issues` with `organizationSlug` and `projectSlug` (filter e.g. `is:unresolved`).
3. **Inspect Root Cause**: Call `sentry__get_issue` or `get_latest_event` with `issueId` to read the exception stacktrace and tags before summarizing.
