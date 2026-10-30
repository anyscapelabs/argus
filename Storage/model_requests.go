package Storage

import (
	"database/sql"
	"fmt"
)

type ModelRequest struct {
	ID                 string
	AgentID            string
	MessageID          string
	Attempt            int
	ProviderID         string
	ModelID            string
	ModelRef           string
	AdapterFamily      string
	BaseHost           string
	Slot               string
	ReasoningLevel     string
	Temperature        *float64
	MaxOutputTokens    int64
	SystemPrompt       string
	ContextThroughSeq  int
	ContextMessageCount int
	Status             string
	HTTPStatus         int
	ProviderRequestID  string
	StopReason         string
	ErrorCode          string
	ErrorMessage       string
	ErrorRetryable     bool
	ErrorRaw           string
	UsageSource        string
	InputTokens        *int64
	CacheReadTokens    *int64
	CacheWriteTokens   *int64
	OutputTokens       *int64
	ReasoningTokens    *int64
	UsageRawJSON       string
	CostUSD            *float64
	PriceJSON          string
	StartedAt          int64
	FirstByteAt        int64
	FirstTokenAt       int64
	CompletedAt        int64
}

func (db *DB) InsertModelRequest(r ModelRequest) error {
	if r.ID == "" {
		return fmt.Errorf("storage: request id is required")
	}
	if r.Attempt == 0 {
		r.Attempt = 1
	}
	if r.UsageSource == "" {
		r.UsageSource = "missing"
	}
	if r.StartedAt == 0 {
		r.StartedAt = nowUnixMs()
	}
	_, err := db.conn.Exec(
		`INSERT INTO model_requests (
			id, agent_id, message_id, attempt, provider_id, model_id, model_ref, adapter_family, base_host, slot, reasoning_level, temperature, max_output_tokens, system_prompt, context_through_seq, context_message_count, status, http_status, provider_request_id, stop_reason, error_code, error_message, error_retryable, error_raw, usage_source, input_tokens, cache_read_tokens, cache_write_tokens, output_tokens, reasoning_tokens, usage_raw_json, cost_usd, price_json, started_at, first_byte_at, first_token_at, completed_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ID, r.AgentID, r.MessageID, r.Attempt, r.ProviderID, r.ModelID, r.ModelRef, r.AdapterFamily, r.BaseHost, nullString(r.Slot), nullString(r.ReasoningLevel), r.Temperature, nullInt64(r.MaxOutputTokens), r.SystemPrompt, r.ContextThroughSeq, r.ContextMessageCount, r.Status, nullInt64(int64(r.HTTPStatus)), nullString(r.ProviderRequestID), nullString(r.StopReason), nullString(r.ErrorCode), nullString(r.ErrorMessage), boolToInt(r.ErrorRetryable), nullString(r.ErrorRaw), r.UsageSource, r.InputTokens, r.CacheReadTokens, r.CacheWriteTokens, r.OutputTokens, r.ReasoningTokens, nullString(r.UsageRawJSON), r.CostUSD, nullString(r.PriceJSON), r.StartedAt, nullInt64(r.FirstByteAt), nullInt64(r.FirstTokenAt), nullInt64(r.CompletedAt),
	)
	if err != nil {
		return fmt.Errorf("storage: request insert failed: %w", err)
	}
	return nil
}

func (db *DB) FinalizeModelRequest(r ModelRequest) error {
	if r.CompletedAt == 0 {
		r.CompletedAt = nowUnixMs()
	}
	_, err := db.conn.Exec(
		`UPDATE model_requests SET
			status = ?, http_status = ?, provider_request_id = ?, stop_reason = ?, error_code = ?, error_message = ?, error_retryable = ?, error_raw = ?,
			usage_source = ?, input_tokens = ?, cache_read_tokens = ?, cache_write_tokens = ?, output_tokens = ?, reasoning_tokens = ?, usage_raw_json = ?, cost_usd = ?, price_json = ?,
			first_byte_at = ?, first_token_at = ?, completed_at = ?
		 WHERE id = ?`,
		r.Status, nullInt64(int64(r.HTTPStatus)), nullString(r.ProviderRequestID), nullString(r.StopReason), nullString(r.ErrorCode), nullString(r.ErrorMessage), boolToInt(r.ErrorRetryable), nullString(r.ErrorRaw),
		r.UsageSource, r.InputTokens, r.CacheReadTokens, r.CacheWriteTokens, r.OutputTokens, r.ReasoningTokens, nullString(r.UsageRawJSON), r.CostUSD, nullString(r.PriceJSON),
		nullInt64(r.FirstByteAt), nullInt64(r.FirstTokenAt), r.CompletedAt, r.ID,
	)
	if err != nil {
		return fmt.Errorf("storage: request finalize failed: %w", err)
	}
	return nil
}

func (db *DB) GetModelRequest(id string) (ModelRequest, bool, error) {
	row := db.conn.QueryRow(`SELECT id, agent_id, message_id, attempt, provider_id, model_id, model_ref, adapter_family, base_host, slot, reasoning_level, temperature, max_output_tokens, system_prompt, context_through_seq, context_message_count, status, http_status, provider_request_id, stop_reason, error_code, error_message, error_retryable, error_raw, usage_source, input_tokens, cache_read_tokens, cache_write_tokens, output_tokens, reasoning_tokens, usage_raw_json, cost_usd, price_json, started_at, first_byte_at, first_token_at, completed_at FROM model_requests WHERE id = ?`, id)
	return scanModelRequest(row)
}

func (db *DB) ListModelRequests(agentID string, limit int) ([]ModelRequest, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := db.conn.Query(`SELECT id, agent_id, message_id, attempt, provider_id, model_id, model_ref, adapter_family, base_host, slot, reasoning_level, temperature, max_output_tokens, system_prompt, context_through_seq, context_message_count, status, http_status, provider_request_id, stop_reason, error_code, error_message, error_retryable, error_raw, usage_source, input_tokens, cache_read_tokens, cache_write_tokens, output_tokens, reasoning_tokens, usage_raw_json, cost_usd, price_json, started_at, first_byte_at, first_token_at, completed_at FROM model_requests WHERE agent_id = ? ORDER BY started_at DESC LIMIT ?`, agentID, limit)
	if err != nil {
		return nil, fmt.Errorf("storage: request listing failed: %w", err)
	}
	defer rows.Close()
	var out []ModelRequest
	for rows.Next() {
		r, err := scanModelRequestRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func scanModelRequest(row *sql.Row) (ModelRequest, bool, error) {
	var r ModelRequest
	var slot, reasoningLevel, providerReqID, stopReason, errorCode, errorMessage, errorRaw, usageRaw, priceJSON sql.NullString
	var httpStatus sql.NullInt64
	var temp sql.NullFloat64
	var maxOut sql.NullInt64
	var firstByte, firstToken, completed sql.NullInt64
	var input, cacheRead, cacheWrite, output, reasoning *int64
	var cost *float64
	var retryable sql.NullInt64
	err := row.Scan(
		&r.ID, &r.AgentID, &r.MessageID, &r.Attempt, &r.ProviderID, &r.ModelID, &r.ModelRef, &r.AdapterFamily, &r.BaseHost, &slot, &reasoningLevel, &temp, &maxOut, &r.SystemPrompt, &r.ContextThroughSeq, &r.ContextMessageCount, &r.Status, &httpStatus, &providerReqID, &stopReason, &errorCode, &errorMessage, &retryable, &errorRaw, &r.UsageSource, &input, &cacheRead, &cacheWrite, &output, &reasoning, &usageRaw, &cost, &priceJSON, &r.StartedAt, &firstByte, &firstToken, &completed,
	)
	if err == sql.ErrNoRows {
		return ModelRequest{}, false, nil
	}
	if err != nil {
		return ModelRequest{}, false, fmt.Errorf("storage: request scan failed: %w", err)
	}
	applyNulls(&r, slot, reasoningLevel, providerReqID, stopReason, errorCode, errorMessage, errorRaw, usageRaw, priceJSON, httpStatus, temp, maxOut, firstByte, firstToken, completed, input, cacheRead, cacheWrite, output, reasoning, cost, retryable)
	return r, true, nil
}

func scanModelRequestRows(rows *sql.Rows) (ModelRequest, error) {
	var r ModelRequest
	var slot, reasoningLevel, providerReqID, stopReason, errorCode, errorMessage, errorRaw, usageRaw, priceJSON sql.NullString
	var httpStatus sql.NullInt64
	var temp sql.NullFloat64
	var maxOut sql.NullInt64
	var firstByte, firstToken, completed sql.NullInt64
	var input, cacheRead, cacheWrite, output, reasoning *int64
	var cost *float64
	var retryable sql.NullInt64
	err := rows.Scan(
		&r.ID, &r.AgentID, &r.MessageID, &r.Attempt, &r.ProviderID, &r.ModelID, &r.ModelRef, &r.AdapterFamily, &r.BaseHost, &slot, &reasoningLevel, &temp, &maxOut, &r.SystemPrompt, &r.ContextThroughSeq, &r.ContextMessageCount, &r.Status, &httpStatus, &providerReqID, &stopReason, &errorCode, &errorMessage, &retryable, &errorRaw, &r.UsageSource, &input, &cacheRead, &cacheWrite, &output, &reasoning, &usageRaw, &cost, &priceJSON, &r.StartedAt, &firstByte, &firstToken, &completed,
	)
	if err != nil {
		return ModelRequest{}, fmt.Errorf("storage: request scan failed: %w", err)
	}
	applyNulls(&r, slot, reasoningLevel, providerReqID, stopReason, errorCode, errorMessage, errorRaw, usageRaw, priceJSON, httpStatus, temp, maxOut, firstByte, firstToken, completed, input, cacheRead, cacheWrite, output, reasoning, cost, retryable)
	return r, nil
}

func applyNulls(r *ModelRequest, slot, reasoningLevel, providerReqID, stopReason, errorCode, errorMessage, errorRaw, usageRaw, priceJSON sql.NullString, httpStatus sql.NullInt64, temp sql.NullFloat64, maxOut sql.NullInt64, firstByte, firstToken, completed sql.NullInt64, input, cacheRead, cacheWrite, output, reasoning *int64, cost *float64, retryable sql.NullInt64) {
	r.Slot = slot.String
	r.ReasoningLevel = reasoningLevel.String
	r.ProviderRequestID = providerReqID.String
	r.StopReason = stopReason.String
	r.ErrorCode = errorCode.String
	r.ErrorMessage = errorMessage.String
	r.ErrorRaw = errorRaw.String
	r.UsageRawJSON = usageRaw.String
	r.PriceJSON = priceJSON.String
	if httpStatus.Valid {
		r.HTTPStatus = int(httpStatus.Int64)
	}
	if temp.Valid {
		v := temp.Float64
		r.Temperature = &v
	}
	if maxOut.Valid {
		r.MaxOutputTokens = maxOut.Int64
	}
	if firstByte.Valid {
		r.FirstByteAt = firstByte.Int64
	}
	if firstToken.Valid {
		r.FirstTokenAt = firstToken.Int64
	}
	if completed.Valid {
		r.CompletedAt = completed.Int64
	}
	r.InputTokens = input
	r.CacheReadTokens = cacheRead
	r.CacheWriteTokens = cacheWrite
	r.OutputTokens = output
	r.ReasoningTokens = reasoning
	r.CostUSD = cost
	r.ErrorRetryable = retryable.Valid && retryable.Int64 != 0
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

