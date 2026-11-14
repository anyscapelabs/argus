package MCP

import (
	"database/sql"
	"encoding/json"
	"sync"
	"time"

	"github.com/anyscapelabs/argus/Keyring"
	"github.com/anyscapelabs/argus/Storage"
	"golang.org/x/oauth2"
)

type storedSession struct {
	ClientID     string    `json:"client_id"`
	ClientSecret string    `json:"client_secret,omitempty"`
	AuthURL      string    `json:"auth_url"`
	TokenURL     string    `json:"token_url"`
	Scopes       []string  `json:"scopes"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	AccessToken  string    `json:"access_token,omitempty"`
	Expiry       time.Time `json:"expiry,omitempty"`
}

func (s storedSession) config() *oauth2.Config {
	return &oauth2.Config{
		ClientID:     s.ClientID,
		ClientSecret: s.ClientSecret,
		Scopes:       s.Scopes,
		Endpoint:     oauth2.Endpoint{AuthURL: s.AuthURL, TokenURL: s.TokenURL},
	}
}

// usable builds the token that lets this session reconnect without prompting.
// A refresh token is the normal route. GitHub's OAuth App hands out a
// non-expiring access token and no refresh token at all, so there the stored
// access token is the session.
func (s storedSession) usable() (*oauth2.Config, *oauth2.Token, bool) {
	if s.RefreshToken == "" {
		if s.AccessToken == "" {
			return nil, nil, false
		}
		if e := s.Expiry; !e.IsZero() && time.Now().Add(time.Minute).After(e) {
			return nil, nil, false
		}
	}
	tok := &oauth2.Token{
		RefreshToken: s.RefreshToken,
		AccessToken:  s.AccessToken,
		TokenType:    "Bearer",
		Expiry:       s.Expiry,
	}
	return s.config(), tok, true
}

type tokenStore struct {
	serverID string
}

func (t tokenStore) ref() string { return "mcp/" + t.serverID + "/session" }

func (t tokenStore) load() (*oauth2.Config, *oauth2.Token, bool) {
	raw, err := Keyring.GetSecret(t.ref())
	if err != nil {
		return nil, nil, false
	}
	var s storedSession
	if json.Unmarshal([]byte(raw), &s) != nil {
		return nil, nil, false
	}
	return s.usable()
}

func (t tokenStore) save(c *oauth2.Config, tok *oauth2.Token) error {
	rt := tok.RefreshToken
	if rt == "" {
		if _, old, ok := t.load(); ok {
			rt = old.RefreshToken
		}
	}
	if rt == "" && tok.AccessToken == "" {
		return nil
	}
	b, _ := json.Marshal(storedSession{
		ClientID:     c.ClientID,
		ClientSecret: c.ClientSecret,
		Scopes:       c.Scopes,
		AuthURL:      c.Endpoint.AuthURL,
		TokenURL:     c.Endpoint.TokenURL,
		RefreshToken: rt,
		AccessToken:  tok.AccessToken,
		Expiry:       tok.Expiry,
	})
	return Keyring.SetSecret(t.ref(), string(b))
}

func (t tokenStore) clear() { _ = Keyring.DeleteSecret(t.ref()) }

// hasSession reports whether this server was signed in before, so that
// reconnection can be attempted without ever prompting.
func (t tokenStore) hasSession() bool {
	_, _, ok := t.load()
	return ok
}

// savingSource persists the session whenever the access token changes.
type savingSource struct {
	mu   sync.Mutex
	src  oauth2.TokenSource
	cfg  *oauth2.Config
	save func(*oauth2.Config, *oauth2.Token) error
	last string
}

func (s *savingSource) Token() (*oauth2.Token, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tok, err := s.src.Token()
	if err != nil {
		return nil, err
	}
	if tok.AccessToken != s.last {
		s.last = tok.AccessToken
		_ = s.save(s.cfg, tok)
	}
	return tok, nil
}

// sessionKey is the keyring ref for one connection's raw token. A server with
// no connection id keeps the legacy single-account ref; every signed-in
// account after that gets its own ref, so two inboxes on the same server
// never share a token.
func sessionKey(serverID, connectionID string) string {
	if connectionID == "" {
		return "mcp/" + serverID + "/session"
	}
	return "mcp/" + serverID + "/" + connectionID + "/session"
}

// SaveSessionToken persists one connection's raw token under its own ref.
func SaveSessionToken(serverID, connectionID, raw string) error {
	return Keyring.SetSecret(sessionKey(serverID, connectionID), raw)
}

// LoadSessionToken reads one connection's raw token.
func LoadSessionToken(serverID, connectionID string) (string, error) {
	return Keyring.GetSecret(sessionKey(serverID, connectionID))
}

// DeleteSessionToken removes one connection's raw token.
func DeleteSessionToken(serverID, connectionID string) error {
	return Keyring.DeleteSecret(sessionKey(serverID, connectionID))
}

// MigrateLegacyTokens upgrades opaque, pre-connection tokens into
// per-connection keyring entries plus a default connection row bound to every
// agent, so an install that signed in before connections existed keeps its
// session and gains per-agent access tiers in one step.
//
// Values that parse as a storedSession are left alone: those belong to the
// SDK OAuth flow and are keyed by server, not by account; the flow renews
// them itself and must not be adopted as a raw connection token.
func MigrateLegacyTokens(db *sql.DB, knownServers []string, agentIDs []string) error {
	for _, srvID := range knownServers {
		raw, err := Keyring.GetSecret(sessionKey(srvID, ""))
		if err != nil || raw == "" {
			continue
		}
		var probe storedSession
		if json.Unmarshal([]byte(raw), &probe) == nil && (probe.AccessToken != "" || probe.RefreshToken != "") {
			continue
		}

		connID := "conn_" + srvID + "_default"
		if err := SaveSessionToken(srvID, connID, raw); err != nil {
			return err
		}

		_ = Storage.CreateMCPConnection(db, Storage.MCPConnection{
			ID:       connID,
			ServerID: srvID,
			Label:    "Default",
			Status:   "connected",
		})

		// Gmail can send on someone's behalf, so the seeded tier stops at
		// drafts; read-mostly servers start at full.
		tier := "full"
		if srvID == "gmail" {
			tier = "draft"
		}
		for _, agID := range agentIDs {
			_ = Storage.UpsertAgentConnection(db, agID, connID, tier)
		}
		_ = Keyring.DeleteSecret(sessionKey(srvID, ""))
	}
	return nil
}
