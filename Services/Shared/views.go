package Shared

import (
	"github.com/anyscapelabs/argus/Models"
	"github.com/anyscapelabs/argus/Protocol"
)

type PickerOptions struct {
	Search      string `json:"search"`
	IncludeBeta bool   `json:"include_beta"`
	ToolsOnly   bool   `json:"tools_only"`
	RefreshLive bool   `json:"refresh_live"`
}

type ProviderView struct {
	ID                 string `json:"id"`
	Name               string `json:"name"`
	Logo               string `json:"logo"`
	Monogram           string `json:"monogram"`
	NoAuth             bool   `json:"no_auth"`
	KeyHint            string `json:"key_hint"`
	KeyHelpURL         string `json:"key_help_url"`
	DocURL             string `json:"doc_url"`
	Configured         bool   `json:"configured"`
	KeyLast4           string `json:"key_last4,omitempty"`
	ValidatedAt        string `json:"validated_at,omitempty"`
	ModelCount         int    `json:"model_count"`
	SelectedModelCount int    `json:"selected_model_count"`
	Order              int    `json:"order"`
}

type ModelView struct {
	Ref              string              `json:"ref"`
	ProviderID       string              `json:"provider_id"`
	ProviderName     string              `json:"provider_name,omitempty"`
	ID               string              `json:"id"`
	Name             string              `json:"name"`
	Family           string              `json:"family,omitempty"`
	Availability     string              `json:"availability"`
	Released         string              `json:"released,omitempty"`
	ReleasedLabel    string              `json:"released_label,omitempty"`
	ContextWindow    int64               `json:"context_window"`
	MaxOutput        int64               `json:"max_output"`
	ContextLabel     string              `json:"context_label"`
	MaxOutputLabel   string              `json:"max_output_label"`
	Price            *Models.Cost        `json:"price"`
	PriceLabel       string              `json:"price_label"`
	Capabilities     []string            `json:"capabilities"`
	Caps             Models.Capabilities `json:"caps"`
	CapsKnown        bool                `json:"caps_known"`
	Status           string              `json:"status"`
	OpenWeights      bool                `json:"open_weights"`
	Recommended      string              `json:"recommended,omitempty"`
	EligibleMain     bool                `json:"eligible_main"`
	EligibleCheap    bool                `json:"eligible_cheap"`
	IneligibleReason string              `json:"ineligible_reason,omitempty"`
	SelectedFor      []string            `json:"selected_for,omitempty"`
}

type Section struct {
	ID       string      `json:"id"`
	Title    string      `json:"title"`
	Subtitle string      `json:"subtitle,omitempty"`
	Models   []ModelView `json:"models"`
}

type Recommendations struct {
	Main  string `json:"main,omitempty"`
	Cheap string `json:"cheap,omitempty"`
}

type Picker struct {
	Provider      ProviderView    `json:"provider"`
	LiveChecked   bool            `json:"live_checked"`
	LiveError     *ActionError    `json:"live_error,omitempty"`
	Recommended   Recommendations `json:"recommended"`
	Sections      []Section       `json:"sections"`
	TotalCount    int             `json:"total_count"`
	CatalogSource string          `json:"catalog_source"`
	Stale         bool            `json:"stale,omitempty"`
}

type PickerResult struct {
	Ok     bool         `json:"ok"`
	Error  *ActionError `json:"error,omitempty"`
	Picker *Picker      `json:"picker,omitempty"`
}

type SaveKeyResult struct {
	Ok     bool         `json:"ok"`
	Error  *ActionError `json:"error,omitempty"`
	Picker *Picker      `json:"picker,omitempty"`
}

type ActionResult struct {
	Ok           bool         `json:"ok"`
	Error        *ActionError `json:"error,omitempty"`
	ClearedSlots []string     `json:"cleared_slots,omitempty"`
}

type GetModelResult struct {
	View  *ModelView   `json:"view,omitempty"`
	Error *ActionError `json:"error,omitempty"`
}

type SlotView struct {
	Slot   string     `json:"slot"`
	Ref    string     `json:"ref"`
	Model  *ModelView `json:"model,omitempty"`
	Status string     `json:"status"`
	Reason string     `json:"reason,omitempty"`
}

type ChatModel struct {
	Ref          string    `json:"ref"`
	ProviderID   string    `json:"provider_id"`
	ProviderName string    `json:"provider_name"`
	Model        ModelView `json:"model"`
}

type CustomModelInput struct {
	ProviderID    string   `json:"provider_id"`
	ModelID       string   `json:"model_id"`
	DisplayName   string   `json:"display_name"`
	ContextWindow int64    `json:"context_window,omitempty"`
	MaxOutput     int64    `json:"max_output,omitempty"`
	Tools         bool     `json:"tools"`
	Vision        bool     `json:"vision"`
	Reasoning     bool     `json:"reasoning"`
	PriceKnown    bool     `json:"price_known"`
	PriceInput    *float64 `json:"price_input,omitempty"`
	PriceOutput   *float64 `json:"price_output,omitempty"`
}

type StatusView struct {
	Source        string `json:"source"`
	FetchedAt     string `json:"fetched_at,omitempty"`
	LastAttempt   string `json:"last_attempt,omitempty"`
	LastError     string `json:"last_error,omitempty"`
	ProviderCount int    `json:"provider_count"`
	ModelCount    int    `json:"model_count"`
	Stale         bool   `json:"stale"`
	Placeholder   bool   `json:"placeholder"`
}

type RefreshResult struct {
	Ok     bool         `json:"ok"`
	Error  *ActionError `json:"error,omitempty"`
	Status *StatusView  `json:"status,omitempty"`
}

type SelectionsResult struct {
	Refs  []string     `json:"refs"`
	Error *ActionError `json:"error,omitempty"`
}

type AgentProfile struct {
	ID           string `json:"id"`
	Avatar       string `json:"avatar"`
	AgentName    string `json:"agent_name"`
	ModelRef     string `json:"model_ref"`
	SystemPrompt string `json:"system_prompt"`
	UpdatedAt    string `json:"updated_at"`
}

type AgentProfileResult struct {
	Profile *AgentProfile `json:"profile,omitempty"`
	Error   *ActionError  `json:"error,omitempty"`
}

type StreamRequest struct {
	Ref             string             `json:"ref"`
	System          string             `json:"system,omitempty"`
	Messages        []Protocol.Message `json:"messages"`
	MaxOutputTokens int64              `json:"max_output_tokens,omitempty"`
	Temperature     *float64           `json:"temperature,omitempty"`
	StopSequences   []string           `json:"stop_sequences,omitempty"`
	Reasoning       string             `json:"reasoning,omitempty"`
}

type StreamStart struct {
	Ok    bool         `json:"ok"`
	RunID string       `json:"run_id,omitempty"`
	Error *ActionError `json:"error,omitempty"`
}

type StreamEvent struct {
	RunID     string         `json:"run_id"`
	AgentID   string         `json:"agent_id,omitempty"`
	MessageID string         `json:"message_id,omitempty"`
	Event     Protocol.Event `json:"event"`
}

type SendResult struct {
	Ok                 bool         `json:"ok"`
	Error              *ActionError `json:"error,omitempty"`
	RunID              string       `json:"run_id,omitempty"`
	UserMessageID      string       `json:"user_message_id,omitempty"`
	AssistantMessageID string       `json:"assistant_message_id,omitempty"`
}

type ChatMessage struct {
	ID           string           `json:"id"`
	AgentID      string           `json:"agent_id"`
	Seq          int              `json:"seq"`
	Role         string           `json:"role"`
	Text         string           `json:"text"`
	Status       string           `json:"status"`
	ErrorMessage string           `json:"error_message,omitempty"`
	StopReason   string           `json:"stop_reason,omitempty"`
	ModelRef     string           `json:"model_ref,omitempty"`
	CreatedAt    int64            `json:"created_at"`
	CompletedAt  int64            `json:"completed_at,omitempty"`
	Blocks       []Protocol.Block `json:"blocks,omitempty"`
}

type GetChatResult struct {
	Messages []ChatMessage `json:"messages"`
	// NextBeforeSeq is the cursor the next older page is fetched with: seq
	// strictly below it. Zero means there is no older page — the conversation
	// has been read back to its first message.
	NextBeforeSeq int         `json:"next_before_seq,omitempty"`
	Error         *ActionError `json:"error,omitempty"`
}

type UsageResult struct {
	InputTokens  int64        `json:"input_tokens"`
	OutputTokens int64        `json:"output_tokens"`
	CostUSD      float64      `json:"cost_usd"`
	Requests     int          `json:"requests"`
	Failed       int          `json:"failed"`
	Error        *ActionError `json:"error,omitempty"`
}

type ListRequestsResult struct {
	Requests []ModelRequestView `json:"requests"`
	Error    *ActionError       `json:"error,omitempty"`
}

type ModelRequestView struct {
	ID           string  `json:"id"`
	Status       string  `json:"status"`
	ModelRef     string  `json:"model_ref"`
	InputTokens  int64   `json:"input_tokens"`
	OutputTokens int64   `json:"output_tokens"`
	CostUSD      float64 `json:"cost_usd"`
	StartedAt    int64   `json:"started_at"`
	CompletedAt  int64   `json:"completed_at"`
}

type PermissionRequestView struct {
	RequestID string `json:"request_id"`
	AgentID   string `json:"agent_id"`
	AgentName string `json:"agent_name"`
	Tool      string `json:"tool"`
	Command   string `json:"command"`
}

// ActivityView tells the frontend what the agent is live-doing
// (for example running a terminal command) while no text streams.
type ActivityView struct {
	AgentID string `json:"agent_id"`
	RunID   string `json:"run_id,omitempty"`
	Kind    string `json:"kind"` // tool_started | tool_results | tool_finished
	Tool    string `json:"tool"`
	Command string `json:"command,omitempty"`
	// Sources carries the sites a web_search call found, so the live
	// indicator can show favicon plus result titles instead of the query.
	Sources []ActivitySource `json:"sources,omitempty"`
	// Page carries where a browser action left the agent, so the live indicator
	// can name the site instead of the verb.
	Page *ActivityPage `json:"page,omitempty"`
}

// ActivityPage is the state of the agent's browser at one moment.
type ActivityPage struct {
	Host   string `json:"host"`
	Title  string `json:"title"`
	Action string `json:"action"`
	Tab    int    `json:"tab"`
}

// ActivitySource is one site the agent's search surfaced.
type ActivitySource struct {
	Host  string `json:"host"`
	Title string `json:"title"`
}

type PermissionDecisionView struct {
	RequestID string `json:"request_id"`
	AgentID   string `json:"agent_id"`
	AgentName string `json:"agent_name,omitempty"`
	Tool      string `json:"tool"`
	Command   string `json:"command"`
	Decision  string `json:"decision"`
}

type PermissionNoticeView struct {
	ID        string `json:"id"`
	AgentID   string `json:"agent_id"`
	AgentName string `json:"agent_name"`
	Tool      string `json:"tool"`
	Command   string `json:"command"`
	Decision  string `json:"decision"`
	// Kind separates a permission decision from any other notice, and Text
	// carries the copy for the kinds the UI cannot compose itself.
	Kind      string `json:"kind"`
	Text      string `json:"text"`
	CreatedAt int64  `json:"created_at"`
}

type MCPConnectRequestView struct {
	RequestID   string `json:"request_id"`
	AgentID     string `json:"agent_id"`
	AgentName   string `json:"agent_name"`
	ServerID    string `json:"server_id"`
	ServerName  string `json:"server_name"`
	Description string `json:"description"`
	Icon        string `json:"icon"`
	// State carries the card's last known state so a restored card shows what
	// it did before the remount: without it a connected integration would come
	// back offering Connect again. Empty means still waiting on the user.
	State string `json:"state"`
	// CreatedAt is the instant the card entered the conversation, in unix
	// milliseconds. The transcript weaves the card at that moment, the same way
	// it weaves notices, so it stays where the sign-in was asked for instead of
	// following every newer message to the bottom.
	CreatedAt int64 `json:"created_at"`
}

type MCPConnectDecisionView struct {
	RequestID string `json:"request_id"`
	AgentID   string `json:"agent_id"`
	Decision  string `json:"decision"`
	ServerID  string `json:"server_id"`
}

// TakeoverRequestView is the browser takeover card: what page the agent is on
// (its screenshot included) and where the handover stands. States ride the
// card itself so a restored card shows what happened before a remount:
// requested → active → done, or declined at any step.
type TakeoverRequestView struct {
	RequestID string `json:"request_id"`
	AgentID   string `json:"agent_id"`
	AgentName string `json:"agent_name,omitempty"`
	// Reason is why the agent asked: a CAPTCHA it cannot solve, a step that
	// needs something only the user can type (credentials, bank cards).
	Reason string `json:"reason,omitempty"`
	// Screenshot is a PNG data URL of the agent's viewport. Kept here rather
	// than on disk-path because the webview cannot read the OS cache dir.
	Screenshot string `json:"screenshot,omitempty"`
	Host       string `json:"host,omitempty"`
	PageTitle  string `json:"page_title,omitempty"`
	State      string `json:"state"`
	CreatedAt  int64  `json:"created_at"`
}

// TakeoverListResult is what the frontend reads on mount to restore cards.
type TakeoverListResult struct {
	Takeovers []TakeoverRequestView `json:"takeovers"`
	Error     *ActionError          `json:"error,omitempty"`
}

type NoticesResult struct {
	Notices []PermissionNoticeView `json:"notices"`
	Error   *ActionError           `json:"error,omitempty"`
}

type PermissionState struct {
	AgentID string `json:"agent_id"`
	Tool    string `json:"tool"`
	Allowed bool   `json:"allowed"`
	Denied  bool   `json:"denied"`
}

type PermissionStateResult struct {
	State PermissionState `json:"state"`
	Error *ActionError    `json:"error,omitempty"`
}

// ScheduledTaskView is one row of the routines panel. Times are unix
// milliseconds so the panel can render them in the user's own calendar
// ("Today at 8:00am") instead of a string the backend chose for them.
type ScheduledTaskView struct {
	ID         string `json:"id"`
	AgentID    string `json:"agent_id"`
	Kind       string `json:"kind"` // routine | reminder
	Title      string `json:"title"`
	Prompt     string `json:"prompt"`
	Summary    string `json:"summary"`
	NextRunAt  int64  `json:"next_run_at"`
	LastRunAt  int64  `json:"last_run_at"`
	LastStatus string `json:"last_status,omitempty"`
	Paused     bool   `json:"paused"`
	// Retired means a one-off reminder already ran: it has no next fire and is
	// history rather than a live schedule.
	Retired   bool  `json:"retired"`
	CreatedAt int64 `json:"created_at"`
}

type ScheduledTasksResult struct {
	Tasks []ScheduledTaskView `json:"tasks"`
	Error *ActionError        `json:"error,omitempty"`
}

// AgentStatusView is the small dashboard the right panel's Details tab renders.
// StartedAtMs is the app process start (uptime reads off it), LastActiveMs is
// the moment the agent last finished a turn (0 = never), Busy says a run is
// on right now so the row can read Working instead of Online.
type AgentStatusView struct {
	StartedAtMs  int64 `json:"started_at_ms"`
	LastActiveMs int64 `json:"last_active_ms"`
	Busy         bool  `json:"busy"`
}

type AgentStatusResult struct {
	Status AgentStatusView `json:"status"`
	Error  *ActionError    `json:"error,omitempty"`
}

// ArtifactView is one document the agent wrote. Body is the canonical Markdown
// and arrives only from Get: a panel listing thirty reports should not carry
// thirty documents over the bridge. Words and Bytes are stored, not recomputed,
// so the number the panel shows is the number the writer was told.
type ArtifactView struct {
	ID      string `json:"id"`
	AgentID string `json:"agent_id"`
	Title   string `json:"title"`
	// Format is the authoring format, and there is only one: "markdown". It
	// stays in the row because the export targets are formats a document can
	// become, never formats it is written in.
	Format string `json:"format"`
	Body   string `json:"body,omitempty"`
	// Meta is the document's front matter — title, author, date, language,
	// paper, orientation — the fields the exporters honour.
	Meta      map[string]string `json:"meta,omitempty"`
	Words     int               `json:"words"`
	Bytes     int               `json:"bytes"`
	Version   int               `json:"version"`
	CreatedAt int64             `json:"created_at"`
	UpdatedAt int64             `json:"updated_at"`
}

type ArtifactsResult struct {
	Artifacts []ArtifactView `json:"artifacts"`
	Error     *ActionError   `json:"error,omitempty"`
}

type ArtifactResult struct {
	Artifact ArtifactView `json:"artifact"`
	Found    bool         `json:"found"`
	Error    *ActionError `json:"error,omitempty"`
}

// ArtifactWriteResult is the reply to a writer. A document that broke the rules
// is refused rather than stored, and Rejected plus Findings is how the agent
// hears that: Error stays nil, because the call worked and the answer was no.
type ArtifactWriteResult struct {
	Artifact ArtifactView `json:"artifact"`
	Rejected bool         `json:"rejected"`
	Findings string       `json:"findings"`
	Error    *ActionError `json:"error,omitempty"`
}

// ExportFormatView is one entry of the export menu. A format the machine cannot
// produce is still listed, disabled, with Required explaining what to install —
// hiding an option the user can fix by installing something is worse than
// showing it greyed out.
type ExportFormatView struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	Extension string `json:"extension"`
	Available bool   `json:"available"`
	Hint      string `json:"hint"`
	Required  string `json:"required,omitempty"`
}

type ExportFormatsResult struct {
	Formats []ExportFormatView `json:"formats"`
	Error   *ActionError       `json:"error,omitempty"`
}

// ArtifactExportResult reports where the derived file landed. Path is absolute,
// because the panel offers to reveal it.
type ArtifactExportResult struct {
	Path   string       `json:"path"`
	Format string       `json:"format"`
	Bytes  int          `json:"bytes"`
	Error  *ActionError `json:"error,omitempty"`
}

// SaveAccountKeyResult acknowledges a keyring write of the agent account key.
type SaveAccountKeyResult struct {
	Ok    bool         `json:"ok"`
	Error *ActionError `json:"error,omitempty"`
}

// KeyStatus is the presence probe the shell's router uses, without ever
// touching the secret's value.
type KeyStatusResult struct {
	Status *KeyStatus    `json:"status,omitempty"`
	Error  *ActionError  `json:"error,omitempty"`
}

type KeyStatus struct {
	HasKey bool `json:"hasKey"`
}

// EntitlementUser mirrors the API identity block.
type EntitlementUser struct {
	ID    string  `json:"id"`
	Name  *string `json:"name"`
	Email *string `json:"email"`
	Image *string `json:"image"`
}

// EntitlementPlan mirrors the API plan block.
type EntitlementPlan struct {
	ID string `json:"id"`
}

// Entitlements is the identity + plan + allowlist trio the agent runs on.
// The allowlist carries the router's catalog id plus display name per entry
// so the picker shows the display name without any frontend trimming.
type Entitlements struct {
	User      EntitlementUser `json:"user"`
	Plan      EntitlementPlan `json:"plan"`
	Allowlist []AllowedModel  `json:"allowlist"`
	Offline   bool            `json:"offline,omitempty"`
}

type AllowedModel struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name,omitempty"`
	Name        string `json:"name,omitempty"`
}

func (m AllowedModel) Display() string {
	if m.DisplayName != "" {
		return m.DisplayName
	}
	if m.Name != "" {
		return m.Name
	}
	return m.ID
}

// EntitlementsView carries the entitlements snapshot or a typed error.
type EntitlementsView struct {
	Account *Entitlements `json:"account,omitempty"`
	Error   *ActionError  `json:"error,omitempty"`
}
