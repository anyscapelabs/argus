package MCP

import (
	"context"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
	"golang.org/x/oauth2"
)

// RemoteSpec describes a remote MCP server.
type RemoteSpec struct {
	ID      string
	Name    string
	URL     string
	Scopes  []string
	Client  *oauthex.ClientCredentials // nil => dynamic registration
}

func (m *Manager) ConnectRemote(ctx context.Context, spec RemoteSpec, interactive bool) error {
	m.mu.RLock()
	_, already := m.servers[spec.ID]
	m.mu.RUnlock()
	if already {
		return nil
	}
	store := tokenStore{serverID: spec.ID}
	cfg := &auth.AuthorizationCodeHandlerConfig{
		RedirectURL:              CallbackURL,
		AuthorizationCodeFetcher: fetchCode(interactive),
		RequestRefreshToken:      true,
		NewTokenSource: func(c context.Context, oc *oauth2.Config, t *oauth2.Token) (oauth2.TokenSource, error) {
			_ = store.save(oc, t)
			return &savingSource{src: oc.TokenSource(c, t), cfg: oc, save: store.save, last: t.AccessToken}, nil
		},
	}
	if spec.Client != nil {
		cfg.PreregisteredClient = spec.Client
	} else {
		cfg.DynamicClientRegistrationConfig = &auth.DynamicClientRegistrationConfig{
			Metadata: &oauthex.ClientRegistrationMetadata{
				ClientName:    "Argus",
				RedirectURIs:  []string{CallbackURL},
				GrantTypes:    []string{"authorization_code", "refresh_token"},
				ResponseTypes: []string{"code"},
			},
		}
	}
	if len(spec.Scopes) > 0 {
		cfg.ScopeFilter = func([]string) []string { return spec.Scopes }
	}
	if oc, tok, ok := store.load(); ok {
		cfg.InitialTokenSource = &savingSource{src: oc.TokenSource(context.Background(), tok), cfg: oc, save: store.save}
	}
	h, err := auth.NewAuthorizationCodeHandler(cfg)
	if err != nil {
		return err
	}

	connCtx, cancel := context.WithCancel(context.Background())
	stop := context.AfterFunc(ctx, cancel)
	timer := time.AfterFunc(4*time.Minute, cancel)
	client := mcp.NewClient(&mcp.Implementation{Name: "argus", Version: "0.1.0"}, nil)
	sess, err := client.Connect(connCtx, &mcp.StreamableClientTransport{Endpoint: spec.URL, OAuthHandler: h}, nil)
	timer.Stop()
	stop()
	if err != nil {
		cancel()
		return err
	}
	m.mu.Lock()
	m.servers[spec.ID] = &serverConn{
		cfg:    ServerConfig{ID: spec.ID, Name: spec.Name, Enabled: true},
		client: client,
		sess:   sess,
		cancel: cancel,
	}
	m.refreshToolsLocked()
	m.mu.Unlock()
	return nil
}

func (m *Manager) IsConnected(id string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.servers[id]
	return ok
}

func (m *Manager) DisconnectServer(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if sc, ok := m.servers[id]; ok {
		if sc.cancel != nil {
			sc.cancel()
		}
		if sc.sess != nil {
			_ = sc.sess.Close()
		}
		delete(m.servers, id)
	}
	m.refreshToolsLocked()
}
