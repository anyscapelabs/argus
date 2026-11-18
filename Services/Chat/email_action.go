package Chat

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/anyscapelabs/argus/Protocol"
	"github.com/anyscapelabs/argus/Services/MCP"
	"github.com/anyscapelabs/argus/Services/Permission"
	"github.com/anyscapelabs/argus/Services/Shared"
	"github.com/anyscapelabs/argus/Storage"
	"github.com/google/uuid"
)

// email_send never calls the Gmail connector — the connector publishes no
// send tool, and a sent message is irreversible, so the truth comes from
// Google's own API responses recorded in the action ledger.
var emailSendToolDef = Protocol.ToolDef{
	Name: Permission.EmailSendTool,
	Description: "Send an email from the user's connected Gmail account. " +
		"The user must approve the send through the app before anything leaves; " +
		"if they decline you receive an error result and should continue without sending. " +
		"Only report what the tool result tells you: it states exactly whether the " +
		"message was sent, failed, or could not be confirmed.",
	Schema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"to": {
				"type": "array",
				"items": {"type": "string"},
				"description": "Recipient email addresses, one or more"
			},
			"cc": {
				"type": "array",
				"items": {"type": "string"},
				"description": "Optional CC recipients"
			},
			"subject": {
				"type": "string",
				"description": "Email subject line"
			},
			"body": {
				"type": "string",
				"description": "Plain-text email body"
			},
			"connection_id": {
				"type": "string",
				"description": "Optional Gmail connection to send from when the user has more than one; omit to use the default"
			}
		},
		"required": ["to", "subject", "body"]
	}`),
}

// opIDFromRequest fingerprints one attempted operation, so a model retry of
// the same content maps to the same op_id and can be detected as a duplicate.
func opIDFromRequest(to, cc []string, subject, body, connectionID string) string {
	h := sha256.New()
	fmt.Fprintf(h, "email_send\x00%s\x00%s\x00%s\x00%s\x00%s",
		normalizeAddresses(to), normalizeAddresses(cc),
		strings.TrimSpace(subject), body, connectionID)
	return "op_" + hex.EncodeToString(h.Sum(nil))[:16]
}

func normalizeAddresses(in []string) string {
	out := make([]string, 0, len(in))
	for _, a := range in {
		out = append(out, strings.ToLower(strings.TrimSpace(a)))
	}
	return strings.Join(out, ",")
}

func validAddress(a string) bool {
	a = strings.TrimSpace(a)
	at := strings.LastIndex(a, "@")
	return at > 0 && at < len(a)-1 && !strings.ContainsAny(a, " \t\r\n,")
}

// resolveGmailConnection picks the connection a send rides on.
func resolveGmailConnection(db *sql.DB, agentID, connectionID string) (Storage.MCPConnection, error) {
	if connectionID != "" {
		conn, ok, err := Storage.GetMCPConnection(db, connectionID)
		if err != nil {
			return Storage.MCPConnection{}, fmt.Errorf("could not look up that Gmail connection")
		}
		if !ok || conn.ServerID != "gmail" || conn.Status != "connected" {
			return Storage.MCPConnection{}, fmt.Errorf(
				"that Gmail connection is not available (missing or not connected); call connect_mcp first or omit connection_id")
		}
		return conn, nil
	}
	bindings, err := Storage.ListAgentConnectionsForAgent(db, agentID)
	if err != nil {
		return Storage.MCPConnection{}, fmt.Errorf("could not look up this agent's Gmail connection")
	}
	var found []Storage.AgentConnection
	for _, b := range bindings {
		if b.ServerID == "gmail" && b.Status == "connected" {
			found = append(found, b)
		}
	}
	if len(found) == 0 {
		return Storage.MCPConnection{}, fmt.Errorf(
			"no Gmail account is connected for this agent; tell the user to connect one in Integrations first, and do not claim anything was sent")
	}
	if len(found) > 1 {
		return Storage.MCPConnection{}, fmt.Errorf(
			"this agent has more than one Gmail connection; pass connection_id to choose one (%s)",
			strings.Join(connectionLabels(found), ", "))
	}
	conn, ok, err := Storage.GetMCPConnection(db, found[0].ConnectionID)
	if err != nil || !ok {
		return Storage.MCPConnection{}, fmt.Errorf("the agent's Gmail connection could not be loaded")
	}
	return conn, nil
}

func connectionLabels(bindings []Storage.AgentConnection) []string {
	out := make([]string, 0, len(bindings))
	for _, b := range bindings {
		out = append(out, b.Label+" ("+b.ConnectionID+")")
	}
	return out
}

// runEmailSendTool gates, then executes, one Gmail send.
func runEmailSendTool(
	s *Service,
	ctx context.Context,
	_ string,
	agentID, agentName string,
	call Protocol.ToolCall,
	result resultFn,
	_ activityFn,
) Protocol.Block {
	var input struct {
		To           []string `json:"to"`
		CC           []string `json:"cc"`
		Subject      string   `json:"subject"`
		Body         string   `json:"body"`
		ConnectionID string   `json:"connection_id"`
	}
	if err := json.Unmarshal([]byte(call.Input), &input); err != nil {
		return result("invalid tool input: could not parse the email arguments", true)
	}
	to, bad := cleanAddresses(input.To)
	if bad != "" {
		return result(fmt.Sprintf("invalid tool input: %q is not an email address", bad), true)
	}
	cc, bad := cleanAddresses(input.CC)
	if bad != "" {
		return result(fmt.Sprintf("invalid tool input: %q is not an email address", bad), true)
	}
	if len(to) == 0 {
		return result("invalid tool input: at least one recipient in 'to' is required", true)
	}
	if strings.TrimSpace(input.Subject) == "" {
		return result("invalid tool input: subject is required", true)
	}

	db := s.Core.DB.SQL()
	conn, err := resolveGmailConnection(db, agentID, input.ConnectionID)
	if err != nil {
		return result(err.Error(), true)
	}

	actionID := "act_" + uuid.Must(uuid.NewV7()).String()
	opID := opIDFromRequest(to, cc, input.Subject, input.Body, conn.ID)
	requestJSON, _ := json.Marshal(map[string]any{
		"to": to, "cc": cc, "subject": input.Subject, "body": input.Body,
	})
	action := Storage.Action{
		ID: actionID, AgentID: agentID, AgentName: agentName,
		Kind: "email_send", Provider: "gmail", ConnectionID: conn.ID,
		RequestJSON: string(requestJSON), Status: "requested", OpID: opID,
	}
	if err := Storage.CreateAction(db, action); err != nil {
		return result("the action ledger rejected this send: "+err.Error(), true)
	}

	// Duplicate detection: a retry of the same content must never send twice.
	if prior, found, _ := Storage.FindActionByOpID(db, opID, action.Kind, action.Provider); found && prior.ID != actionID {		switch prior.Status {
		case "succeeded":
			r, _, _ := Storage.GetActionReceipt(db, prior.ID)
			return result(alreadySentResult(prior, r), false)
		case "unknown":
			return result(fmt.Sprintf(
				"An identical email (action %s) was already attempted earlier and its outcome could not be confirmed. "+
					"Do not resend. Tell the user to check the Sent folder in Gmail for the message.", prior.ID), true)
		case "running":
			return result(fmt.Sprintf(
				"An identical email (action %s) is still being sent. Do not resend; wait for its result.", prior.ID), true)
		}
	}

	s.Core.EmitEvent(Shared.EventActionRequested, emailView(action, to, cc, input.Subject, input.Body, conn))

	decision, err := Permission.Request(ctx, s.Permissions, agentID, agentName,
		Permission.EmailSendTool, emailSendSummary(to, cc, input.Subject))
	if err != nil {
		_ = Storage.SetActionStatus(db, actionID, "failed")
		emitActionStatus(s, actionID, "failed", "the permission request was cancelled")
		return result("the permission request was cancelled, so the email was not sent", true)
	}
	if decision == Permission.Deny {
		_ = Storage.SetActionStatus(db, actionID, "denied")
		emitActionStatus(s, actionID, "denied", "")
		Shared.PersistNotice(s.Core, agentID, agentName, Permission.EmailSendTool,
			"denied", "action", fmt.Sprintf("Declined %s's send of \"%s\" to %s",
				agentLabel(agentName), input.Subject, strings.Join(to, ", ")))
		return result("The user declined this email send. Do not retry it; continue without sending email.", true)
	}

	_ = Storage.SetActionStatus(db, actionID, "running")
	emitActionStatus(s, actionID, "running", "")

	outcome := s.executeEmailSend(ctx, db, action, conn, to, cc, input.Subject, input.Body)
	emitActionStatus(s, actionID, outcome.status, outcome.message)
	persistActionNotice(s, agentID, agentName, input.Subject, to, outcome)
	return result(outcome.modelResult, outcome.isError)
}

func cleanAddresses(in []string) ([]string, string) {
	var out []string
	for _, a := range in {
		a = strings.TrimSpace(a)
		if a == "" {
			continue
		}
		if !validAddress(a) {
			return nil, a
		}
		out = append(out, a)
	}
	return out, ""
}

func agentLabel(agentName string) string {
	if agentName == "" {
		return "the agent"
	}
	return agentName
}

func emailSendSummary(to, cc []string, subject string) string {
	who := strings.Join(to, ", ")
	if len(cc) > 0 {
		who += ", cc " + strings.Join(cc, ", ")
	}
	return fmt.Sprintf("Send email \"%s\" to %s", subject, who)
}

func emailView(action Storage.Action, to, cc []string, subject, body string, conn Storage.MCPConnection) Shared.ActionEventView {
	return Shared.ActionEventView{
		ActionID:  action.ID,
		AgentID:   action.AgentID,
		AgentName: action.AgentName,
		Kind:      action.Kind,
		Provider:  action.Provider,
		Status:    action.Status,
		To:        to,
		CC:        cc,
		Subject:   subject,
		Body:      body,
		From:      conn.AccountEmail,
	}
}

func emitActionStatus(s *Service, actionID, status, message string) {
	s.Core.EmitEvent(Shared.EventActionStatus, map[string]any{
		"action_id": actionID,
		"status":    status,
		"error":     message,
	})
}

func persistActionNotice(s *Service, agentID, agentName, subject string, to []string, outcome emailOutcome) {
	var text string
	switch outcome.status {
	case "succeeded":
		text = fmt.Sprintf("Sent %s's email \"%s\" to %s", agentLabel(agentName), subject, strings.Join(to, ", "))
	case "failed":
		text = fmt.Sprintf("%s's email \"%s\" failed to send: %s", agentLabel(agentName), subject, outcome.message)
	case "unknown":
		text = fmt.Sprintf("%s's email \"%s\" could not be confirmed as sent or failed", agentLabel(agentName), subject)
	default:
		return
	}
	Shared.PersistNotice(s.Core, agentID, agentName, Permission.EmailSendTool, outcome.status, "action", text)
}

func alreadySentResult(prior Storage.Action, r Storage.ActionReceipt) string {
	return fmt.Sprintf(
		"An identical email (action %s) was already sent earlier: message id %s. Do not send it again; "+
			"tell the user it was already delivered.", prior.ID, r.MessageID)
}

// emailOutcome is what the ledger and the model each need to hear: the model
// gets a verdict sentence that matches what the ledger recorded, no more.
type emailOutcome struct {
	status      string // succeeded | failed | unknown
	message     string
	modelResult string
	isError     bool
}

// executeEmailSend runs the real Gmail flow: draft, then send.
func (s *Service) executeEmailSend(
	ctx context.Context,
	db *sql.DB,
	action Storage.Action,
	conn Storage.MCPConnection,
	to, cc []string, subject, body string,
) emailOutcome {
	client, err := MCP.GoogleHTTPClient(ctx, conn.ServerID, conn.ID)
	if err != nil {
		outcome := emailOutcome{
			status:  "failed",
			message: "the Gmail session needs to be signed in again",
			isError: true,
		}
		outcome.modelResult = emailFailedResult(action.ID, outcome.message)
		_ = Storage.SetActionStatus(db, action.ID, outcome.status)
		return outcome
	}
	api := gmailAPI{base: gmailAPIBase, client: client}
	raw := buildRFC822(conn.AccountEmail, to, cc, subject, body, action.OpID)

	draftID, err := api.createDraft(ctx, raw)
	if err != nil {
		outcome := emailOutcome{
			status:  "failed",
			message: fmt.Sprintf("creating the Gmail draft failed: %s", err),
			isError: true,
		}
		outcome.modelResult = emailFailedResult(action.ID, outcome.message)
		_ = Storage.SetActionStatus(db, action.ID, outcome.status)
		return outcome
	}

	msgID, threadID, err := api.sendDraft(ctx, draftID)
	if err == nil {
		receiptJSON, _ := json.Marshal(map[string]string{"draft_id": draftID})
		_ = Storage.RecordActionReceipt(db, Storage.ActionReceipt{
			ActionID: action.ID, MessageID: msgID, ThreadID: threadID,
			DraftID: draftID, ReceiptJSON: string(receiptJSON),
		})
		_ = Storage.SetActionStatus(db, action.ID, "succeeded")
		return emailOutcome{
			status: "succeeded",
			modelResult: fmt.Sprintf(
				"EMAIL SEND SUCCEEDED (action %s): the message is confirmed sent by Gmail, message id %s, thread id %s. "+
					"You may tell the user it was sent.", action.ID, msgID, threadID),
		}
	}

	// A 4xx refusal (including 429 quota) means Google refused and nothing
	// went out; anything else is ambiguous and must be reconciled.
	if isClientRejected(err) {
		outcome := emailOutcome{
			status:  "failed",
			message: fmt.Sprintf("Gmail refused the send: %s", err),
			isError: true,
		}
		outcome.modelResult = emailFailedResult(action.ID, outcome.message+
			" The draft was not sent; the draft id is "+draftID+". Do not retry without asking the user.")
		_ = Storage.SetActionStatus(db, action.ID, outcome.status)
		return outcome
	}

	return s.reconcileEmailSend(ctx, db, action, api, draftID, err)
}

// reconcileEmailSend resolves an ambiguous send against Google's own state.
func (s *Service) reconcileEmailSend(
	ctx context.Context,
	db *sql.DB,
	action Storage.Action,
	api gmailAPI,
	draftID string,
	sendErr error,
) emailOutcome {
	record := func(method string, found bool, verdict, detail string) {
		_ = Storage.RecordActionReconciliation(db, Storage.ActionReconciliation{
			ActionID: action.ID, Method: method, Found: found, Verdict: verdict, Detail: detail,
		})
	}

	msgID, threadID, found, err := api.searchSentMessage(ctx, action.OpID)
	if err == nil && found {
		receiptJSON, _ := json.Marshal(map[string]string{"draft_id": draftID})
		_ = Storage.RecordActionReceipt(db, Storage.ActionReceipt{
			ActionID: action.ID, MessageID: msgID, ThreadID: threadID,
			DraftID: draftID, ReceiptJSON: string(receiptJSON),
		})
		_ = Storage.SetActionStatus(db, action.ID, "succeeded")
		record("message_search", true, "succeeded", "found sent message "+msgID)
		return emailOutcome{
			status: "succeeded",
			modelResult: fmt.Sprintf(
				"EMAIL SEND SUCCEEDED (action %s): the first attempt errored (%s), but the message was found in the Sent folder, message id %s. "+
					"You may tell the user it was sent.", action.ID, sendErr, msgID),
		}
	}
	record("message_search", false, "unknown", fmt.Sprintf("sent-folder search failed or found nothing (err=%v)", err))

	exists, derr := api.draftExists(ctx, draftID)
	switch {
	case derr == nil && exists:
		record("draft_gone", false, "unknown", "draft "+draftID+" still exists")
	case derr == nil && !exists:
		record("draft_gone", true, "unknown", "draft "+draftID+" was consumed but no sent message matched the token")
	case isNotFound(derr):
		record("draft_gone", true, "unknown", "draft "+draftID+" lookup returned not found")
	default:
		record("draft_gone", false, "unknown", fmt.Sprintf("draft lookup failed (err=%v)", derr))
	}

	_ = Storage.SetActionStatus(db, action.ID, "unknown")
	return emailOutcome{
		status:  "unknown",
		message: fmt.Sprintf("the send could not be confirmed: %s", sendErr),
		isError: true,
		modelResult: fmt.Sprintf(
			"EMAIL SEND UNCONFIRMED (action %s): the send call failed (%s) and reconciliation could not confirm "+
				"whether the message was delivered. Do NOT tell the user it was sent, and do NOT claim it failed. "+
				"Say the send is unconfirmed; the draft may still exist in Gmail (draft id %s), and the user can check "+
				"their Sent folder. Do not resend unless the user asks after checking.",
			action.ID, sendErr, draftID),
	}
}

func emailFailedResult(actionID, message string) string {
	return fmt.Sprintf(
		"EMAIL SEND FAILED (action %s): %s. The message was NOT sent. You may tell the user it failed and why. Do not retry without asking.",
		actionID, message)
}

// isClientRejected separates "Google said no" from "we never heard back".
func isClientRejected(err error) bool {
	code := apiErrorCode(err)
	return code >= 400 && code < 500
}

func isNotFound(err error) bool {
	return apiErrorCode(err) == http.StatusNotFound
}

func apiErrorCode(err error) int {
	if apiErr, ok := err.(*gmailAPIError); ok {
		return apiErr.Code
	}
	return 0
}

// gmailAPIError is a structured Google API response.
type gmailAPIError struct {
	Code    int
	Message string
}

func (e *gmailAPIError) Error() string {
	return fmt.Sprintf("gmail api error %d: %s", e.Code, e.Message)
}

const gmailAPIBase = "https://gmail.googleapis.com"

// gmailAPI is the narrow slice of the Gmail REST API a verified send needs.
type gmailAPI struct {
	base   string
	client *http.Client
}

func (g gmailAPI) do(ctx context.Context, method, path, body string, out any) error {
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, g.base+path, rdr)
	if err != nil {
		return err
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := g.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	if resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(raw))
		var parsed struct {
			Error struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(raw, &parsed) == nil && parsed.Error.Message != "" {
			msg = parsed.Error.Message
		}
		return &gmailAPIError{Code: resp.StatusCode, Message: msg}
	}
	if out != nil {
		return json.Unmarshal(raw, out)
	}
	return nil
}

func (g gmailAPI) createDraft(ctx context.Context, rawB64 string) (string, error) {
	payload, _ := json.Marshal(map[string]any{
		"message": map[string]any{"raw": rawB64},
	})
	var out struct {
		ID string `json:"id"`
	}
	if err := g.do(ctx, http.MethodPost, "/gmail/v1/users/me/drafts", string(payload), &out); err != nil {
		return "", err
	}
	if out.ID == "" {
		return "", &gmailAPIError{Code: 500, Message: "draft response contained no id"}
	}
	return out.ID, nil
}

func (g gmailAPI) sendDraft(ctx context.Context, draftID string) (messageID, threadID string, err error) {
	payload, _ := json.Marshal(map[string]any{"id": draftID})
	var out struct {
		ID       string `json:"id"`
		ThreadID string `json:"threadId"`
	}
	if err := g.do(ctx, http.MethodPost, "/gmail/v1/users/me/drafts/"+draftID+"/send", string(payload), &out); err != nil {
		return "", "", err
	}
	if out.ID == "" {
		return "", "", &gmailAPIError{Code: 500, Message: "send response contained no message id"}
	}
	return out.ID, out.ThreadID, nil
}

func (g gmailAPI) draftExists(ctx context.Context, draftID string) (bool, error) {
	err := g.do(ctx, http.MethodGet, "/gmail/v1/users/me/drafts/"+draftID, "", nil)
	if err == nil {
		return true, nil
	}
	if isNotFound(err) {
		return false, nil
	}
	return false, err
}

// searchSentMessage looks for the op token in the Sent mailbox.
func (g gmailAPI) searchSentMessage(ctx context.Context, opID string) (messageID, threadID string, found bool, err error) {
	q := `in:sent "argus-op-` + opID + `"`
	var out struct {
		Messages []struct {
			ID       string `json:"id"`
			ThreadID string `json:"threadId"`
		} `json:"messages"`
	}
	path := "/gmail/v1/users/me/messages?" + url.Values{"q": {q}, "maxResults": {"5"}}.Encode()
	if err := g.do(ctx, http.MethodGet, path, "", &out); err != nil {
		return "", "", false, err
	}
	if len(out.Messages) == 0 {
		return "", "", false, nil
	}
	return out.Messages[0].ID, out.Messages[0].ThreadID, true, nil
}

// buildRFC822 renders the message Google actually stores; the op id rides in
// a header and in a trailing body line so the Sent-folder search can match it.
func buildRFC822(from string, to, cc []string, subject, body, opID string) string {
	encSubject := mime.QEncoding.Encode("utf-8", subject)
	var b strings.Builder
	b.WriteString("From: " + from + "\r\n")
	b.WriteString("To: " + strings.Join(to, ", ") + "\r\n")
	if len(cc) > 0 {
		b.WriteString("Cc: " + strings.Join(cc, ", ") + "\r\n")
	}
	b.WriteString("Subject: " + encSubject + "\r\n")
	b.WriteString("Date: " + time.Now().Format(time.RFC1123Z) + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	b.WriteString("Content-Transfer-Encoding: 8bit\r\n")
	b.WriteString("X-Argus-Op-Id: " + opID + "\r\n")
	b.WriteString("\r\n")
	b.WriteString(body)
	b.WriteString("\r\n\r\n[argus-op-" + opID + "]")
	return base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString([]byte(b.String()))
}
