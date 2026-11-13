---
name: github
description: Work with GitHub repositories, code, issues, and pull requests.
---

# GitHub Workflow

All GitHub tools are prefixed `github__`.

## Standard Chains
1. **Resolve Owner & Repo**: Every GitHub tool requires separate `owner` and `repo` strings (e.g. `owner: "anyscapelabs"`, `repo: "argus"`). If unknown, search repositories first.
2. **Issues & PRs**: Call `github__list_issues` or `github__list_pull_requests` to locate exact `issue_number` / `pull_number` before calling `get_*`, `create_comment`, or `merge_*`.
3. **Code Inspection**: Use `github__search_code` or `github__get_file_contents` with `path` and optional `ref` (branch/SHA).

## Gotchas
- Never pass `"owner/repo"` as a single field when the schema separates `owner` and `repo`.
