package Shared

const (
	EventCatalogUpdated      = "catalog:updated"
	EventProvidersChanged    = "providers:changed"
	EventSlotsChanged        = "slots:changed"
	EventSelectionsChanged   = "selections:changed"
	EventModelStream         = "model:stream"
	EventStreamCancelled     = "model:stream_cancelled"
	EventChatMessage         = "chat:message"
	EventChatTyping          = "chat:typing"
	EventChatRunFinished     = "chat:run_finished"
	EventChatRunStarted      = "chat:run_started"
	EventChatPhase           = "chat:phase"
	EventPermissionRequested = "permission:requested"
	EventPermissionResolved  = "permission:resolved"
	EventMCPConnectRequested = "mcp:connect:requested"
	EventMCPConnectResolved  = "mcp:connect:resolved"
	EventMCPConnectStatus    = "mcp:connect:status"
	EventActivity            = "agent:activity"
	// EventScheduleUpdated fires whenever scheduled_tasks changes — the agent
	// created or cancelled one, or the engine advanced, retired or paused it —
	// so the routines panel redraws from the database instead of polling.
	EventScheduleUpdated = "schedule:updated"
	// EventTakeoverRequested fires when the agent (or a captcha detection)
	// offers the user the browser window. EventTakeoverStatus carries every
	// later step of that card's life — the user taking over, finishing, or
	// declining — so the card redraws from events like the permission one.
	EventTakeoverRequested = "browser:takeover:requested"
	EventTakeoverStatus    = "browser:takeover:status"
	// EventArtifactUpdated fires whenever artifacts changes: the agent wrote or
	// extended a document, or one was deleted. The Artifacts panel refetches on
	// it, so a report appears the moment it is stored rather than on remount.
	EventArtifactUpdated = "artifact:updated"
)

const (
	AvailabilityAvailable  = "available"
	AvailabilityUnverified = "unverified"
	AvailabilityLiveOnly   = "live_only"
	AvailabilityCustom     = "custom"
)

const (
	SlotMain     = "main"
	SlotCheap    = "cheap"
	SlotFallback = "fallback"
)

var SlotNames = []string{SlotMain, SlotCheap, SlotFallback}

const (
	StatusOK                    = "ok"
	StatusProviderNotConfigured = "provider_not_configured"
	StatusModelNotInCatalog     = "model_not_in_catalog"
	StatusUnset                 = "unset"
)
