package MCP

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ServerConfig describes how to connect to an MCP server.
type ServerConfig struct {
	ID      string            `json:"id"`
	Name    string            `json:"name"`
	Enabled bool              `json:"enabled"`
	Type    string            `json:"type"` // "stdio"
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env"`
}

// Tool represents a discovered MCP tool.
type Tool struct {
	ServerID    string                 `json:"server_id"`
	ServerName  string                 `json:"server_name"`
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"input_schema"`
}

// Manager manages MCP server connections and discovered tools.
type Manager struct {
	mu      sync.RWMutex
	servers map[string]*serverConn
	configs map[string]ServerConfig
	tools   []Tool
}

type serverConn struct {
	cfg    ServerConfig
	client *mcp.Client
	sess   *mcp.ClientSession
	cancel context.CancelFunc
}

// New creates a new MCP manager.
func New() *Manager {
	return &Manager{
		servers: make(map[string]*serverConn),
		configs: make(map[string]ServerConfig),
	}
}

// LoadConfigs loads server configurations.
func (m *Manager) LoadConfigs(cfgs []ServerConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.configs = make(map[string]ServerConfig)
	for _, c := range cfgs {
		m.configs[c.ID] = c
	}
}

// ListConfigs returns all server configurations.
func (m *Manager) ListConfigs() []ServerConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]ServerConfig, 0, len(m.configs))
	for _, c := range m.configs {
		out = append(out, c)
	}
	return out
}

// AddConfig adds or updates a server configuration.
func (m *Manager) AddConfig(cfg ServerConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if cfg.ID == "" {
		cfg.ID = strings.ReplaceAll(strings.ToLower(cfg.Name), " ", "-")
	}
	m.configs[cfg.ID] = cfg
}

// RemoveConfig removes a server configuration and disconnects if connected.
func (m *Manager) RemoveConfig(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if sc, ok := m.servers[id]; ok {
		if sc.cancel != nil {
			sc.cancel()
		}
		if sc.sess != nil {
			sc.sess.Close()
		}
		delete(m.servers, id)
	}
	delete(m.configs, id)
	m.refreshToolsLocked()
	return nil
}

// Connect connects to enabled servers.
func (m *Manager) Connect(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var firstErr error
	for id, cfg := range m.configs {
		if !cfg.Enabled {
			if sc, ok := m.servers[id]; ok {
				if sc.cancel != nil {
					sc.cancel()
				}
				if sc.sess != nil {
					sc.sess.Close()
				}
				delete(m.servers, id)
			}
			continue
		}
		if _, ok := m.servers[id]; ok {
			continue
		}
		if err := m.connectLocked(ctx, cfg); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
	}
	m.refreshToolsLocked()
	return firstErr
}

// connectLocked connects to a single server (caller must hold lock).
func (m *Manager) connectLocked(ctx context.Context, cfg ServerConfig) error {
	if cfg.Type == "" {
		cfg.Type = "stdio"
	}
	if cfg.Type != "stdio" {
		return fmt.Errorf("unsupported mcp transport: %s", cfg.Type)
	}
	if strings.TrimSpace(cfg.Command) == "" {
		return errors.New("mcp command required")
	}

	c := mcp.NewClient(&mcp.Implementation{Name: "argus", Version: "0.1.0"}, nil)

	var env []string
	for k, v := range cfg.Env {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}

	transport := &mcp.CommandTransport{
		Command: execCmd(cfg.Command, cfg.Args, env),
	}

	sessCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	sess, err := c.Connect(sessCtx, transport, nil)
	if err != nil {
		return err
	}

	connCtx, connCancel := context.WithCancel(context.Background())
	sc := &serverConn{
		cfg:    cfg,
		client: c,
		sess:   sess,
		cancel: connCancel,
	}
	m.servers[cfg.ID] = sc

	_ = connCtx
	return nil
}

// ListTools returns all discovered tools from connected servers.
func (m *Manager) ListTools() []Tool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Tool, len(m.tools))
	copy(out, m.tools)
	return out
}

// ListServerIDs lists the configured servers holding a live session. Tool
// schemas are attached lazily, so the asker needs the ids without the
// schemas themselves.
func (m *Manager) ListServerIDs() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]string, 0, len(m.servers))
	for id, sc := range m.servers {
		if sc.sess != nil {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

// refreshToolsLocked refreshes tool list (caller must hold lock).
func (m *Manager) refreshToolsLocked() {
	var tools []Tool
	for id, sc := range m.servers {
		if sc.sess == nil {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		res, err := sc.sess.ListTools(ctx, &mcp.ListToolsParams{})
		cancel()
		if err != nil {
			continue
		}
		for _, t := range res.Tools {
			var schema map[string]interface{}
			if t.InputSchema != nil {
				b, _ := json.Marshal(t.InputSchema)
				_ = json.Unmarshal(b, &schema)
			}
			tools = append(tools, Tool{
				ServerID:    id,
				ServerName:  sc.cfg.Name,
				Name:        t.Name,
				Description: t.Description,
				InputSchema: schema,
			})
		}
	}
	m.tools = tools
}

// CallTool calls a tool on the appropriate MCP server.
func (m *Manager) CallTool(ctx context.Context, serverID, toolName string, args map[string]interface{}) (*mcp.CallToolResult, error) {
	m.mu.RLock()
	sc, ok := m.servers[serverID]
	m.mu.RUnlock()
	if !ok {
		return nil, errors.New("mcp server not connected")
	}
	callCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	return sc.sess.CallTool(callCtx, &mcp.CallToolParams{
		Name:      toolName,
		Arguments: args,
	})
}

// Disconnect disconnects all servers.
func (m *Manager) Disconnect() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, sc := range m.servers {
		if sc.cancel != nil {
			sc.cancel()
		}
		if sc.sess != nil {
			sc.sess.Close()
		}
	}
	m.servers = make(map[string]*serverConn)
	m.tools = nil
}
