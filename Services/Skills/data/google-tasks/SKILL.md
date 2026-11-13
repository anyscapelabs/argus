---
name: google-tasks
description: List, create, and complete Google Tasks.
---

# Google Tasks Workflow

All Google Tasks tools are prefixed `google_tasks__`.

## Standard Chains
1. **List Tasks**: Call `google_tasks__list_tasks` (use default task list `@default` unless a specific `tasklistId` is requested).
2. **Create Task**: Call `google_tasks__create_task` with `title`, optional `notes`, and RFC3339 `due` timestamp.
3. **Complete Task**: First call `google_tasks__list_tasks` to find the exact `taskId`, then update its `status` to `"completed"`.
