package MCP

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/anyscapelabs/argus/Services/Integrations"
	"github.com/anyscapelabs/argus/Services/Shared"
	"github.com/anyscapelabs/argus/Storage"
)

// CustomServerInput is what the Add-server form submits: either a remote URL
// (streamable HTTP) or a local command (stdio) that speaks MCP over stdin.
type CustomServerInput struct {
	Name      string            `json:"name"`
	Transport string            `json:"transport"` // http | stdio
	URL       string            `json:"url,omitempty"`
	Command   string            `json:"command,omitempty"`
	Args      []string          `json:"args,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
}

var customIDSlug = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(name string) string {
	slug := strings.Trim(customIDSlug.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if slug == "" {
		slug = "server"
	}
	if len(slug) > 40 {
		slug = slug[:40]
	}
	return slug
}

// customServerID builds a unique server id from the display name. Catalog ids
// are reserved first: a custom server must never shadow a bundled connector.
func (s *Service) customServerID(db *sql.DB, cat *Integrations.Catalog, name string) string {
	base := "custom-" + slugify(name)
	taken := func(id string) bool {
		if _, ok := cat.Get(id); ok {
			return true
		}
		if db == nil {
			return false
		}
		var exists int
		_ = db.QueryRow(`SELECT EXISTS(SELECT 1 FROM custom_mcp_servers WHERE id = ?)`, id).Scan(&exists)
		return exists == 1
	}
	if !taken(base) {
		return base
	}
	for n := 2; ; n++ {
		id := fmt.Sprintf("%s-%d", base, n)
		if !taken(id) {
			return id
		}
	}
}

// AddCustomServer validates, persists and connects a user-added MCP server.
// The returned row echoes what was stored so the caller can show it without
// re-reading the list.
func (s *Service) AddCustomServer(in CustomServerInput) (Storage.CustomMCPServer, error) {
	s.mu.RLock()
	m := s.manager
	cat := s.catalog
	s.mu.RUnlock()
	if m == nil || s.Core == nil || s.Core.DB == nil {
		return Storage.CustomMCPServer{}, errors.New("mcp not initialized")
	}
	if cat == nil {
		c, err := Integrations.Default()
		if err != nil {
			return Storage.CustomMCPServer{}, err
		}
		cat = c
		s.mu.Lock()
		s.catalog = cat
		s.mu.Unlock()
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return Storage.CustomMCPServer{}, errors.New("a name is required")
	}
	in.Transport = strings.ToLower(strings.TrimSpace(in.Transport))
	db := s.Core.DB.SQL()

	var stored Storage.CustomMCPServer
	switch in.Transport {
	case "http":
		url := strings.TrimSpace(in.URL)
		if url == "" {
			return Storage.CustomMCPServer{}, errors.New("a server URL is required")
		}
		if !strings.HasPrefix(url, "https://") && !strings.HasPrefix(url, "http://localhost") && !strings.HasPrefix(url, "http://127.0.0.1") {
			return Storage.CustomMCPServer{}, errors.New("the URL must start with https:// (or http:// on localhost)")
		}
		stored = Storage.CustomMCPServer{ID: s.customServerID(db, cat, name), Name: name, Transport: "http", URL: url}
	case "stdio":
		command := strings.TrimSpace(in.Command)
		if command == "" {
			return Storage.CustomMCPServer{}, errors.New("a command is required")
		}
		if err := validateCommand(command); err != nil {
			return Storage.CustomMCPServer{}, err
		}
		args := append([]string(nil), in.Args...)
		env := map[string]string{}
		for k, v := range in.Env {
			if k = strings.TrimSpace(k); k != "" {
				env[k] = v
			}
		}
		stored = Storage.CustomMCPServer{ID: s.customServerID(db, cat, name), Name: name, Transport: "stdio", Command: command, Args: args, Env: env}
	default:
		return Storage.CustomMCPServer{}, errors.New("transport must be http or stdio")
	}

	if err := Storage.CreateCustomMCPServer(db, stored); err != nil {
		return Storage.CustomMCPServer{}, err
	}
	if err := s.connectCustom(stored); err != nil {
		// The registration is kept: a server that is down right now should
		// still be listed, and the session comes back when it is reachable.
		// The error is reported so the form can show it either way.
		return stored, err
	}
	s.Core.EmitEvent(Shared.EventIntegrationsChanged, nil)
	return stored, nil
}

// connectCustom dials one stored custom server. A 30s deadline bounds a stdio
// binary that never completes the handshake; remote servers get the same
// bound because nobody is sitting in a browser for this connect.
func (s *Service) connectCustom(stored Storage.CustomMCPServer) error {
	s.mu.RLock()
	m := s.manager
	s.mu.RUnlock()
	if m == nil {
		return errors.New("mcp not initialized")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	switch stored.Transport {
	case "http":
		return m.ConnectPlainRemote(ctx, stored.ID, stored.Name, stored.URL)
	case "stdio":
		m.AddConfig(ServerConfig{
			ID: stored.ID, Name: stored.Name, Enabled: true, Type: "stdio",
			Command: stored.Command, Args: stored.Args, Env: stored.Env,
		})
		return m.Connect(ctx)
	}
	return fmt.Errorf("unknown transport %q", stored.Transport)
}

// ListCustomServers returns every user-added server.
func (s *Service) ListCustomServers() []Storage.CustomMCPServer {
	if s.Core == nil || s.Core.DB == nil {
		return nil
	}
	out, err := Storage.ListCustomMCPServers(s.Core.DB.SQL())
	if err != nil {
		return nil
	}
	return out
}

// IsCustomServerConnected reports whether a user-added server holds a session.
func (s *Service) IsCustomServerConnected(id string) bool {
	s.mu.RLock()
	m := s.manager
	s.mu.RUnlock()
	if m == nil {
		return false
	}
	return m.IsConnected(id)
}

// RemoveCustomServer forgets a user-added server: the session is closed, the
// persisted row is deleted, and its tools leave the chat on the next refresh.
func (s *Service) RemoveCustomServer(id string) error {
	if s.Core == nil || s.Core.DB == nil {
		return errors.New("mcp not initialized")
	}
	db := s.Core.DB.SQL()
	_, found, err := Storage.GetCustomMCPServer(db, id)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("server %q is not registered", id)
	}
	s.mu.RLock()
	m := s.manager
	s.mu.RUnlock()
	if m != nil {
		m.DisconnectServer(id)
		_ = m.RemoveConfig(id)
	}
	if err := Storage.DeleteCustomMCPServer(db, id); err != nil {
		return err
	}
	s.Core.EmitEvent(Shared.EventIntegrationsChanged, nil)
	return nil
}

// restoreCustomServers re-registers the servers persisted by earlier runs.
// Failures are silent: a server that is offline today is still listed, and the
// session comes back when it is reachable again.
func (s *Service) restoreCustomServers() {
	if s.Core == nil || s.Core.DB == nil {
		return
	}
	stored, err := Storage.ListCustomMCPServers(s.Core.DB.SQL())
	if err != nil {
		return
	}
	for _, server := range stored {
		_ = s.connectCustom(server)
	}
}
