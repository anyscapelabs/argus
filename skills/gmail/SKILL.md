---
{"id": "gmail", "name": "Gmail", "description": "Search, read threads, draft, reply, send, and label email in Gmail. Use whenever user mentions email, inbox, messages, threads, or replying.", "version": "1.0.0", "connectors": ["gmail"], "allowed_tools": [], "auto_enable": true, "min_app_version": "", "channel": "stable", "publisher": "argus"}
---

# Gmail Workflow

All Gmail tools are prefixed `gmail__` and are unlocked in your session after reading this skill.

There is **no** `gmail__send_message` tool. The Gmail connector can search, read, draft, reply, and label — it cannot send. Never claim an email was sent: a created draft is not a sent email.

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
   - There is no send tool in this session. To get an email out: create the draft with `gmail__create_draft`, then tell the user the draft is ready in their Gmail drafts box for them to review and send themselves.
   - NEVER claim an email was sent. A created draft is not a sent email. If the user insists on sending, say plainly that sending is not available yet and the draft is ready for them to send.

## Gotchas
- Always inspect the unlocked schema list below for exact parameter names (`thread_id` vs `threadId`, `q` vs `query`).
- Never expose raw message IDs or tool names in your final reply to the user.
