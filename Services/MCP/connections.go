package MCP

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// RemoteTool is one tool a connection's server publishes, with its input
// schema left as raw JSON for the caller to hand to the model unchanged.
type RemoteTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
	ReadOnly    bool            `json:"read_only"`
	Destructive *bool           `json:"destructive,omitempty"`
}

// Client is one connection's view of a live server session. Tools and calls
// go through the shared *mcp.ClientSession the Manager already owns for that
// server: tool lists do not differ between two accounts on the same server,
// only the data each call touches does, which is what the connection's own
// token (and the access tier checked before the call) decides.
type Client struct {
	m        *Manager
	serverID string
	conn     *serverConn
}

// Tools lists the session's tools. The schema map is re-marshalled so callers
// receive provider-shaped JSON rather than the SDK's decoded form.
func (c *Client) Tools() []RemoteTool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res, err := c.conn.sess.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		return nil
	}
	out := make([]RemoteTool, 0, len(res.Tools))
	for _, t := range res.Tools {
		schema := json.RawMessage("null")
		if t.InputSchema != nil {
			if b, err := json.Marshal(t.InputSchema); err == nil {
				schema = b
			}
		}
		out = append(out, RemoteTool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: schema,
			ReadOnly:    t.Annotations != nil && t.Annotations.ReadOnlyHint,
			Destructive: destructiveHint(t.Annotations),
		})
	}
	return out
}

// CallTool runs one tool and returns its text content, or an error carrying
// the server's tool-error text.
func (c *Client) CallTool(ctx context.Context, name, argsJSON string) (string, error) {
	callCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	var args map[string]any
	if argsJSON != "" {
		_ = json.Unmarshal([]byte(argsJSON), &args)
	}
	res, err := c.conn.sess.CallTool(callCtx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		return "", err
	}
	if res != nil && res.IsError {
		return "", fmt.Errorf("%s", resultText(res))
	}
	if res == nil {
		return "{}", nil
	}
	return resultText(res), nil
}

// Healthy reports whether the underlying session can still be reached. The
// session is owned by the Manager; a disconnected server invalidates every
// client wrapped around it, so the check goes through the live registry
// rather than the held pointer.
func (c *Client) Healthy() bool {
	if c == nil || c.conn == nil || c.conn.sess == nil {
		return false
	}
	c.m.mu.RLock()
	defer c.m.mu.RUnlock()
	if c.conn.connectionID != "" {
		alive := c.m.connections[c.conn.connectionID]
		return alive == c.conn
	}
	alive := c.m.servers[c.serverID]
	return alive == c.conn
}

// Close releases this connection's reference to the session. The session
// itself stays owned by the Manager until DisconnectServer.
func (c *Client) Close() error {
	if c == nil || c.conn == nil || c.conn.sess == nil {
		return nil
	}
	return c.conn.sess.Close()
}

// GetConnectionClient returns the session authenticated with this connection's
// own token. A sibling account on the same server is intentionally not a
// fallback: doing so would make the UI's account label disagree with the data
// the tool can access.
func (m *Manager) GetConnectionClient(ctx context.Context, serverID, connectionID string) (*Client, error) {
	if connectionID == "" {
		return nil, fmt.Errorf("missing connection_id for server %s", serverID)
	}

	m.mu.RLock()
	sc, ok := m.connections[connectionID]
	m.mu.RUnlock()
	if !ok || sc.sess == nil || sc.serverID != serverID {
		return nil, fmt.Errorf("account %s for %s is not connected; reconnect it before use", connectionID, serverID)
	}
	return &Client{m: m, serverID: serverID, conn: sc}, nil
}

// CloseConnection cancels in-flight calls and closes only this account's MCP
// session. Sibling accounts use independent sessions and remain available.
func (m *Manager) CloseConnection(connectionID string) {
	m.mu.Lock()
	sc := m.connections[connectionID]
	delete(m.connections, connectionID)
	if sc != nil {
		if sc.cancel != nil {
			sc.cancel()
		}
		if sc.sess != nil {
			_ = sc.sess.Close()
		}
	}
	m.refreshToolsLocked()
	m.mu.Unlock()
}

func destructiveHint(a *mcp.ToolAnnotations) *bool {
	if a == nil {
		return nil
	}
	return a.DestructiveHint
}
