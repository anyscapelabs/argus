# Browser automation

How the Argus browser works: what it can do, how the agent drives it, and the
safety gates around it. Implementation lives in `Services/Browser` (the
Chrome/CDP session) and `Services/Chat/browser.go` + `takeover.go` (the chat
tool path).

## The session

- Argus runs its own Chrome instance over CDP — not the user's browser.
- The profile lives under the Argus config directory (`browser/`, or under
  `ARGUS_BROWSER_PROFILE_DIR`), so logins, cookies, and cache persist across
  runs but stay isolated from the user's personal browser profile.
- The service tracks open tabs/pages, session state, and idle timers; the
  browser shuts down when idle. Lifecycle is app-managed (owned by the chat
  service, not exposed as a separate UI-bound service).

## What the agent can do

The `browser` tool drives the page through its **accessibility tree** (see
[tools reference](../reference/tools.md)): open, read/snapshot, click, fill,
press, scroll, find, manage tabs, run JS, and import a Chrome profile. Each call
returns the page as an accessibility snapshot so the agent works from structure,
not pixels.

Navigation, reading, snapshots, screenshots/captures, and actions are all
implemented in `Services/Browser` (`run.go`, `page.go`, `actions.go`,
`cdp_eval.go`).

## Human takeover

Some steps need a human: logins, CAPTCHAs, confirmations, or anything sensitive.
The takeover flow:

1. The agent (or a captcha detection) offers the user the browser window —
   `browser:takeover:requested`.
2. The user takes over, finishes, or declines; every later step is carried by
   `browser:takeover:status` so the card redraws from events.
3. Control returns to the agent when the user finishes.

Takeover state (including blocked states) is tracked in memory by the browser
service and surfaced to the UI.

## Safety gates

- Sensitive or irreversible actions (submit/send/purchase/post/delete) flow
  through the `browser_action` permission family, which supports host-scoped
  grants — see [security](../concepts/security.md).
- Interactive `browser_use` can be disabled as a kill switch via policy.
- The isolated profile prevents the agent from touching the user's personal
  browser session.

## Where to change what

- New page action → `Services/Browser/actions.go` + accessibility snapshot
  support if needed, then a dispatch line in the chat browser tool.
- New sensitive action → decide whether it needs the `browser_action`
  permission family and/or takeover.
- Browser settings UI → `frontend/src/pages/BrowserSettings.tsx`.
