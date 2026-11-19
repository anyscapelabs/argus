package Storage

import (
	"database/sql"
	"time"
)

// Action is one side effect the agent wants on the world (today: email
// sends). Status moves forward only: requested → running → succeeded |
// failed | denied | unknown. unknown is terminal, not a retry prompt.
type Action struct {
	ID           string `json:"id"`
	AgentID      string `json:"agent_id"`
	AgentName    string `json:"agent_name"`
	Kind         string `json:"kind"`
	Provider     string `json:"provider"`
	ConnectionID string `json:"connection_id"`
	RequestJSON  string `json:"request_json"`
	Status       string `json:"status"`
	OpID         string `json:"op_id"`
	CreatedAt    int64  `json:"created_at"`
	UpdatedAt    int64  `json:"updated_at"`
}

// ActionReceipt is the provider's own confirmation for a completed action.
type ActionReceipt struct {
	ActionID    string `json:"action_id"`
	MessageID   string `json:"message_id"`
	ThreadID    string `json:"thread_id"`
	DraftID     string `json:"draft_id"`
	ReceiptJSON string `json:"receipt_json"`
	RecordedAt  int64  `json:"recorded_at"`
}

// ActionReconciliation records one evidence pass for an action left unknown.
type ActionReconciliation struct {
	ActionID   string `json:"action_id"`
	Method     string `json:"method"` // message_search | draft_gone
	Found      bool   `json:"found"`
	Verdict    string `json:"verdict"` // succeeded | unknown
	Detail     string `json:"detail"`
	RecordedAt int64  `json:"recorded_at"`
}

func CreateAction(db *sql.DB, a Action) error {
	now := time.Now().UnixMilli()
	if a.CreatedAt == 0 {
		a.CreatedAt = now
	}
	if a.UpdatedAt == 0 {
		a.UpdatedAt = now
	}
	_, err := db.Exec(`INSERT INTO actions (
			id, agent_id, agent_name, kind, provider, connection_id, request_json, status, op_id, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.ID, a.AgentID, a.AgentName, a.Kind, a.Provider, a.ConnectionID,
		a.RequestJSON, a.Status, a.OpID, a.CreatedAt, a.UpdatedAt,
	)
	return err
}

func GetAction(db *sql.DB, actionID string) (Action, bool, error) {
	var a Action
	err := db.QueryRow(`SELECT id, agent_id, agent_name, kind, provider, connection_id,
			request_json, status, op_id, created_at, updated_at
		FROM actions WHERE id = ?`, actionID).Scan(
		&a.ID, &a.AgentID, &a.AgentName, &a.Kind, &a.Provider, &a.ConnectionID,
		&a.RequestJSON, &a.Status, &a.OpID, &a.CreatedAt, &a.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return Action{}, false, nil
	}
	return a, err == nil, err
}

// FindActionByOpID resolves the idempotency lookup: same op_id, kind and
// provider means this exact operation was already attempted.
func FindActionByOpID(db *sql.DB, opID, kind, provider string) (Action, bool, error) {
	var a Action
	err := db.QueryRow(`SELECT id, agent_id, agent_name, kind, provider, connection_id,
			request_json, status, op_id, created_at, updated_at
		FROM actions WHERE op_id = ? AND kind = ? AND provider = ?`, opID, kind, provider).Scan(
		&a.ID, &a.AgentID, &a.AgentName, &a.Kind, &a.Provider, &a.ConnectionID,
		&a.RequestJSON, &a.Status, &a.OpID, &a.CreatedAt, &a.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return Action{}, false, nil
	}
	return a, err == nil, err
}

// SetActionStatus moves an action forward; terminal states are final.
func SetActionStatus(db *sql.DB, actionID, status string) error {
	_, err := db.Exec(`UPDATE actions SET status = ?, updated_at = ?
		WHERE id = ? AND status NOT IN ('succeeded', 'failed', 'denied', 'unknown')
		AND NOT (status = 'running' AND ? IN ('denied', 'running'))`,
		status, time.Now().UnixMilli(), actionID, status)
	return err
}

func RecordActionReceipt(db *sql.DB, r ActionReceipt) error {
	if r.RecordedAt == 0 {
		r.RecordedAt = time.Now().UnixMilli()
	}
	_, err := db.Exec(`INSERT INTO action_receipts (
			action_id, message_id, thread_id, draft_id, receipt_json, recorded_at
		) VALUES (?, ?, ?, ?, ?, ?)`,
		r.ActionID, r.MessageID, r.ThreadID, r.DraftID, r.ReceiptJSON, r.RecordedAt,
	)
	return err
}

func GetActionReceipt(db *sql.DB, actionID string) (ActionReceipt, bool, error) {
	var r ActionReceipt
	err := db.QueryRow(`SELECT action_id, message_id, thread_id, draft_id, receipt_json, recorded_at
		FROM action_receipts WHERE action_id = ?`, actionID).Scan(
		&r.ActionID, &r.MessageID, &r.ThreadID, &r.DraftID, &r.ReceiptJSON, &r.RecordedAt,
	)
	if err == sql.ErrNoRows {
		return ActionReceipt{}, false, nil
	}
	return r, err == nil, err
}

func RecordActionReconciliation(db *sql.DB, r ActionReconciliation) error {
	if r.RecordedAt == 0 {
		r.RecordedAt = time.Now().UnixMilli()
	}
	_, err := db.Exec(`INSERT INTO action_reconciliations (
			action_id, method, found, verdict, detail, recorded_at
		) VALUES (?, ?, ?, ?, ?, ?)`,
		r.ActionID, r.Method, r.Found, r.Verdict, r.Detail, r.RecordedAt,
	)
	return err
}
