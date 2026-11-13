package Chat

import (
	"net/url"
	"strings"

	"github.com/anyscapelabs/argus/Services/Shared"
)

// LiveConfig returns a ready-to-open WebSocket URL for a live voice call. The
// desktop deliberately does not name a model: the router resolves the configured
// default (the JSON entry flagged `default`) and injects it into the setup frame
// itself, so the app never has to know a model id. The account key rides in the
// URL's query because a WebSocket client (the webview) cannot set custom headers
// and the browser cannot read the OS keyring.
func (s *Service) LiveConfig(agentID string) Shared.LiveConfigResult {
	if aerr := s.Core.Ready(); aerr != nil {
		return Shared.LiveConfigResult{Error: aerr}
	}
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		agentID = "default"
	}
	base := s.routerBaseURL()
	if base == "" {
		return Shared.LiveConfigResult{Error: Shared.NewError(Shared.CodeNotConfigured, "The router is not configured")}
	}
	key, err := getKey(Shared.RouterProviderID)
	if err != nil || strings.TrimSpace(key) == "" {
		return Shared.LiveConfigResult{Error: Shared.NewError(Shared.CodeNotConfigured, "Connect your account before a voice call")}
	}
	wsURL := liveWSURL(base, key)
	if wsURL == "" {
		return Shared.LiveConfigResult{Error: Shared.NewError(Shared.CodeBadInput, "The router URL is not valid")}
	}
	profile, err := s.ensureAgent(agentID)
	if err != nil {
		return Shared.LiveConfigResult{Error: Shared.NewError(Shared.CodeStorage, err.Error())}
	}
	sessionID := s.beginLiveToolSession(agentID, agentName(profile))
	return Shared.LiveConfigResult{Config: &Shared.LiveConfig{
		WsURL:             wsURL,
		SessionID:         sessionID,
		SystemInstruction: s.liveSystemPrompt(profile),
		Tools:             liveFunctionDeclarations(),
	}}
}

// routerBaseURL resolves the router provider's base URL, honouring any
// per-provider override stored in the database.
func (s *Service) routerBaseURL() string {
	if s.Core == nil || s.Core.Store == nil {
		return ""
	}
	provider, ok := s.Core.Store.Get().Provider(Shared.RouterProviderID)
	if !ok {
		return ""
	}
	base := provider.BaseURL
	if ov := s.baseURLOverride(Shared.RouterProviderID); ov != "" {
		base = ov
	}
	return strings.TrimRight(base, "/")
}

// liveWSURL turns the router's http(s) base URL into the /v1/live WebSocket
// endpoint carrying only the account key as a query param.
func liveWSURL(base, key string) string {
	u, err := url.Parse(base)
	if err != nil {
		return ""
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		u.Scheme = "wss"
	default:
		u.Scheme = "ws"
	}
	u.Path = strings.TrimSuffix(u.Path, "/") + "/live"
	q := u.Query()
	q.Set("key", key)
	u.RawQuery = q.Encode()
	return u.String()
}
