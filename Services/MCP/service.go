package MCP

import (
	"context"
	"strings"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/anyscapelabs/argus/Services/Shared"
)

// Service exposes MCP management to the frontend.
type Service struct {
	Core *Shared.Core

	mu      sync.RWMutex
	manager *Manager
}

// ServiceStartup initializes the MCP manager.
func (s *Service) ServiceStartup(ctx context.Context, options application.ServiceOptions) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.manager = New()
	// Load saved configs if needed in future
	return nil
}

// ServiceShutdown disconnects all servers.
func (s *Service) ServiceShutdown(ctx context.Context) error {
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
