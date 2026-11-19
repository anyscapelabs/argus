package Storage

import (
	"database/sql"
	"encoding/json"
	"time"
)

// CustomMCPServer is one server the user added themselves rather than picking
// from the bundled catalog: either a remote URL (streamable HTTP) or a local
// command (stdio). Unlike catalog connectors these carry no OAuth accounts —
// the server is the unit of connection, and its tools surface in chat under
// the server's own id.
type CustomMCPServer struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Transport string            `json:"transport"` // http | stdio
	URL       string            `json:"url,omitempty"`
	Command   string            `json:"command,omitempty"`
	Args      []string          `json:"args,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	CreatedAt int64             `json:"created_at"`
}

func CreateCustomMCPServer(db *sql.DB, s CustomMCPServer) error {
	if s.CreatedAt == 0 {
		s.CreatedAt = time.Now().UnixMilli()
	}
	args, err := json.Marshal(s.Args)
	if err != nil {
		return err
	}
	if args == nil {
		args = []byte("[]")
	}
	env, err := json.Marshal(s.Env)
	if err != nil {
		return err
	}
	if env == nil {
		env = []byte("{}")
	}
	_, err = db.Exec(`
		INSERT INTO custom_mcp_servers (id, name, transport, url, command, args_json, env_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		s.ID, s.Name, s.Transport, s.URL, s.Command, string(args), string(env), s.CreatedAt,
	)
	return err
}

func ListCustomMCPServers(db *sql.DB) ([]CustomMCPServer, error) {
	rows, err := db.Query(`
		SELECT id, name, transport, url, command, args_json, env_json, created_at
		FROM custom_mcp_servers ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CustomMCPServer
	for rows.Next() {
		var s CustomMCPServer
		var argsRaw, envRaw string
		if err := rows.Scan(&s.ID, &s.Name, &s.Transport, &s.URL, &s.Command, &argsRaw, &envRaw, &s.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(argsRaw), &s.Args)
		_ = json.Unmarshal([]byte(envRaw), &s.Env)
		out = append(out, s)
	}
	return out, rows.Err()
}

func GetCustomMCPServer(db *sql.DB, id string) (CustomMCPServer, bool, error) {
	var s CustomMCPServer
	var argsRaw, envRaw string
	err := db.QueryRow(`
		SELECT id, name, transport, url, command, args_json, env_json, created_at
		FROM custom_mcp_servers WHERE id = ?`, id).Scan(
		&s.ID, &s.Name, &s.Transport, &s.URL, &s.Command, &argsRaw, &envRaw, &s.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return CustomMCPServer{}, false, nil
	}
	if err != nil {
		return CustomMCPServer{}, false, err
	}
	_ = json.Unmarshal([]byte(argsRaw), &s.Args)
	_ = json.Unmarshal([]byte(envRaw), &s.Env)
	return s, true, nil
}

func DeleteCustomMCPServer(db *sql.DB, id string) error {
	_, err := db.Exec(`DELETE FROM custom_mcp_servers WHERE id = ?`, id)
	return err
}
