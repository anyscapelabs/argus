package Storage

import (
	"testing"

	"github.com/anyscapelabs/argus/Protocol"
)

// The shape a real tool-using turn leaves behind, written exactly the way
// Chat/send.go writes it: the user row at N, the reply row reserved at N+1, the
// tool round pairs after it, and then the reserved row parked at the end by
// ReseqMessage — which abandons N+1 as a permanent hole, because NextSeq only
// ever hands out MAX(seq)+1.
//
// The startup cleanser used to read that hole as "this question was never
// answered" and delete the user row. These tests are the reason it can no
// longer touch it.
func seedToolTurn(t *testing.T, db *DB, agent string) {
	t.Helper()
	rows := []Message{
		{ID: agent + "-user", AgentID: agent, Seq: 10, Role: "user", Text: "check the repos in my github",
			BlocksJSON: `[{"type":"text","text":"check the repos in my github"}]`, Status: "complete", CreatedAt: 1, CompletedAt: 1},
		// seq 11 is the hole the parked reply left behind. No row exists there.
		{ID: agent + "-round1a", AgentID: agent, Seq: 12, Role: "assistant", Text: "",
			BlocksJSON: `[{"type":"tool_use","id":"t1","name":"search_repositories"}]`, Status: "complete", CreatedAt: 2, CompletedAt: 2},
		{ID: agent + "-round1b", AgentID: agent, Seq: 13, Role: "user", Text: "",
			BlocksJSON: `[{"type":"tool_result","tool_use_id":"t1","content":"…"}]`, Status: "complete", CreatedAt: 3, CompletedAt: 3},
		{ID: agent + "-reply", AgentID: agent, Seq: 14, Role: "assistant", Text: "Your GitHub connection is active.",
			BlocksJSON: `[{"type":"text","text":"Your GitHub connection is active."}]`, Status: "complete", CreatedAt: 4, CompletedAt: 4},
	}
	for _, m := range rows {
		if err := db.InsertMessage(m); err != nil {
			t.Fatalf("seed %s: %v", m.ID, err)
		}
	}
}

func keptRows(t *testing.T, db *DB, agent string) map[int]Message {
	t.Helper()
	msgs, err := db.ListMessages(agent, 0, 100)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	bySeq := make(map[int]Message, len(msgs))
	for _, m := range msgs {
		bySeq[m.Seq] = m
	}
	return bySeq
}

// The turn that used to be eaten: a hole directly above the question, with the
// reply parked further up the transcript.
func TestStartupKeepsAToolTurnsQuestionAcrossTheHole(t *testing.T) {
	db := openTestDB(t)
	seedAgent(t, db, "default")
	seedToolTurn(t, db, "default")

	// The whole startup path, in the order Chat.ServiceStartup runs it.
	if err := db.SweepInterrupted(); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	unanswered, err := db.UnansweredTurnsAll()
	if err != nil {
		t.Fatalf("count: %v", err)
	}

	if unanswered != 0 {
		t.Errorf("unanswered = %d, want 0: the reply exists, four seqs above its hole", unanswered)
	}
	rows := keptRows(t, db, "default")
	if len(rows) != 4 {
		t.Fatalf("%d rows survived, want all 4 still stored", len(rows))
	}
	q, ok := rows[10]
	if !ok || q.Role != "user" || q.Text != "check the repos in my github" {
		t.Fatalf("the question is gone or altered: %+v", q)
	}
	if rows[14].Text != "Your GitHub connection is active." {
		t.Errorf("reply = %q, want it parked where ReseqMessage left it", rows[14].Text)
	}
}

// A turn with no reply at all is reported, not removed: the user still has what
// they asked, and the log line is the only thing that changes.
func TestUnansweredTurnIsCountedAndKept(t *testing.T) {
	db := openTestDB(t)
	seedAgent(t, db, "default")
	// The last message of a session, killed before the reply row was written.
	if err := db.InsertMessage(Message{ID: "q1", AgentID: "default", Seq: 20, Role: "user",
		Text: "connect my todoist", BlocksJSON: "[]", Status: "complete", CreatedAt: 5}); err != nil {
		t.Fatalf("insert: %v", err)
	}

	if err := db.SweepInterrupted(); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	n, err := db.UnansweredTurns("default")
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Errorf("unanswered = %d, want the lone question counted once", n)
	}
	rows := keptRows(t, db, "default")
	if rows[20].Text != "connect my todoist" {
		t.Fatalf("the question was removed: %+v", rows)
	}
}

// Tool rounds alone do not answer a question: a run that died after two round
// trips leaves its round rows and its question behind, and is still reported as
// unanswered.
func TestRoundsWithoutAReplyAreStillUnanswered(t *testing.T) {
	db := openTestDB(t)
	seedAgent(t, db, "default")
	rows := []Message{
		{ID: "u", AgentID: "default", Seq: 30, Role: "user", Text: "open my linear account", BlocksJSON: "[]", Status: "complete", CreatedAt: 6},
		{ID: "r1", AgentID: "default", Seq: 31, Role: "assistant", Text: "", BlocksJSON: `[{"type":"tool_use"}]`, Status: "streaming", CreatedAt: 7},
		{ID: "c1", AgentID: "default", Seq: 32, Role: "user", Text: "", BlocksJSON: `[{"type":"tool_result"}]`, Status: "complete", CreatedAt: 8},
	}
	for _, m := range rows {
		if err := db.InsertMessage(m); err != nil {
			t.Fatalf("insert %s: %v", m.ID, err)
		}
	}

	if err := db.SweepInterrupted(); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	n, err := db.UnansweredTurns("default")
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Errorf("unanswered = %d, want the crashed run reported once", n)
	}
	after := keptRows(t, db, "default")
	if len(after) != 3 {
		t.Fatalf("%d rows left, want the crashed run's three rows kept", len(after))
	}
	if after[31].Status != "interrupted" {
		t.Errorf("reply status = %q, want the sweep to have marked it interrupted", after[31].Status)
	}
	if after[30].Text != "open my linear account" {
		t.Errorf("question = %q, want it untouched", after[30].Text)
	}
}

// A row that carries an error message answers its turn even with no prose: the
// user needs the failure, and the count must not call it unanswered.
func TestErrorReplyCountsAsAnAnswer(t *testing.T) {
	db := openTestDB(t)
	seedAgent(t, db, "default")
	rows := []Message{
		{ID: "u2", AgentID: "default", Seq: 40, Role: "user", Text: "list the repos", BlocksJSON: "[]", Status: "complete", CreatedAt: 9},
		{ID: "e2", AgentID: "default", Seq: 41, Role: "assistant", Text: "",
			BlocksJSON: "[]", Status: "streaming", CreatedAt: 10},
	}
	for _, m := range rows {
		if err := db.InsertMessage(m); err != nil {
			t.Fatalf("insert %s: %v", m.ID, err)
		}
	}
	// The failure arrives the way the send path writes it: the reserved row is
	// finalised, and error_message is only ever set there.
	if err := db.FinalizeMessage("e2", []Protocol.Block{}, "", "error", "", "", "provider returned 429", 11); err != nil {
		t.Fatalf("finalize: %v", err)
	}
	n, err := db.UnansweredTurns("default")
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Errorf("unanswered = %d, want an errored reply to close the turn", n)
	}
}

// The next visible user row ends a turn: a reply belonging to the turn below
// must not be credited to the one above it.
func TestUnansweredStopsAtTheNextQuestion(t *testing.T) {
	db := openTestDB(t)
	seedAgent(t, db, "default")
	rows := []Message{
		{ID: "u3", AgentID: "default", Seq: 50, Role: "user", Text: "first question", BlocksJSON: "[]", Status: "complete", CreatedAt: 11},
		{ID: "u4", AgentID: "default", Seq: 51, Role: "user", Text: "second question", BlocksJSON: "[]", Status: "complete", CreatedAt: 12},
		{ID: "r3", AgentID: "default", Seq: 52, Role: "assistant", Text: "an answer to the second", BlocksJSON: "[]", Status: "complete", CreatedAt: 13, CompletedAt: 13},
	}
	for _, m := range rows {
		if err := db.InsertMessage(m); err != nil {
			t.Fatalf("insert %s: %v", m.ID, err)
		}
	}
	n, err := db.UnansweredTurns("default")
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Errorf("unanswered = %d, want only the first question counted", n)
	}
}

// A steering row is a turn the app started on the user's behalf: it is counted
// like any other question, and it is kept like any other question.
func TestSteeringRowIsKeptAsATurn(t *testing.T) {
	db := openTestDB(t)
	seedAgent(t, db, "default")
	if err := db.InsertMessage(Message{ID: "st", AgentID: "default", Seq: 60, Role: "user",
		Kind: MessageKindSteering, Text: "Gmail just finished connecting", BlocksJSON: "[]", Status: "complete", CreatedAt: 14}); err != nil {
		t.Fatalf("insert: %v", err)
	}
	n, err := db.UnansweredTurns("default")
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Errorf("unanswered = %d, want the steering turn reported while it has no reply", n)
	}
	if _, found, err := db.GetMessage("st"); err != nil || !found {
		t.Fatal("the steering row must still be stored for buildHistory to replay")
	}
}
