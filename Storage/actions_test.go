package Storage

import "testing"

func action(op string) Action {
	return Action{
		ID: "act_" + op, AgentID: "default", AgentName: "Argus",
		Kind: "email_send", Provider: "gmail", ConnectionID: "conn_1",
		RequestJSON: `{"to":["a@b.c"]}`, Status: "requested", OpID: op,
	}
}

func TestActionLedgerRoundTrip(t *testing.T) {
	db := openTestDB(t)

	if err := CreateAction(db.SQL(), action("op1")); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, ok, err := GetAction(db.SQL(), "act_op1")
	if err != nil || !ok {
		t.Fatalf("get: ok=%v err=%v", ok, err)
	}
	if got.Kind != "email_send" || got.Status != "requested" || got.OpID != "op1" {
		t.Errorf("unexpected row: %+v", got)
	}

	if _, ok, _ := FindActionByOpID(db.SQL(), "op1", "email_send", "gmail"); !ok {
		t.Errorf("op lookup should find the row")
	}
	if _, ok, _ := FindActionByOpID(db.SQL(), "op1", "email_send", "other"); ok {
		t.Errorf("op lookup must be scoped by provider")
	}
}

func TestActionStatusOnlyMovesForward(t *testing.T) {
	db := openTestDB(t)
	if err := CreateAction(db.SQL(), action("op2")); err != nil {
		t.Fatalf("create: %v", err)
	}

	for _, step := range []string{"running", "succeeded"} {
		if err := SetActionStatus(db.SQL(), "act_op2", step); err != nil {
			t.Fatalf("set %s: %v", step, err)
		}
	}
	got, _, _ := GetAction(db.SQL(), "act_op2")
	if got.Status != "succeeded" {
		t.Fatalf("expected succeeded, got %s", got.Status)
	}

	// A terminal state is final.
	if err := SetActionStatus(db.SQL(), "act_op2", "failed"); err != nil {
		t.Fatalf("set failed returned err: %v", err)
	}
	got, _, _ = GetAction(db.SQL(), "act_op2")
	if got.Status != "succeeded" {
		t.Errorf("terminal action moved to %s", got.Status)
	}

	// requested → denied is legal; denied is terminal.
	if err := CreateAction(db.SQL(), action("op3")); err != nil {
		t.Fatalf("create op3: %v", err)
	}
	if err := SetActionStatus(db.SQL(), "act_op3", "denied"); err != nil {
		t.Fatalf("deny: %v", err)
	}
	if err := SetActionStatus(db.SQL(), "act_op3", "running"); err != nil {
		t.Fatalf("re-run returned err: %v", err)
	}
	got, _, _ = GetAction(db.SQL(), "act_op3")
	if got.Status != "denied" {
		t.Errorf("denied action moved to %s", got.Status)
	}
}

func TestActionReceiptAndReconciliation(t *testing.T) {
	db := openTestDB(t)
	if err := CreateAction(db.SQL(), action("op4")); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := RecordActionReceipt(db.SQL(), ActionReceipt{
		ActionID: "act_op4", MessageID: "msg_1", ThreadID: "th_1", DraftID: "d_1",
		ReceiptJSON: `{"draft_id":"d_1"}`,
	}); err != nil {
		t.Fatalf("receipt: %v", err)
	}
	r, ok, err := GetActionReceipt(db.SQL(), "act_op4")
	if err != nil || !ok || r.MessageID != "msg_1" {
		t.Fatalf("receipt: ok=%v err=%v r=%+v", ok, err, r)
	}

	if err := RecordActionReconciliation(db.SQL(), ActionReconciliation{
		ActionID: "act_op4", Method: "message_search", Found: true,
		Verdict: "succeeded", Detail: "found sent message msg_1",
	}); err != nil {
		t.Fatalf("reconciliation: %v", err)
	}
}
