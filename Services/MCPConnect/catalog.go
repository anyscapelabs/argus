package MCPConnect

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/anyscapelabs/argus/Services/MCP"
	"github.com/anyscapelabs/argus/Services/Shared"
	"github.com/anyscapelabs/argus/Storage"
)

// AgentBindingView is one agent's binding on one connection, as the catalog
// page renders it.
type AgentBindingView struct {
	AgentID     string `json:"agent_id"`
	Handle      string `json:"handle"`
	AgentName   string `json:"agent_name"`
	AccessLevel string `json:"access_level"`
}

// ConnectionView is one signed-in account with the agents bound to it.
type ConnectionView struct {
	ConnectionID string             `json:"connection_id"`
	Label        string             `json:"label"`
	AccountEmail string             `json:"account_email"`
	Scopes       string             `json:"scopes"`
	Status       string             `json:"status"`
	Bindings     []AgentBindingView `json:"bindings"`
}

type DisconnectImpactView struct {
	Connection ConnectionView `json:"connection"`
}

// ConnectorCatalogView is one integration server as the catalog shows it: how
// many accounts are connected and who is bound to each.
type ConnectorCatalogView struct {
	ServerID     string           `json:"server_id"`
	DisplayName  string           `json:"display_name"`
	Category     string           `json:"category,omitempty"`
	MultiAccount bool             `json:"multi_account"`
	Custom       bool             `json:"custom,omitempty"`
	Connections  []ConnectionView `json:"connections"`
}

// sqlDB is the raw handle the Storage free-function subsystem works on.
func (s *Service) sqlDB() *sql.DB {
	if s.Core == nil || s.Core.DB == nil {
		return nil
	}
	return s.Core.DB.SQL()
}

// manager is the live MCP session registry; nil-safe because the catalog must
// still render (with no live state) before MCP service startup.
func (s *Service) manager() *MCP.Manager {
	if s.MCP == nil {
		return nil
	}
	return s.MCP.Manager()
}

// ListCatalog assembles the integrations page: the fixed catalog of servers,
// each annotated with its connected accounts and, per account, the agents
// bound to it with their access tiers.
func (s *Service) ListCatalog(_ context.Context) ([]ConnectorCatalogView, error) {
	db := s.sqlDB()
	if db == nil {
		return nil, fmt.Errorf("storage unavailable")
	}
	conns, err := Storage.ListMCPConnections(db)
	if err != nil {
		return nil, err
	}
	binds, err := Storage.ListAllConnectionBindings(db)
	if err != nil {
		return nil, err
	}

	byConn := make(map[string][]AgentBindingView)
	for _, b := range binds {
		byConn[b.ConnectionID] = append(byConn[b.ConnectionID], AgentBindingView{
			AgentID:     b.AgentID,
			Handle:      b.Handle,
			AgentName:   b.AgentName,
			AccessLevel: b.AccessLevel,
		})
	}

	byServer := make(map[string][]ConnectionView)
	for _, c := range conns {
		lbl := c.Label
		if lbl == "" {
			lbl = c.AccountEmail
		}
		byServer[c.ServerID] = append(byServer[c.ServerID], ConnectionView{
			ConnectionID: c.ID,
			Label:        lbl,
			AccountEmail: c.AccountEmail,
			Scopes:       c.Scopes,
			Status:       c.Status,
			Bindings:     byConn[c.ID],
		})
	}

	catalog := s.connectorCatalog()
	for i := range catalog {
		catalog[i].Connections = byServer[catalog[i].ServerID]
		if catalog[i].Connections == nil {
			catalog[i].Connections = []ConnectionView{}
		}
	}
	// User-added servers ride the same view so the detail page can manage
	// them. They hold no OAuth accounts, so their connection list stays empty.
	if s.MCP != nil {
		for _, custom := range s.MCP.ListCustomServers() {
			if _, seen := byServer[custom.ID]; seen {
				continue
			}
			catalog = append(catalog, ConnectorCatalogView{
				ServerID:     custom.ID,
				DisplayName:  custom.Name,
				Category:     "Custom",
				MultiAccount: false,
				Custom:       true,
				Connections:  []ConnectionView{},
			})
		}
	}
	return catalog, nil
}

// UpsertBotBinding attaches an agent to a connection, or changes its access
// tier. An empty tier means draft: new bindings never start with send rights.
func (s *Service) UpsertBotBinding(_ context.Context, connectionID, agentID, accessLevel string) error {
	if connectionID == "" || agentID == "" {
		return fmt.Errorf("connection_id and agent_id are required")
	}
	db := s.sqlDB()
	if db == nil {
		return fmt.Errorf("storage unavailable")
	}
	if err := Storage.UpsertAgentConnection(db, agentID, connectionID, accessLevel); err != nil {
		return err
	}
	s.Core.EmitEvent(Shared.EventIntegrationsChanged, nil)
	return nil
}

// RemoveBotBinding detaches an agent from a connection without touching the
// connection itself: the account stays signed in for everyone else.
func (s *Service) RemoveBotBinding(_ context.Context, connectionID, agentID string) error {
	if connectionID == "" || agentID == "" {
		return fmt.Errorf("connection_id and agent_id are required")
	}
	db := s.sqlDB()
	if db == nil {
		return fmt.Errorf("storage unavailable")
	}
	if err := Storage.RemoveAgentConnection(db, agentID, connectionID); err != nil {
		return err
	}
	s.Core.EmitEvent(Shared.EventIntegrationsChanged, nil)
	return nil
}

// PreviewDisconnect returns the account and every agent binding a disconnect
// would remove. The UI presents this before the destructive action.
func (s *Service) PreviewDisconnect(_ context.Context, connectionID string) (DisconnectImpactView, error) {
	db := s.sqlDB()
	if db == nil {
		return DisconnectImpactView{}, fmt.Errorf("storage unavailable")
	}
	conn, found, err := Storage.GetMCPConnection(db, connectionID)
	if err != nil || !found {
		if err == nil {
			err = fmt.Errorf("connection not found")
		}
		return DisconnectImpactView{}, err
	}
	bindings, err := Storage.ListAllConnectionBindings(db)
	if err != nil {
		return DisconnectImpactView{}, err
	}
	view := ConnectionView{
		ConnectionID: conn.ID, Label: conn.Label, AccountEmail: conn.AccountEmail,
		Scopes: conn.Scopes, Status: conn.Status, Bindings: []AgentBindingView{},
	}
	for _, binding := range bindings {
		if binding.ConnectionID != connectionID {
			continue
		}
		view.Bindings = append(view.Bindings, AgentBindingView{
			AgentID: binding.AgentID, Handle: binding.Handle,
			AgentName: binding.AgentName, AccessLevel: binding.AccessLevel,
		})
	}
	return DisconnectImpactView{Connection: view}, nil
}

// DisconnectConnection derives the server from storage, refuses new calls,
// cancels the account session, revokes Google grants best-effort, then removes
// the token and row (whose bindings cascade).
func (s *Service) DisconnectConnection(ctx context.Context, connectionID string) error {
	if connectionID == "" {
		return fmt.Errorf("connection_id is required")
	}
	db := s.sqlDB()
	if db == nil {
		return fmt.Errorf("storage unavailable")
	}
	conn, found, err := Storage.GetMCPConnection(db, connectionID)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("connection not found")
	}
	if err := Storage.SetMCPConnectionStatus(db, connectionID, "disconnecting"); err != nil {
		return err
	}
	s.Core.EmitEvent(Shared.EventIntegrationsChanged, nil)
	if m := s.manager(); m != nil {
		m.CloseConnection(connectionID)
	}
	if cat := s.catalog(); cat != nil {
		if item, ok := cat.Get(conn.ServerID); ok && item.OAuth.Mode == "google" {
			_ = MCP.RevokeGoogleSession(ctx, conn.ServerID, connectionID)
		}
	}
	_ = MCP.DeleteSessionToken(conn.ServerID, connectionID)
	if err := Storage.DeleteMCPConnection(db, connectionID); err != nil {
		return err
	}
	s.Core.EmitEvent(Shared.EventIntegrationsChanged, nil)
	return nil
}

func (s *Service) RenameConnection(_ context.Context, connectionID, label string) error {
	db := s.sqlDB()
	if db == nil {
		return fmt.Errorf("storage unavailable")
	}
	conn, found, err := Storage.GetMCPConnection(db, connectionID)
	if err != nil || !found {
		if err == nil {
			err = fmt.Errorf("connection not found")
		}
		return err
	}
	if exists, err := Storage.ConnectionLabelExists(db, conn.ServerID, label, connectionID); err != nil {
		return err
	} else if exists {
		return fmt.Errorf("%s already has an account labelled %q", conn.ServerID, strings.TrimSpace(label))
	}
	if err := Storage.RenameMCPConnection(db, connectionID, label); err != nil {
		return err
	}
	s.Core.EmitEvent(Shared.EventIntegrationsChanged, nil)
	return nil
}

// providerOAuthMeta describes one first-party OAuth provider the HTTP
// deployment connects directly (authorization-URL build, code exchange,
// userinfo lookup). The desktop build's Google connectors go through the MCP
// SDK's flow instead; this registry is that same knowledge, server-side.
type providerOAuthMeta struct {
	DisplayName  string
	MultiAccount bool
	AuthURL      string
	TokenURL     string
	UserInfoURL  string
	ClientIDEnv  string
	SecretEnv    string
	Scopes       []string
}

var providerRegistry = map[string]providerOAuthMeta{
	"gmail": {
		DisplayName:  "Gmail",
		MultiAccount: true,
		AuthURL:      "https://accounts.google.com/o/oauth2/v2/auth",
		TokenURL:     "https://oauth2.googleapis.com/token",
		UserInfoURL:  "https://www.googleapis.com/oauth2/v2/userinfo",
		ClientIDEnv:  "GOOGLE_OAUTH_CLIENT_ID",
		SecretEnv:    "GOOGLE_OAUTH_CLIENT_SECRET",
		Scopes: []string{
			"openid",
			"email",
			"https://www.googleapis.com/auth/gmail.modify",
			"https://www.googleapis.com/auth/gmail.compose",
		},
	},
	"google-drive": {
		DisplayName:  "Google Drive",
		MultiAccount: true,
		AuthURL:      "https://accounts.google.com/o/oauth2/v2/auth",
		TokenURL:     "https://oauth2.googleapis.com/token",
		UserInfoURL:  "https://www.googleapis.com/oauth2/v2/userinfo",
		ClientIDEnv:  "GOOGLE_OAUTH_CLIENT_ID",
		SecretEnv:    "GOOGLE_OAUTH_CLIENT_SECRET",
		Scopes:       []string{"openid", "email", "https://www.googleapis.com/auth/drive.readonly"},
	},
}

func (s *Service) connectorCatalog() []ConnectorCatalogView {
	cat := s.catalog()
	if cat == nil {
		return nil
	}
	out := make([]ConnectorCatalogView, 0, len(cat.Items()))
	for _, item := range cat.Items() {
		if item.URL == "" {
			continue
		}
		out = append(out, ConnectorCatalogView{
			ServerID:     item.ID,
			DisplayName:  item.Name,
			Category:     item.Category,
			MultiAccount: true,
			Connections:  []ConnectionView{},
		})
	}
	return out
}

// AgentPromptBlock describes only this agent's usable accounts, plus the
// machine-level accounts it may ask the user to bind.
func (s *Service) AgentPromptBlock(agentID string, withTools bool) string {
	db := s.sqlDB()
	if db == nil {
		return ""
	}
	bound, err := Storage.ListAgentConnectionsForAgent(db, agentID)
	if err != nil {
		return ""
	}
	all, err := Storage.ListMCPConnections(db)
	if err != nil {
		return ""
	}
	boundIDs := map[string]bool{}
	var b strings.Builder
	b.WriteString("\n## Your connected accounts\n")
	if len(bound) == 0 {
		b.WriteString("- None.\n")
	}
	for _, conn := range bound {
		boundIDs[conn.ConnectionID] = true
		label := conn.Label
		if label == "" {
			label = conn.AccountEmail
		}
		fmt.Fprintf(&b, "- %s / %s (%s, access:%s, connection_id:%s)\n",
			conn.ServerID, label, conn.Status, conn.AccessLevel, conn.ConnectionID)
	}
	var available []string
	for _, conn := range all {
		if boundIDs[conn.ID] || conn.Status != "connected" {
			continue
		}
		label := conn.Label
		if label == "" {
			label = conn.AccountEmail
		}
		available = append(available, fmt.Sprintf("- %s / %s (connection_id:%s)", conn.ServerID, label, conn.ID))
	}
	if len(available) > 0 {
		b.WriteString("Accounts connected on this machine but not bound to you:\n")
		b.WriteString(strings.Join(available, "\n"))
		b.WriteString("\n")
	}
	if withTools {
		b.WriteString("Use connect_mcp to request a new account or user-approved access to an existing connection_id. Never imply you can use an unbound account.\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// callbackPath is the redirect URI path both legs of the server-side flow
// agree on; ARGUS_PUBLIC_URL supplies the host in front of it.
const callbackPath = "/api/integrations/oauth/callback"

// BuildProviderAuthURL assembles the authorization URL for one server,
// pinning the account picker to the expected email when the user gave one.
func (s *Service) BuildProviderAuthURL(serverID, state, expectedEmail string) (string, error) {
	meta, ok := providerRegistry[serverID]
	if !ok {
		return "", fmt.Errorf("unknown integration provider %q", serverID)
	}
	cid := strings.TrimSpace(os.Getenv(meta.ClientIDEnv))
	if cid == "" {
		return "", fmt.Errorf("missing %s", meta.ClientIDEnv)
	}
	redir := strings.TrimRight(os.Getenv("ARGUS_PUBLIC_URL"), "/") + callbackPath
	return MCP.BuildAuthURL(meta.AuthURL, MCP.OAuthStartOptions{
		ClientID:      cid,
		RedirectURI:   redir,
		Scopes:        meta.Scopes,
		State:         state,
		ExpectedEmail: expectedEmail,
	})
}

// ExchangeProviderCode trades the authorization code for a token, then reads
// the account email off userinfo so the caller can verify the pin and label
// the connection. The raw token body is returned verbatim: it is stored in
// the keyring exactly as the provider sent it.
func (s *Service) ExchangeProviderCode(ctx context.Context, serverID, code string) (string, string, string, error) {
	meta, ok := providerRegistry[serverID]
	if !ok {
		return "", "", "", fmt.Errorf("unknown integration provider %q", serverID)
	}
	redir := strings.TrimRight(os.Getenv("ARGUS_PUBLIC_URL"), "/") + callbackPath
	form := url.Values{
		"code":          {code},
		"client_id":     {os.Getenv(meta.ClientIDEnv)},
		"client_secret": {os.Getenv(meta.SecretEnv)},
		"redirect_uri":  {redir},
		"grant_type":    {"authorization_code"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, meta.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", "", "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", "", err
	}
	defer resp.Body.Close()

	rawTok, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", "", err
	}
	if resp.StatusCode >= 300 {
		return "", "", "", fmt.Errorf("oauth token exchange failed (%d): %s", resp.StatusCode, string(rawTok))
	}

	var tok struct {
		AccessToken string `json:"access_token"`
		Scope       string `json:"scope"`
	}
	_ = json.Unmarshal(rawTok, &tok)

	uReq, _ := http.NewRequestWithContext(ctx, http.MethodGet, meta.UserInfoURL, nil)
	uReq.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	uResp, err := http.DefaultClient.Do(uReq)
	if err != nil {
		// The token itself succeeded; the email is decoration. Store the
		// connection unlabelled rather than throwing away a working grant.
		return string(rawTok), "", tok.Scope, nil
	}
	defer uResp.Body.Close()

	var info struct {
		Email string `json:"email"`
	}
	_ = json.NewDecoder(uResp.Body).Decode(&info)
	return string(rawTok), info.Email, tok.Scope, nil
}
