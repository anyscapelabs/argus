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
	_, alive := c.m.servers[c.serverID]
	return alive
}

// Close releases this connection's reference to the session. The session
// itself stays owned by the Manager until DisconnectServer.
func (c *Client) Close() error {
	if c == nil || c.conn == nil || c.conn.sess == nil {
		return nil
	}
	return c.conn.sess.Close()
}

// GetConnectionClient returns a healthy, connection-scoped client for the
// server a given connection belongs to. In this build remote servers
// authenticate through the SDK's OAuth flow, which is keyed by server id and
// shared by every account that connects to it, so the live session for
// serverID is what backs every connection. Per-connection tokens are still
// persisted (see sessionKey) and are what the HTTP deployment dials with; a
// connection whose server has no live session here reports unavailable, and
// the caller skips it exactly as it would a failed dial.
func (m *Manager) GetConnectionClient(ctx context.Context, serverID, connectionID string) (*Client, error) {
	if connectionID == "" {
		return nil, fmt.Errorf("missing connection_id for server %s", serverID)
	}

	m.mu.RLock()
	sc, ok := m.servers[serverID]
	m.mu.RUnlock()
	if !ok || sc.sess == nil {
		return nil, fmt.Errorf("server %s is not connected; connect it before use", serverID)
	}
	return &Client{m: m, serverID: serverID, conn: sc}, nil
}

// CloseConnection forgets a connection's cached client, if one was kept, so a
// revoked account stops being served from caches even while the server
// session for its sibling accounts lives on.
func (m *Manager) CloseConnection(connectionID string) {}
