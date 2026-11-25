# V0.1.1 - 2026-11-25

V0.1.1 is a polish and reliability release on top of the initial launch. It makes stopping a reply read as a deliberate stop, keeps thinking state alive while you move around the app or leave it in the tray, adds unread counters and recency ordering to the agent list, and fixes the Windows installer packaging so releases ship the real signed installer.

### Stopped Streams Are No Longer Shown as Errors

Stopping a streaming reply now shows a calm "Streaming stopped by you." notice instead of "An error has occurred. Try again later". Text that had already streamed stays visible in the reply instead of being replaced by the error notice, and the distinction is preserved in stored history so it still reads correctly after reload.

### Thinking State Survives Navigation and the Tray

The thinking indicator and active run state are now held in a shared Zustand store rather than in the chat page's local state. Moving from chat to settings and back, switching agents, or leaving Argus running in the system tray no longer drops the live run state or its event listeners. A per-agent run ID is exposed through the status API, so returning to a chat reattaches to an in-flight run and the Stop control keeps working. Events from other agents are tracked too, so their indicators settle correctly when their runs finish.

### Unread Message Counters

When an agent finishes a reply while you are in another agent's chat or in settings, its row in the agent list now shows an accent-colored unread counter below the timestamp. Counts survive restarts, clear as soon as you open that agent, and are ignored for runs you stopped yourself.

### Recent-First Agent List With Clearer Times

The agent list is now ordered by the time of the last message rather than by name or creation order, so the agent you spoke to most recently is at the top. Pinned agents still stay in front. Timestamps read naturally: the clock time for today, "Yesterday" for the previous day, the weekday within the last week, a month and day beyond that, and a full numeric date beyond a year.

### Wider Agent Replies

Agent reply bubbles can now use up to 85% of the conversation column, up from 70%, giving longer answers and wide tables more room to breathe.

### Windows Installer Packaging

Windows release builds now put NSIS on the PATH and call `makensis` directly against the already signed executable, instead of going through a task that rebuilds and replaces it with an unsigned binary. Packaging failures no longer pass silently: a missing installer or one too small to contain the app fails the release, and the WebView2 bootstrapper in the build directory is never collected as the installer.

# V0.1.0 - 2026-11-25

Argus V0.1.0 is the initial release of Argus, a desktop AI agent app for Mac, Windows, and Linux. This release brings the core Argus experience together in one app, with secure account pairing, persistent agent conversations, built-in tools, integrations, and desktop-native workflows.

### Account Pairing and Secure Access

Argus connects to your workspace through a service account key that you paste during sign-in. The key is verified against the Anyscape Gateway and stored in the operating system keyring, while entitlements are cached locally so Argus can continue working within a supported offline grace period. Signing out clears pairing state, and pairing a different account isolates account-scoped local data to prevent cross-account leakage.

### Persistent Chat with Streaming Agent Runs

Chat is the main workspace in Argus, where conversations with agents persist across sessions and reload with history. Assistant responses stream in real time, with visible run activity as the agent thinks, uses tools, and completes work. You can stop an active run, react to messages, and continue conversations with full context preserved in durable local chat history.

### Built-in Agent Tools

Argus agents can take action through built-in tools for everyday work, including reading and editing files, running terminal commands, searching the web, inspecting time and location, and working with conversation history. These tools let the agent move beyond text responses and complete practical tasks directly inside your desktop workflow, with tool results captured as part of the conversation.

### Permissions and Safety Approvals

Sensitive actions in Argus are protected by permission controls. You can set global defaults and per-agent overrides for areas such as terminal commands, file access, browser actions, and skill installation, choosing whether Argus should ask, allow, or block an action. When approval is needed during chat, Argus presents a clear permission prompt, and unattended background runs hold actions that require attention instead of silently proceeding.

### Integrations and MCP Connectors

Argus supports MCP integrations so agents can connect to external services and tools. From the Integrations experience, you can connect accounts, manage bindings per agent, choose access tiers, reconnect or disconnect services, and add custom MCP servers. When an agent needs an integration during a task, Argus can prompt you to connect it and make the newly available tools usable in chat.

### Browser Automation with Takeover

Argus includes an Argus-managed browser that agents can use to navigate pages, read content, click, fill forms, manage tabs, and gather information from the web. For sensitive steps such as logins, CAPTCHAs, or confirmations, Argus hands control back to you through a takeover flow so you can complete the action safely before the agent continues.

### Scheduled Routines and Reminders

You can schedule one-time reminders or recurring routines that run as durable background work. Argus stores scheduled tasks locally, catches up missed work when appropriate, and runs due tasks as unattended wakes that can notify you of outcomes. If a scheduled run needs permission, Argus holds the action and leaves an actionable notice so you can continue it interactively.

### Artifacts, Documents, and Slide Decks

Argus can create artifacts such as documents and slide decks as persistent deliverables from chat. Artifacts open in a dedicated viewer, keep their own versioned history, and can be exported in supported formats, including document exports and PowerPoint slide deck exports. This makes agent output easier to review, reuse, and share outside the conversation.

### File Uploads and Attachments

Chat supports file attachments so you can bring documents and images into the conversation. Argus validates uploads, stores them locally, extracts document text for agent reasoning, and shows attachment previews where appropriate. Attachments remain part of the conversation context, allowing agents to work with the files you provide.

### Memory, Personalization, and Skills

Argus can remember durable preferences and useful context, recall them when relevant, and forget them when needed. It also supports skills, which provide specialized instructions and workflows that agents can discover, read, and install with permission. Together, memory and skills help Argus adapt to how you work while keeping sensitive capability changes visible and controlled.

### Voice Interactions

Argus includes voice capabilities for live interactions inside the desktop app. You can start voice sessions from chat, see live voice state in the interface, and keep voice call records as part of the conversation history. This gives you a hands-free way to interact with Argus while preserving the context of the interaction.

### Desktop Experience and App Updates

The initial release delivers a native desktop experience with settings for appearance, notifications, browser behavior, permissions, usage, profile, billing, and updates. Argus can run in the background from the system tray, keep active work alive when the window is closed, and support launch-at-login background startup. The Updates screen lets you check for new versions and install available updates when they are ready.
