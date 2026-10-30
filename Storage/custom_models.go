package Storage

import (
	"database/sql"
	"fmt"
	"time"
)

type CustomModel struct {
	ProviderID    string
	ModelID       string
	DisplayName   string
	ContextWindow int64
	MaxOutput     int64
	Tools         bool
	Vision        bool
	Reasoning     bool
	PriceInput    float64
	PriceOutput   float64
	PriceKnown    bool
	CreatedAt     time.Time
}

func (cm CustomModel) Ref() string {
	return cm.ProviderID + "/" + cm.ModelID
}

func (db *DB) SaveCustomModel(cm *CustomModel) error {
	var pin, pout any
	if cm.PriceKnown {
		pin, pout = cm.PriceInput, cm.PriceOutput
	}
	_, err := db.conn.Exec(
		`INSERT INTO custom_models
		 (provider_id, model_id, display_name, context_window, max_output,
		  tools, vision, reasoning, price_input, price_output, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(provider_id, model_id) DO UPDATE SET
		   display_name = excluded.display_name,
		   context_window = excluded.context_window,
		   max_output = excluded.max_output,
		   tools = excluded.tools,
		   vision = excluded.vision,
		   reasoning = excluded.reasoning,
		   price_input = excluded.price_input,
		   price_output = excluded.price_output`,
		cm.ProviderID, cm.ModelID, cm.DisplayName,
		nullInt(cm.ContextWindow), nullInt(cm.MaxOutput),
		b2i(cm.Tools), b2i(cm.Vision), b2i(cm.Reasoning),
		pin, pout, nowRFC3339(),
	)
	if err != nil {
		return fmt.Errorf("storage: custom model save failed: %w", err)
	}
	return nil
}

func (db *DB) DeleteCustomModel(providerID, modelID string) error {
	if _, err := db.conn.Exec(
		`DELETE FROM custom_models WHERE provider_id = ? AND model_id = ?`,
		providerID, modelID,
	); err != nil {
		return fmt.Errorf("storage: custom model delete failed: %w", err)
	}
	return nil
}

func (db *DB) ListCustomModels(providerID string) ([]CustomModel, error) {
	rows, err := db.conn.Query(
		`SELECT provider_id, model_id, display_name, context_window, max_output,
		        tools, vision, reasoning, price_input, price_output, created_at
		 FROM custom_models WHERE provider_id = ? ORDER BY created_at, model_id`, providerID)
	if err != nil {
		return nil, fmt.Errorf("storage: custom model listing failed: %w", err)
	}
	return scanCustomRows(rows)
}

func (db *DB) ListAllCustomModels() ([]CustomModel, error) {
	rows, err := db.conn.Query(
		`SELECT provider_id, model_id, display_name, context_window, max_output,
		        tools, vision, reasoning, price_input, price_output, created_at
		 FROM custom_models ORDER BY created_at, provider_id, model_id`)
	if err != nil {
		return nil, fmt.Errorf("storage: custom model listing failed: %w", err)
	}
	return scanCustomRows(rows)
}

func (db *DB) HasCustomModel(providerID, modelID string) (bool, error) {
	var one int
	err := db.conn.QueryRow(
		`SELECT 1 FROM custom_models WHERE provider_id = ? AND model_id = ?`,
		providerID, modelID,
	).Scan(&one)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("storage: custom model lookup failed: %w", err)
	}
	return true, nil
}

func scanCustomRows(rows *sql.Rows) ([]CustomModel, error) {
	defer rows.Close()
	var out []CustomModel
	for rows.Next() {
		var cm CustomModel
		var ctx, maxo sql.NullInt64
		var tools, vision, reasoning int64
		var pin, pout sql.NullFloat64
		var created string
		if err := rows.Scan(&cm.ProviderID, &cm.ModelID, &cm.DisplayName, &ctx, &maxo,
			&tools, &vision, &reasoning, &pin, &pout, &created); err != nil {
			return nil, fmt.Errorf("storage: custom model scan failed: %w", err)
		}
		cm.ContextWindow = ctx.Int64
		cm.MaxOutput = maxo.Int64
		cm.Tools = tools != 0
		cm.Vision = vision != 0
		cm.Reasoning = reasoning != 0
		cm.PriceKnown = pin.Valid && pout.Valid
		cm.PriceInput = pin.Float64
		cm.PriceOutput = pout.Float64
		cm.CreatedAt, _ = time.Parse(time.RFC3339, created)
		out = append(out, cm)
	}
	return out, rows.Err()
}
