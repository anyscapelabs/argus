---
name: todoist
description: Manage tasks and projects in Todoist.
---

# Todoist Workflow

All Todoist tools are prefixed `todoist__`.

## Standard Chains
1. **Find Tasks / Projects**: Call `todoist__find-tasks` / `todoist__get-tasks` (or `find-projects`) before completing, updating, or moving items so you have the exact `task_id` or `project_id`.
2. **Create Task**: Call `todoist__add-tasks` (or `create_task`) with `content` (task title), optional `description`, `due_string` (e.g. `"tomorrow at 9am"`), and `priority` (1-4).
3. **Complete Task**: Call `todoist__complete-tasks` with the exact `id` returned by search.

## Gotchas
- In Todoist API, priority `4` is highest (p1 urgent) and `1` is normal (p4).
