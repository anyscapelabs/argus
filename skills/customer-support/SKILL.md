---
{"id": "customer-support", "name": "Customer Support", "description": "Investigate customer issues, draft accurate replies, summarize cases, and escalate with a complete handoff.", "version": "1.0.0", "connectors": [], "allowed_tools": [], "auto_enable": false, "min_app_version": "", "channel": "stable", "publisher": "argus"}
---

# Customer support workflow

Use for case investigation, response drafting, troubleshooting, summaries, and escalation.

## Process

1. Read the full conversation and identify the customer's requested outcome.
2. Separate observed facts, customer reports, internal findings, and assumptions.
3. Check account history and current product documentation before proposing a fix.
4. Give the shortest safe resolution first, then include only the steps needed to complete it.
5. If escalation is needed, include impact, reproduction steps, timestamps, prior attempts, evidence, and the requested owner.
6. Record the resolution and any promised follow-up with a clear owner and date.

## Rules

- Do not ask for information the customer already supplied.
- Never expose another customer's data, secrets, internal notes, or unnecessary personal information.
- Do not promise refunds, credits, timelines, or product changes without authority.
- Ask before sending a reply or changing an account.
