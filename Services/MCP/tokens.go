package MCP

import (
	"encoding/json"
	"sync"

	"github.com/anyscapelabs/argus/Keyring"
	"golang.org/x/oauth2"
)

type storedSession struct {
	ClientID     string   `json:"client_id"`
	ClientSecret string   `json:"client_secret,omitempty"`
	AuthURL      string   `json:"auth_url"`
	TokenURL     string   `json:"token_url"`
	Scopes       []string `json:"scopes"`
	RefreshToken string   `json:"refresh_token"`
}

func (s storedSession) config() *oauth2.Config {
	return &oauth2.Config{
		ClientID:     s.ClientID,
		ClientSecret: s.ClientSecret,
		Scopes:       s.Scopes,
		Endpoint:     oauth2.Endpoint{AuthURL: s.AuthURL, TokenURL: s.TokenURL},
	}
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
	if json.Unmarshal([]byte(raw), &s) != nil || s.RefreshToken == "" {
		return nil, nil, false
	}
	return s.config(), &oauth2.Token{RefreshToken: s.RefreshToken}, true
}

func (t tokenStore) save(c *oauth2.Config, tok *oauth2.Token) error {
	rt := tok.RefreshToken
	if rt == "" {
		if _, old, ok := t.load(); ok {
			rt = old.RefreshToken
		}
	}
	if rt == "" {
		return nil
	}
	b, _ := json.Marshal(storedSession{
		ClientID:     c.ClientID,
		ClientSecret: c.ClientSecret,
		Scopes:       c.Scopes,
		AuthURL:      c.Endpoint.AuthURL,
		TokenURL:     c.Endpoint.TokenURL,
		RefreshToken: rt,
	})
	return Keyring.SetSecret(t.ref(), string(b))
}

func (t tokenStore) clear() { _ = Keyring.DeleteSecret(t.ref()) }

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
