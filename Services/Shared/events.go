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
	EventPermissionRequested = "permission:requested"
	EventPermissionResolved  = "permission:resolved"
	EventMCPConnectRequested = "mcp:connect:requested"
	EventMCPConnectResolved  = "mcp:connect:resolved"
	EventActivity            = "agent:activity"
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
