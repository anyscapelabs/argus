package MCP

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/oauthex"
	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/anyscapelabs/argus/Services/Integrations"
	"github.com/anyscapelabs/argus/Services/Shared"
	"github.com/anyscapelabs/argus/Storage"
)

// Service exposes MCP management to the frontend.
type Service struct {
	Core *Shared.Core

	mu      sync.RWMutex
	manager *Manager
	catalog *Integrations.Catalog
}

// ServiceStartup initializes the MCP manager.
func (s *Service) ServiceStartup(ctx context.Context, options application.ServiceOptions) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.manager = New()
	if cat, err := Integrations.Default(); err == nil {
		s.catalog = cat
		go s.restoreRemoteSessions(cat)
	}
	go s.restoreCustomServers()
	return nil
}

// restoreRemoteSessions brings back integrations that were signed in on an
// earlier run. interactive=false guarantees nothing here can open a browser:
// a session that cannot be renewed on its own is left for the connect card, and
// the failure is silent because the user never asked for these servers today.
func (s *Service) restoreRemoteSessions(cat *Integrations.Catalog) {
	if s.Core == nil || s.Core.DB == nil {
		return
	}
	db := s.Core.DB.SQL()
	agentIDs, _ := Storage.ListAllAgentIDs(db)
	_ = MigrateLegacyTokens(db, cat.IDs(), agentIDs)
	connections, err := Storage.ListMCPConnections(db)
	if err != nil {
		return
	}
	for _, conn := range connections {
		if conn.Status != "connected" || !(tokenStore{serverID: conn.ServerID, connectionID: conn.ID}).hasSession() {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		err := s.ConnectRemoteConnection(ctx, conn.ServerID, conn.ID, conn.ExpectedEmail, false)
		cancel()
		if err != nil {
			_ = Storage.SetMCPConnectionStatus(db, conn.ID, "needs_reauth")
		}
	}
}

// ServiceShutdown disconnects all servers. Wails' lifecycle interface takes no
// context; a context parameter makes the hook silently ineligible.
func (s *Service) ServiceShutdown() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.manager != nil {
		s.manager.Disconnect()
	}
	return nil
}

// Manager returns the underlying manager.
func (s *Service) Manager() *Manager {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.manager
}

// ListServers returns all server configurations.
func (s *Service) ListServers() []ServerConfig {
	s.mu.RLock()
	m := s.manager
	s.mu.RUnlock()
	if m == nil {
		return nil
	}
	return m.ListConfigs()
}

// AddServer adds a server configuration.
func (s *Service) AddServer(cfg ServerConfig) {
	s.mu.RLock()
	m := s.manager
	s.mu.RUnlock()
	if m == nil {
		return
	}
	m.AddConfig(cfg)
}

// LoadFromConfig loads a server config from JSON.
func (s *Service) LoadFromConfig(configJSON string) error {
	s.mu.RLock()
	m := s.manager
	s.mu.RUnlock()
	if m == nil {
		return nil
	}
	var cfg ServerConfig
	if err := jsonUnmarshal(configJSON, &cfg); err != nil {
		return err
	}
	m.AddConfig(cfg)
	return nil
}

// LoadFromFile loads a server config from a file.
func (s *Service) LoadFromFile(path string) error {
	data, err := osReadFile(path)
	if err != nil {
		return err
	}
	return s.LoadFromConfig(string(data))
}

// RemoveServer removes a server configuration.
func (s *Service) RemoveServer(id string) {
	s.mu.RLock()
	m := s.manager
	s.mu.RUnlock()
	if m == nil {
		return
	}
	_ = m.RemoveConfig(id)
}

// Connect connects to all enabled servers.
func (s *Service) Connect() error {
	s.mu.RLock()
	m := s.manager
	s.mu.RUnlock()
	if m == nil {
		return nil
	}
	return m.Connect(context.Background())
}

// ListTools returns all discovered tools.
func (s *Service) ListTools() []Tool {
	s.mu.RLock()
	m := s.manager
	s.mu.RUnlock()
	if m == nil {
		return nil
	}
	return m.ListTools()
}

// ListServerIDs lists the servers with a live session.
func (s *Service) ListServerIDs() []string {
	s.mu.RLock()
	m := s.manager
	s.mu.RUnlock()
	if m == nil {
		return nil
	}
	return m.ListServerIDs()
}

// loadDotEnv loads a development .env (opt-in via ARGUS_LOAD_DOTENV=1) so OAuth
// client ids work locally. The logic lives in Shared.LoadDotEnv so every
// Shared.ConfigValue lookup sees the same values; a packaged app never reads
// a .env from the directory it happens to be launched in.
func loadDotEnv() { Shared.LoadDotEnv() }

func readGoogleCreds() (string, string) {
	loadDotEnv()
	if id := Shared.ConfigValue("ARGUS_GOOGLE_CLIENT_ID"); id != "" {
		return id, Shared.ConfigValue("ARGUS_GOOGLE_CLIENT_SECRET")
	}
	return readCredsFile("google")
}

// readCredsFile looks for a provider's OAuth client in the usual config
// locations, so a build can ship one id per provider without any code change.
func readCredsFile(name string) (string, string) {
	up := strings.ToUpper(name)
	for _, p := range []string{
		"config/" + name + ".json",
		name + ".json",
		"Services/MCP/" + name + ".json",
		".config/argus/" + name + ".json",
		"$HOME/.config/argus/" + name + ".json",
	} {
		if p == "$HOME/.config/argus/"+name+".json" {
			if home, err := os.UserHomeDir(); err == nil {
				p = home + "/.config/argus/" + name + ".json"
			} else {
				continue
			}
		}
		if b, err := os.ReadFile(p); err == nil {
			var m map[string]string
			if json.Unmarshal(b, &m) == nil {
				id := cmp.Or(m["client_id"], m["clientId"], m["ARGUS_"+up+"_CLIENT_ID"], m[name+"_client_id"])
				sec := cmp.Or(m["client_secret"], m["clientSecret"], m["ARGUS_"+up+"_CLIENT_SECRET"], m[name+"_client_secret"])
				if id != "" {
					return id, sec
				}
			}
		}
	}
	return "", ""
}

// clientCreds builds the pre-registered client a provider insists on. Providers
// that support dynamic registration never get here and need no id at all.
func clientCreds(name string) (*oauthex.ClientCredentials, error) {
	var id, sec string
	if name == "github" {
		loadDotEnv()
		id = Shared.ConfigValue("ARGUS_GITHUB_CLIENT_ID")
		sec = Shared.ConfigValue("ARGUS_GITHUB_CLIENT_SECRET")
		if id == "" {
			id, sec = readCredsFile("github")
		}
	} else {
		id, sec = readGoogleCreds()
	}
	if id == "" {
		return nil, fmt.Errorf("%s sign-in is not configured in this build", name)
	}
	c := &oauthex.ClientCredentials{ClientID: id}
	if sec != "" {
		c.ClientSecretAuth = &oauthex.ClientSecretAuth{ClientSecret: sec}
	}
	return c, nil
}

func googleClient() (*oauthex.ClientCredentials, error) { return clientCreds("google") }

func githubClient() (*oauthex.ClientCredentials, error) { return clientCreds("github") }

// ConnectRemote connects a remote MCP server with OAuth.
func (s *Service) ConnectRemote(ctx context.Context, serverID string, interactive bool) error {
	return s.ConnectRemoteConnection(ctx, serverID, "", "", interactive)
}

// ConnectRemoteConnection dials one account with its own persisted OAuth
// session. connectionID is empty only for the legacy server-level API.
func (s *Service) ConnectRemoteConnection(ctx context.Context, serverID, connectionID, expectedEmail string, interactive bool) error {
	s.mu.RLock()
	cat := s.catalog
	m := s.manager
	s.mu.RUnlock()
	if cat == nil {
		cat, _ = Integrations.Default()
	}
	if m == nil {
		return errors.New("mcp not initialized")
	}
	it, ok := cat.Get(serverID)
	if !ok {
		return errors.New("unknown integration")
	}
	spec := RemoteSpec{
		ID: it.ID, ConnectionID: connectionID, Name: it.Name, URL: it.URL,
		Scopes: append([]string(nil), it.OAuth.Scopes...), ProbeTool: it.OAuth.ProbeTool,
		ExpectedEmail: expectedEmail,
	}
	switch it.OAuth.Mode {
	case "google":
		spec.Scopes = append(spec.Scopes, "openid", "email")
		c, err := googleClient()
		if err != nil {
			var ok bool
			c, ok = (tokenStore{serverID: serverID, connectionID: connectionID}).clientCredentials()
			if !ok {
				return err
			}
		}
		spec.Client = c
	case "github":
		// GitHub publishes no registration endpoint, so its client comes from an
		// OAuth App created by whoever builds Argus. That app has one callback URL
		// and GitHub matches the path of it exactly, hence the fixed "/callback";
		// the port is free to vary because GitHub ignores it for loopback redirects.
		c, err := githubClient()
		if err != nil {
			return err
		}
		spec.Client = c
		spec.Callback = "/callback"
	}
	return m.ConnectRemote(ctx, spec, interactive)
}

// ReconnectRemoteConnection retains the database connection and its agent
// bindings, but replaces the invalid OAuth session behind it.
func (s *Service) ReconnectRemoteConnection(ctx context.Context, serverID, connectionID, expectedEmail string) error {
	s.mu.RLock()
	m := s.manager
	s.mu.RUnlock()
	if m == nil {
		return errors.New("mcp not initialized")
	}
	m.CloseConnection(connectionID)
	_ = DeleteSessionToken(serverID, connectionID)
	return s.ConnectRemoteConnection(ctx, serverID, connectionID, expectedEmail, true)
}

// HasStoredSession reports whether an integration was signed in on this
// machine before, whether or not it is connected right now. The connect card
// uses it to rebuild a settled card after a restart; it is preferred over
// IsConnected because restoring sessions happens in the background and a card
// asked for during that window would otherwise disappear.
func (s *Service) HasStoredSession(serverID string) bool {
	return tokenStore{serverID: serverID}.hasSession()
}

func (s *Service) HasStoredConnectionSession(serverID, connectionID string) bool {
	return tokenStore{serverID: serverID, connectionID: connectionID}.hasSession()
}

// DisconnectRemote disconnects a remote server.
func (s *Service) DisconnectRemote(serverID string) {
	s.mu.RLock()
	m := s.manager
	s.mu.RUnlock()
	if m == nil {
		return
	}
	m.DisconnectServer(serverID)
	store := tokenStore{serverID: serverID}
	store.clear()
}

// GetToolsForChat returns MCP tools formatted for the chat system.
func (s *Service) GetToolsForChat() []Tool {
	return s.ListTools()
}

// CallTool calls an MCP tool.
func (s *Service) CallTool(serverID, toolName string, argsJSON string) (string, error) {
	s.mu.RLock()
	m := s.manager
	s.mu.RUnlock()
	if m == nil {
		return "", nil
	}
	var args map[string]interface{}
	if strings.TrimSpace(argsJSON) != "" {
		_ = jsonUnmarshal(argsJSON, &args)
	}
	res, err := m.CallTool(context.Background(), serverID, toolName, args)
	if err != nil {
		return "", err
	}
	if res == nil {
		return "", nil
	}
	// Convert result to JSON string
	b, _ := jsonMarshal(res)
	return string(b), nil
}

func jsonUnmarshal(s string, v interface{}) error {
	return jsonUnmarshalImpl(s, v)
}

func jsonMarshal(v interface{}) ([]byte, error) {
	return jsonMarshalImpl(v)
}
