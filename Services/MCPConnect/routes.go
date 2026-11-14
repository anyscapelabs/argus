package MCPConnect

import (
	"encoding/json"
	"net/http"
	"sync"

	"github.com/anyscapelabs/argus/Services/MCP"
	"github.com/anyscapelabs/argus/Storage"
	"github.com/google/uuid"
)

// pendingOAuth carries what a begun sign-in needs when its callback arrives,
// keyed by the state parameter that ties the two legs together.
type pendingOAuth struct {
	ServerID      string
	Label         string
	ExpectedEmail string
}

var (
	oauthMu     sync.Mutex
	oauthStates = make(map[string]pendingOAuth)
)

// RegisterRoutes mounts the integrations catalog, the per-agent binding
// management and the server-side OAuth connect flow on an HTTP mux.
func (s *Service) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/integrations", s.handleListCatalog)
	mux.HandleFunc("POST /api/integrations/{server_id}/connect", s.handleBeginConnect)
	mux.HandleFunc("GET /api/integrations/oauth/callback", s.handleOAuthCallback)
	mux.HandleFunc("PUT /api/integrations/connections/{connection_id}/agents/{agent_id}", s.handleUpsertBinding)
	mux.HandleFunc("DELETE /api/integrations/connections/{connection_id}/agents/{agent_id}", s.handleRemoveBinding)
	mux.HandleFunc("DELETE /api/integrations/connections/{connection_id}", s.handleDisconnectConnection)
}

// handleBeginConnect starts a sign-in: it remembers what was asked under a
// fresh state value and returns the provider URL the frontend should open.
func (s *Service) handleBeginConnect(w http.ResponseWriter, r *http.Request) {
	srvID := r.PathValue("server_id")
	var body struct {
		Label         string `json:"label"`
		ExpectedEmail string `json:"expected_email"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	state := "st_" + uuid.NewString()
	oauthMu.Lock()
	oauthStates[state] = pendingOAuth{
		ServerID:      srvID,
		Label:         body.Label,
		ExpectedEmail: body.ExpectedEmail,
	}
	oauthMu.Unlock()

	authURL, err := s.BuildProviderAuthURL(srvID, state, body.ExpectedEmail)
	if err != nil {
		oauthMu.Lock()
		delete(oauthStates, state)
		oauthMu.Unlock()
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"auth_url": authURL})
}

// handleOAuthCallback completes a sign-in: the state must match a pending
// request, the code is exchanged for a token, and the account pin is enforced
// before anything is stored.
func (s *Service) handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	code := r.URL.Query().Get("code")

	oauthMu.Lock()
	pending, ok := oauthStates[state]
	delete(oauthStates, state)
	oauthMu.Unlock()
	if !ok || code == "" {
		http.Error(w, "invalid or expired oauth state", http.StatusBadRequest)
		return
	}

	tokRaw, email, scopes, err := s.ExchangeProviderCode(r.Context(), pending.ServerID, code)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	if err := MCP.VerifyAuthorizedEmail(pending.ExpectedEmail, email); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}

	connID := "conn_" + uuid.NewString()
	if err := MCP.SaveSessionToken(pending.ServerID, connID, tokRaw); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	db := s.sqlDB()
	if db == nil {
		http.Error(w, "storage unavailable", http.StatusInternalServerError)
		return
	}
	_ = Storage.CreateMCPConnection(db, Storage.MCPConnection{
		ID:            connID,
		ServerID:      pending.ServerID,
		Label:         pending.Label,
		AccountEmail:  email,
		ExpectedEmail: pending.ExpectedEmail,
		Scopes:        scopes,
		Status:        "connected",
	})
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("Connected. You may close this window."))
}

func (s *Service) handleListCatalog(w http.ResponseWriter, r *http.Request) {
	catalog, err := s.ListCatalog(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(catalog)
}

func (s *Service) handleUpsertBinding(w http.ResponseWriter, r *http.Request) {
	connID := r.PathValue("connection_id")
	agentID := r.PathValue("agent_id")

	var body struct {
		AccessLevel string `json:"access_level"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if err := s.UpsertBotBinding(r.Context(), connID, agentID, body.AccessLevel); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) handleRemoveBinding(w http.ResponseWriter, r *http.Request) {
	connID := r.PathValue("connection_id")
	agentID := r.PathValue("agent_id")

	if err := s.RemoveBotBinding(r.Context(), connID, agentID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) handleDisconnectConnection(w http.ResponseWriter, r *http.Request) {
	connID := r.PathValue("connection_id")
	serverID := r.URL.Query().Get("server_id")

	if err := s.DisconnectConnection(r.Context(), serverID, connID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
