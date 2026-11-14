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
	Status       string             `json:"status"`
	Bindings     []AgentBindingView `json:"bindings"`
}

// ConnectorCatalogView is one integration server as the catalog shows it: how
// many accounts are connected and who is bound to each.
type ConnectorCatalogView struct {
	ServerID     string           `json:"server_id"`
	DisplayName  string           `json:"display_name"`
	MultiAccount bool             `json:"multi_account"`
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
			Status:       c.Status,
			Bindings:     byConn[c.ID],
		})
	}

	catalog := defaultConnectorCatalog()
	for i := range catalog {
		catalog[i].Connections = byServer[catalog[i].ServerID]
		if catalog[i].Connections == nil {
			catalog[i].Connections = []ConnectionView{}
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
	return Storage.UpsertAgentConnection(db, agentID, connectionID, accessLevel)
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
	return Storage.RemoveAgentConnection(db, agentID, connectionID)
}

// DisconnectConnection revokes one account: its live client cache, its token
// and its row all go. When it was the server's last connection the shared
// session is dropped too, so nothing lingers that the user asked to remove.
func (s *Service) DisconnectConnection(_ context.Context, serverID, connectionID string) error {
	if connectionID == "" {
		return fmt.Errorf("connection_id is required")
	}
	db := s.sqlDB()
	if db == nil {
		return fmt.Errorf("storage unavailable")
	}
	if m := s.manager(); m != nil {
		m.CloseConnection(connectionID)
	}
	_ = MCP.DeleteSessionToken(serverID, connectionID)
	if err := Storage.DeleteMCPConnection(db, connectionID); err != nil {
		return err
	}
	remaining, err := Storage.ListMCPConnections(db)
	if err != nil {
		return err
	}
	for _, c := range remaining {
		if c.ServerID == serverID {
			return nil // another account still needs this server's session
		}
	}
	if m := s.manager(); m != nil && serverID != "" {
		m.DisconnectServer(serverID)
	}
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
	"gdrive": {
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

// defaultConnectorCatalog is the server list the catalog always shows, in a
// deliberate order; per-connection state is merged in by ListCatalog.
func defaultConnectorCatalog() []ConnectorCatalogView {
	order := []string{"gmail", "gdrive"}
	out := make([]ConnectorCatalogView, 0, len(order))
	for _, id := range order {
		m := providerRegistry[id]
		out = append(out, ConnectorCatalogView{
			ServerID:     id,
			DisplayName:  m.DisplayName,
			MultiAccount: m.MultiAccount,
			Connections:  []ConnectionView{},
		})
	}
	return out
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
