package Chat

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anyscapelabs/argus/Storage"
)

func newEmailTestDB(t *testing.T) *Storage.DB {
	t.Helper()
	db, err := Storage.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestOpIDIsDeterministicPerContent(t *testing.T) {
	a := opIDFromRequest([]string{"To@Example.com"}, nil, "Hi", "body", "conn1")
	b := opIDFromRequest([]string{"to@example.com "}, nil, "Hi", "body", "conn1")
	if a != b {
		t.Errorf("same content, different fingerprint: %s vs %s", a, b)
	}
	if a == opIDFromRequest([]string{"to@example.com"}, nil, "Hi!", "body", "conn1") {
		t.Errorf("a changed subject must change the fingerprint")
	}
	if a == opIDFromRequest([]string{"other@example.com"}, nil, "Hi", "body", "conn1") {
		t.Errorf("changed recipients must change the fingerprint")
	}
}

func TestValidAddress(t *testing.T) {
	for _, ok := range []string{"a@b.c", "first.last@sub.example.co"} {
		if !validAddress(ok) {
			t.Errorf("%q should be valid", ok)
		}
	}
	for _, bad := range []string{"", "nodomain", "@x.com", "a b@c.com", "a,b@c.com"} {
		if validAddress(bad) {
			t.Errorf("%q should be invalid", bad)
		}
	}
}

func TestBuildRFC822(t *testing.T) {
	raw := buildRFC822("me@gmail.com", []string{"a@b.c"}, []string{"d@e.f"}, "Sub ject", "hello", "op_abc")
	decoded, err := base64.URLEncoding.WithPadding(base64.NoPadding).DecodeString(raw)
	if err != nil {
		t.Fatalf("raw is not unpadded base64url: %v", err)
	}
	s := string(decoded)
	for _, want := range []string{
		"From: me@gmail.com", "To: a@b.c", "Cc: d@e.f",
		"Subject: Sub ject", "X-Argus-Op-Id: op_abc", "hello", "[argus-op-op_abc]",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in:\n%s", want, s)
		}
	}
}

func fakeGmail(t *testing.T, handler http.HandlerFunc) gmailAPI {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return gmailAPI{base: srv.URL, client: srv.Client()}
}

func TestGmailAPISuccessPath(t *testing.T) {
	var gotSend struct {
		body string
		id   string
	}
	api := fakeGmail(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/drafts"):
			var req struct {
				Message struct {
					Raw string `json:"raw"`
				} `json:"message"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			raw, derr := base64.URLEncoding.WithPadding(base64.NoPadding).DecodeString(req.Message.Raw)
			if derr != nil {
				t.Errorf("draft raw is not unpadded base64url: %v", derr)
			}
			gotSend.body = string(raw)
			_, _ = w.Write([]byte(`{"id":"draft_1","message":{"id":"pending"}}`))
		case strings.Contains(r.URL.Path, "/drafts/draft_1/send"):
			var req struct {
				ID string `json:"id"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			gotSend.id = req.ID
			_, _ = w.Write([]byte(`{"id":"msg_9","threadId":"th_9"}`))
		default:
			http.NotFound(w, r)
		}
	})

	raw := buildRFC822("me@x.com", []string{"a@b.c"}, nil, "s", "b", "op_1")
	draftID, err := api.createDraft(context.Background(), raw)
	if err != nil {
		t.Fatalf("createDraft: %v", err)
	}
	msgID, threadID, err := api.sendDraft(context.Background(), draftID)
	if err != nil {
		t.Fatalf("sendDraft: %v", err)
	}
	if msgID != "msg_9" || threadID != "th_9" {
		t.Errorf("unexpected send response %q %q", msgID, threadID)
	}
	if gotSend.id != draftID || !strings.Contains(gotSend.body, "[argus-op-op_1]") {
		t.Errorf("send payload wrong: id=%q body=%q", gotSend.id, gotSend.body)
	}
}

func TestGmailQuotaRefusalIsAClientRejection(t *testing.T) {
	api := fakeGmail(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"code":429,"message":"daily_limit_reached, rate limit exceeded"}}`))
	})
	_, _, err := api.sendDraft(context.Background(), "d1")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !isClientRejected(err) {
		t.Errorf("429 must count as a refusal (nothing sent), got %#v", err)
	}
	if !strings.Contains(err.Error(), "daily_limit_reached") {
		t.Errorf("the parsed Google message should surface, got %v", err)
	}
}

func TestReconciliationFindsTheSentMessage(t *testing.T) {
	db := newEmailTestDB(t)
	action := Storage.Action{
		ID: "act_r1", AgentID: "default", Kind: "email_send", Provider: "gmail",
		Status: "running", OpID: "op_r1",
	}
	if err := Storage.CreateAction(db.SQL(), action); err != nil {
		t.Fatalf("create action: %v", err)
	}
	searched := false
	api := fakeGmail(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/gmail/v1/users/me/messages"):
			if !strings.Contains(r.URL.RawQuery, "argus-op-op_r1") {
				t.Errorf("sent search must query the op token, got %q", r.URL.RawQuery)
			}
			searched = true
			_, _ = w.Write([]byte(`{"messages":[{"id":"msg_5","threadId":"th_5"}]}`))
		default:
			http.NotFound(w, r)
		}
	})

	s := &Service{}
	outcome := s.reconcileEmailSend(context.Background(), db.SQL(), action, api, "draft_1", errNetwork)
	if outcome.status != "succeeded" {
		t.Fatalf("expected succeeded after finding the sent message, got %q (%s)", outcome.status, outcome.message)
	}
	if !searched {
		t.Errorf("reconciliation never searched the sent mailbox")
	}
	got, _, _ := Storage.GetAction(db.SQL(), "act_r1")
	if got.Status != "succeeded" {
		t.Errorf("ledger status %s", got.Status)
	}
	r, ok, _ := Storage.GetActionReceipt(db.SQL(), "act_r1")
	if !ok || r.MessageID != "msg_5" {
		t.Errorf("receipt not recorded: %+v", r)
	}
}

func TestReconciliationLeavesUnconfirmedAsUnknown(t *testing.T) {
	db := newEmailTestDB(t)
	action := Storage.Action{
		ID: "act_r2", AgentID: "default", Kind: "email_send", Provider: "gmail",
		Status: "running", OpID: "op_r2",
	}
	if err := Storage.CreateAction(db.SQL(), action); err != nil {
		t.Fatalf("create action: %v", err)
	}
	api := fakeGmail(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/gmail/v1/users/me/messages") {
			_, _ = w.Write([]byte(`{"messages":[]}`))
			return
		}
		if strings.HasPrefix(r.URL.Path, "/gmail/v1/users/me/drafts/draft_1") {
			_, _ = w.Write([]byte(`{"id":"draft_1"}`))
			return
		}
		http.NotFound(w, r)
	})

	s := &Service{}
	outcome := s.reconcileEmailSend(context.Background(), db.SQL(), action, api, "draft_1", errNetwork)
	if outcome.status != "unknown" {
		t.Fatalf("expected unknown, got %q", outcome.status)
	}
	if !strings.Contains(outcome.modelResult, "Do NOT tell the user it was sent") {
		t.Errorf("the model must be told not to claim success, got:\n%s", outcome.modelResult)
	}
	got, _, _ := Storage.GetAction(db.SQL(), "act_r2")
	if got.Status != "unknown" {
		t.Errorf("ledger status %s", got.Status)
	}
	if _, ok, _ := Storage.GetActionReceipt(db.SQL(), "act_r2"); ok {
		t.Errorf("no receipt may exist for an unconfirmed send")
	}
}

// errNetwork stands in for a connection that died mid-send.
var errNetwork = &gmailAPIError{Code: 0, Message: "connection reset"}
