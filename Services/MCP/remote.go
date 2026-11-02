package MCP

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
	"golang.org/x/oauth2"
)

// RemoteSpec describes a remote MCP server.
type RemoteSpec struct {
	ID        string
	Name      string
	URL       string
	Scopes    []string
	ProbeTool string                     // read-only tool that triggers the auth challenge; empty => not needed
	Callback  string                     // path appended to the loopback redirect URI; providers match it exactly
	Client    *oauthex.ClientCredentials // nil => dynamic registration
}

func (m *Manager) ConnectRemote(ctx context.Context, spec RemoteSpec, interactive bool) error {
	m.mu.RLock()
	_, already := m.servers[spec.ID]
	m.mu.RUnlock()
	if already {
		return nil
	}
	store := tokenStore{serverID: spec.ID}
	redirectURL := PlaceholderRedirectURL
	// The listener is bound before the handler is built because its port is
	// part of the redirect URI, and the URI has to be inside the authorization
	// URL that the SDK assembles. Non-interactive connects never open a browser
	// so they need no listener at all.
	var cb *callback
	if interactive {
		var err error
		cb, err = newCallback(spec.Callback)
		if err != nil {
			return err
		}
		defer cb.close()
		redirectURL = cb.url
	}
	cfg := &auth.AuthorizationCodeHandlerConfig{
		RedirectURL:              redirectURL,
		AuthorizationCodeFetcher: fetchCode(interactive, cb),
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
				RedirectURIs:  []string{redirectURL},
				GrantTypes:    []string{"authorization_code", "refresh_token"},
				ResponseTypes: []string{"code"},
				// "native" is what this is. Left out, an OIDC-flavoured
				// authorization server treats the client as a web app, which is
				// the wrong assumption to make about a desktop binary holding a
				// loopback redirect.
				ApplicationType: "native",
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
	// Only a silent connect gets a deadline. Linear, Notion and Todoist reject
	// the very first request, so the SDK runs the entire browser sign-in inside
	// Connect and the session does not exist until the user is done: a 30 second
	// guard would cancel a flow that is merely waiting on a person. Interactive
	// connects are bounded by the card, which the user can cancel, and
	// non-interactive ones never open a browser at all.
	var timer *time.Timer
	if !interactive {
		timer = time.AfterFunc(30*time.Second, cancel)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "argus", Version: "0.1.0"}, nil)
	sess, err := client.Connect(connCtx, &mcp.StreamableClientTransport{Endpoint: spec.URL, OAuthHandler: h}, nil)
	if err != nil {
		stopTimer(timer)
		stop()
		cancel()
		return err
	}
	// The handshake stays open until the sign-in below finishes, so release it
	// once the session exists.
	stopTimer(timer)
	stop()
	if err := authorizeProbe(ctx, spec, sess, h); err != nil {
		_ = sess.Close()
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

// stopTimer exists because the interactive path deliberately sets no deadline:
// the user may be sitting in a browser, and the only clock that should stop
// that is the one on the connection card.
func stopTimer(t *time.Timer) {
	if t != nil {
		t.Stop()
	}
}

// authorizeProbe makes the server demand credentials.
//
// The Google MCP endpoints (Gmail, Calendar) reply 200 to initialize and
// tools/list even with no Authorization header, and the SDK only starts the
// OAuth flow on a 401/403. Without this call the connection looks successful,
// no browser ever opens and every tool later fails for lack of a token.
// A read-only tool call is the cheapest request that does return the challenge.
func authorizeProbe(ctx context.Context, spec RemoteSpec, sess *mcp.ClientSession, h *auth.AuthorizationCodeHandler) error {
	if spec.ProbeTool == "" {
		return nil
	}
	if ts, err := h.TokenSource(ctx); err == nil && ts != nil {
		return nil // a usable token already exists
	}
	// Long enough for the user to finish in the browser, short enough that an
	// abandoned sign-in reports a reason instead of hanging on "authorizing".
	authCtx, cancelAuth := context.WithTimeout(ctx, 5*time.Minute)
	defer cancelAuth()
	res, callErr := sess.CallTool(authCtx, &mcp.CallToolParams{Name: spec.ProbeTool})
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	// The token is the real signal. The probe call itself can still come back
	// as a tool error, e.g. when this integration's granted scopes do not cover
	// the probe tool, and that must not undo a completed sign-in.
	if ts, err := h.TokenSource(context.Background()); err == nil && ts != nil {
		return nil
	}
	switch {
	case callErr != nil && errors.Is(callErr, context.DeadlineExceeded):
		return fmt.Errorf("%s: sign-in timed out", spec.Name)
	case callErr != nil:
		return fmt.Errorf("%s: sign-in did not complete: %w", spec.Name, callErr)
	case res != nil && res.IsError:
		return fmt.Errorf("%s: sign-in did not complete: %s", spec.Name, resultText(res))
	default:
		return fmt.Errorf("%s: the browser sign-in did not complete", spec.Name)
	}
}

func resultText(res *mcp.CallToolResult) string {
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok && strings.TrimSpace(tc.Text) != "" {
			return strings.TrimSpace(tc.Text)
		}
	}
	return "no detail returned"
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
