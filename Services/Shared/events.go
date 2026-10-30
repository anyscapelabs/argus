package Shared

const (
	EventCatalogUpdated    = "catalog:updated"
	EventProvidersChanged  = "providers:changed"
	EventSlotsChanged      = "slots:changed"
	EventSelectionsChanged = "selections:changed"
	EventModelStream       = "model:stream"
	EventStreamCancelled   = "model:stream_cancelled"
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
