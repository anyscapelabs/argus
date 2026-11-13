package Storage

import "testing"

// A send writes two rows (user turn plus the reserved assistant reply) but
// historically bumped the counter once, deriving the assistant seq as
// userSeq+1. The counter's next value was then already occupied, and the next
// allocation — persistRound's first insert — died on the (agent_id, seq)
// unique index. Two back-to-back allocations must both hand out free seqs.
func TestNextSeqBackToBackAllocationsDoNotCollide(t *testing.T) {
	db := openTestDB(t)
	seedAgent(t, db, "default")

	userSeq, err := db.NextSeq("default")
	if err != nil {
		t.Fatalf("first alloc: %v", err)
	}
	assistantSeq, err := db.NextSeq("default")
	if err != nil {
		t.Fatalf("second alloc: %v", err)
	}

	rows := []Message{
		{ID: "u1", AgentID: "default", Seq: userSeq, Role: "user", Text: "hi", BlocksJSON: "[]", Status: "complete"},
		{ID: "a1", AgentID: "default", Seq: assistantSeq, Role: "assistant", Text: "", BlocksJSON: "[]", Status: "streaming"},
	}
	for _, m := range rows {
		if err := db.InsertMessage(m); err != nil {
			t.Fatalf("insert %s at seq %d: %v", m.ID, m.Seq, err)
		}
	}

	// A later allocation (the round rows) must not hand out an occupied seq.
	roundSeq, err := db.NextSeq("default")
	if err != nil {
		t.Fatalf("round alloc: %v", err)
	}
	if err := db.InsertMessage(Message{ID: "r1", AgentID: "default", Seq: roundSeq, Role: "assistant", Text: "x", BlocksJSON: "[]", Status: "complete"}); err != nil {
		t.Fatalf("round insert: %v", err)
	}
}

// A counter left behind the rows — a database written before the counter
// table existed, or one hit by the derived-assistant-seq bug — must heal:
// NextSeq skips occupied values instead of handing out a colliding seq.
func TestNextSeqSkipsOccupiedSeqs(t *testing.T) {
	db := openTestDB(t)
	seedAgent(t, db, "default")

	// Rows at 1..3 written directly, with no counter row at all: the lazy
	// initialiser starts from MAX+1, so plant a stale counter instead.
	for i, id := range []string{"m1", "m2", "m3"} {
		if err := db.InsertMessage(Message{ID: id, AgentID: "default", Seq: i + 1, Role: "user", Text: "x", BlocksJSON: "[]", Status: "complete"}); err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}
	if _, err := db.conn.Exec(`INSERT INTO seq_counters (agent_id, next_seq) VALUES ('default', 2)`); err != nil {
		t.Fatalf("stale counter: %v", err)
	}

	for want := 4; want <= 6; want++ {
		seq, err := db.NextSeq("default")
		if err != nil {
			t.Fatalf("alloc %d: %v", want, err)
		}
		if seq != want {
			t.Fatalf("NextSeq = %d, want %d", seq, want)
		}
	}
}

// ReseqMessage relocates the reserved reply row behind the round rows and
// leaves a hole; the counter must stay ahead so later allocations never
// collide with the row that moved.
func TestReseqKeepsCounterAhead(t *testing.T) {
	db := openTestDB(t)
	seedAgent(t, db, "default")

	userSeq, _ := db.NextSeq("default")
	assistantSeq, _ := db.NextSeq("default")
	roundSeq, _ := db.NextSeq("default")
	for _, m := range []Message{
		{ID: "u1", AgentID: "default", Seq: userSeq, Role: "user", Text: "hi", BlocksJSON: "[]", Status: "complete"},
		{ID: "a1", AgentID: "default", Seq: assistantSeq, Role: "assistant", Text: "", BlocksJSON: "[]", Status: "streaming"},
		{ID: "r1", AgentID: "default", Seq: roundSeq, Role: "assistant", Text: "tool", BlocksJSON: "[]", Status: "complete"},
	} {
		if err := db.InsertMessage(m); err != nil {
			t.Fatalf("insert %s: %v", m.ID, err)
		}
	}

	finalSeq, err := db.NextSeq("default")
	if err != nil {
		t.Fatalf("final alloc: %v", err)
	}
	if err := db.ReseqMessage("a1", finalSeq); err != nil {
		t.Fatalf("reseq: %v", err)
	}

	next, err := db.NextSeq("default")
	if err != nil {
		t.Fatalf("alloc after reseq: %v", err)
	}
	if err := db.InsertMessage(Message{ID: "n1", AgentID: "default", Seq: next, Role: "user", Text: "again", BlocksJSON: "[]", Status: "complete"}); err != nil {
		t.Fatalf("insert after reseq: %v", err)
	}
}
