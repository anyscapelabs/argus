package MCP

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/anyscapelabs/argus/Keyring"
	"github.com/anyscapelabs/argus/Storage"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
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
	serverID     string
	connectionID string
}

func (t tokenStore) ref() string { return sessionKey(t.serverID, t.connectionID) }

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

func (t tokenStore) clientCredentials() (*oauthex.ClientCredentials, bool) {
	raw, err := Keyring.GetSecret(t.ref())
	if err != nil {
		return nil, false
	}
	var stored storedSession
	if json.Unmarshal([]byte(raw), &stored) != nil || stored.ClientID == "" {
		return nil, false
	}
	credentials := &oauthex.ClientCredentials{ClientID: stored.ClientID}
	if stored.ClientSecret != "" {
		credentials.ClientSecretAuth = &oauthex.ClientSecretAuth{ClientSecret: stored.ClientSecret}
	}
	return credentials, true
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

// SaveOAuthSession normalizes a provider token response into the same durable
// session format used by the desktop SDK flow. HTTP OAuth callbacks can then
// restore and refresh per-connection MCP sessions through the common dialer.
func SaveOAuthSession(serverID, connectionID, clientID, clientSecret, authURL, tokenURL string, scopes []string, raw string) error {
	var payload struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return err
	}
	if payload.AccessToken == "" && payload.RefreshToken == "" {
		return fmt.Errorf("oauth response contained no usable token")
	}
	token := &oauth2.Token{
		AccessToken: payload.AccessToken, RefreshToken: payload.RefreshToken,
		TokenType: payload.TokenType,
	}
	if payload.ExpiresIn > 0 {
		token.Expiry = time.Now().Add(time.Duration(payload.ExpiresIn) * time.Second)
	}
	config := &oauth2.Config{
		ClientID: clientID, ClientSecret: clientSecret, Scopes: scopes,
		Endpoint: oauth2.Endpoint{AuthURL: authURL, TokenURL: tokenURL},
	}
	return tokenStore{serverID: serverID, connectionID: connectionID}.save(config, token)
}

// MigrateLegacyTokens upgrades opaque, pre-connection tokens into
// per-connection keyring entries plus a default connection row bound to every
// agent, so an install that signed in before connections existed keeps its
// session and gains per-agent access tiers in one step.
func MigrateLegacyTokens(db *sql.DB, knownServers []string, agentIDs []string) error {
	for _, srvID := range knownServers {
		raw, err := Keyring.GetSecret(sessionKey(srvID, ""))
		if err != nil || raw == "" {
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

// SessionAccountEmail reads the identity attached to a Google OAuth session.
// The token is refreshed through the same saving source used by MCP calls, so
// an email verification cannot leave a newer access token only in memory.
func SessionAccountEmail(ctx context.Context, serverID, connectionID string) (string, error) {
	raw, err := LoadSessionToken(serverID, connectionID)
	if err != nil {
		return "", err
	}
	var stored storedSession
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		return "", err
	}
	cfg, tok, ok := stored.usable()
	if !ok {
		return "", fmt.Errorf("stored session is not usable")
	}
	store := tokenStore{serverID: serverID, connectionID: connectionID}
	source := &savingSource{src: cfg.TokenSource(ctx, tok), cfg: cfg, save: store.save, last: tok.AccessToken}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://www.googleapis.com/oauth2/v2/userinfo", nil)
	if err != nil {
		return "", err
	}
	resp, err := oauth2.NewClient(ctx, source).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", fmt.Errorf("userinfo failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var info struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return "", err
	}
	return strings.TrimSpace(info.Email), nil
}

// GoogleHTTPClient builds an authenticated *http.Client for a stored Google
// session, refreshing and persisting tokens like live MCP calls do.
func GoogleHTTPClient(ctx context.Context, serverID, connectionID string) (*http.Client, error) {
	raw, err := LoadSessionToken(serverID, connectionID)
	if err != nil {
		return nil, err
	}
	var stored storedSession
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		return nil, err
	}
	cfg, tok, ok := stored.usable()
	if !ok {
		return nil, fmt.Errorf("stored session is not usable")
	}
	store := tokenStore{serverID: serverID, connectionID: connectionID}
	source := &savingSource{src: cfg.TokenSource(ctx, tok), cfg: cfg, save: store.save, last: tok.AccessToken}
	return oauth2.NewClient(ctx, source), nil
}

// RevokeGoogleSession is best effort at the service layer: callers still
// remove the local grant when Google's revocation endpoint is unavailable.
func RevokeGoogleSession(ctx context.Context, serverID, connectionID string) error {
	raw, err := LoadSessionToken(serverID, connectionID)
	if err != nil {
		return err
	}
	var tokenValue string
	var stored storedSession
	if json.Unmarshal([]byte(raw), &stored) == nil {
		tokenValue = stored.RefreshToken
		if tokenValue == "" {
			tokenValue = stored.AccessToken
		}
	}
	if tokenValue == "" {
		var generic struct {
			RefreshToken string `json:"refresh_token"`
			AccessToken  string `json:"access_token"`
		}
		if json.Unmarshal([]byte(raw), &generic) == nil {
			tokenValue = generic.RefreshToken
			if tokenValue == "" {
				tokenValue = generic.AccessToken
			}
		}
	}
	if tokenValue == "" {
		return nil
	}
	form := url.Values{"token": {tokenValue}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://oauth2.googleapis.com/revoke", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("oauth revoke failed (%d)", resp.StatusCode)
	}
	return nil
}
