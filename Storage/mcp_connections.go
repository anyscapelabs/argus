package Storage

import (
	"database/sql"
	"time"
)

// MCPConnection is one signed-in account of an integration server. A server
// and a connection are deliberately separate: the same Gmail server can be
// connected twice (a support inbox and a legal inbox), each with its own
// token, label and expected account, and each bindable to different agents.
type MCPConnection struct {
	ID            string `json:"id"`
	ServerID      string `json:"server_id"`
	Label         string `json:"label"`
	AccountEmail  string `json:"account_email"`
	ExpectedEmail string `json:"expected_email"`
	Scopes        string `json:"scopes"`
	Status        string `json:"status"`
	CreatedAt     int64  `json:"created_at"`
}

// AgentConnection binds one agent to one connection with a coarse access tier:
// read-only agents may query, draft-tier agents may compose but not send, and
// full-tier agents may do everything the server publishes.
type AgentConnection struct {
	AgentID      string `json:"agent_id"`
	ConnectionID string `json:"connection_id"`
	ServerID     string `json:"server_id"`
	Label        string `json:"label"`
	AccountEmail string `json:"account_email"`
	AccessLevel  string `json:"access_level"` // read | draft | full
	Status       string `json:"status"`
}

func CreateMCPConnection(db *sql.DB, conn MCPConnection) error {
	if conn.CreatedAt == 0 {
		conn.CreatedAt = time.Now().UnixMilli()
	}
	if conn.Status == "" {
		conn.Status = "connected"
	}
	_, err := db.Exec(`
		INSERT INTO mcp_connections (
			id, server_id, label, account_email, expected_email, scopes, status, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		conn.ID, conn.ServerID, conn.Label, conn.AccountEmail,
		conn.ExpectedEmail, conn.Scopes, conn.Status, conn.CreatedAt,
	)
	return err
}

func ListMCPConnections(db *sql.DB) ([]MCPConnection, error) {
	rows, err := db.Query(`
		SELECT id, server_id, label, account_email, expected_email, scopes, status, created_at
		FROM mcp_connections
		ORDER BY server_id ASC, created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []MCPConnection
	for rows.Next() {
		var c MCPConnection
		if err := rows.Scan(
			&c.ID, &c.ServerID, &c.Label, &c.AccountEmail,
			&c.ExpectedEmail, &c.Scopes, &c.Status, &c.CreatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// DeleteMCPConnection removes the connection row; agent_connections rows that
// reference it cascade away with it.
func DeleteMCPConnection(db *sql.DB, connectionID string) error {
	_, err := db.Exec(`DELETE FROM mcp_connections WHERE id = ?`, connectionID)
	return err
}

func UpsertAgentConnection(db *sql.DB, agentID, connectionID, accessLevel string) error {
	if accessLevel == "" {
		accessLevel = "draft"
	}
	_, err := db.Exec(`
		INSERT INTO agent_connections (agent_id, connection_id, access_level, created_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(agent_id, connection_id) DO UPDATE SET access_level = excluded.access_level`,
		agentID, connectionID, accessLevel, time.Now().UnixMilli(),
	)
	return err
}

func RemoveAgentConnection(db *sql.DB, agentID, connectionID string) error {
	_, err := db.Exec(
		`DELETE FROM agent_connections WHERE agent_id = ? AND connection_id = ?`,
		agentID, connectionID,
	)
	return err
}

// ListAgentConnectionsForAgent lists every connection the agent is bound to,
// with the server metadata the tool projection prefixes and descriptions from.
func ListAgentConnectionsForAgent(db *sql.DB, agentID string) ([]AgentConnection, error) {
	rows, err := db.Query(`
		SELECT ac.agent_id, ac.connection_id, mc.server_id, mc.label,
		       mc.account_email, ac.access_level, mc.status
		FROM agent_connections ac
		JOIN mcp_connections mc ON mc.id = ac.connection_id
		WHERE ac.agent_id = ?
		ORDER BY mc.server_id ASC, mc.created_at ASC`, agentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []AgentConnection
	for rows.Next() {
		var item AgentConnection
		if err := rows.Scan(
			&item.AgentID, &item.ConnectionID, &item.ServerID, &item.Label,
			&item.AccountEmail, &item.AccessLevel, &item.Status,
		); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// ConnectionBotBinding is one agent's stake in one connection, as the
// integrations catalog shows it.
type ConnectionBotBinding struct {
	ConnectionID string `json:"connection_id"`
	AgentID      string `json:"agent_id"`
	Handle       string `json:"handle"`
	AgentName    string `json:"agent_name"`
	AccessLevel  string `json:"access_level"`
}

func ListAllConnectionBindings(db *sql.DB) ([]ConnectionBotBinding, error) {
	rows, err := db.Query(`
		SELECT ac.connection_id, ac.agent_id, ap.handle, ap.agent_name, ac.access_level
		FROM agent_connections ac
		JOIN agent_profiles ap ON ap.id = ac.agent_id
		ORDER BY ap.agent_name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ConnectionBotBinding
	for rows.Next() {
		var b ConnectionBotBinding
		if err := rows.Scan(&b.ConnectionID, &b.AgentID, &b.Handle, &b.AgentName, &b.AccessLevel); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}
