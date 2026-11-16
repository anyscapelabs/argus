---
name: gmail
description: Search, read, draft, reply, send, and label email in Gmail.
---

# Gmail Workflow

All Gmail tools are prefixed `gmail__` and are unlocked in your session after reading this skill.

## Standard Chains

1. **Find & Read an Email / Thread**
   - Call `invoke_tool` with `gmail__search_messages` (or `gmail__list_messages`) using Gmail search syntax (e.g. `from:bob invoice`, `is:unread`, `newer_than:7d`).
   - Extract `threadId` or `messageId` from the search result.
   - Call `gmail__get_thread` (preferred for full context) or `gmail__get_message` with that ID before summarizing or replying.

2. **Reply to an Existing Thread**
   - NEVER compose a brand-new email when the user asks to reply.
   - First run Chain 1 to get `threadId`, original `Subject`, and sender `From` address.
   - Call `gmail__reply_message` or `gmail__create_draft` with `threadId` so the reply stays threaded.

3. **Send or Draft a New Email**
   - If the user says "draft" or does not explicitly confirm sending to an external recipient, prefer `gmail__create_draft`.
   - For explicit send requests, verify recipient email address (`to`), `subject`, and `body` before calling `gmail__send_message`.

## Gotchas
- Always inspect the unlocked schema list below for exact parameter names (`thread_id` vs `threadId`, `q` vs `query`).
- Never expose raw message IDs or tool names in your final reply to the user.
