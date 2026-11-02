package MCP

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/anyscapelabs/argus/Keyring"
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
