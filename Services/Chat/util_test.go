package Chat

import (
	"testing"

	"github.com/anyscapelabs/argus/Storage"
)

func TestNormalizeAgentID(t *testing.T) {
	cases := map[string]string{
		"":       "default",
		"   ":    "default",
		"a1":     "a1",
		" a1 ":   "a1",
		"\ta1\n": "a1",
	}
	for in, want := range cases {
		if got := normalizeAgentID(in); got != want {
			t.Errorf("normalizeAgentID(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRequestTokensCountsCacheAsInputAndReasoningAsOutput(t *testing.T) {
	n := func(v int64) *int64 { return &v }
	in, out := requestTokens(Storage.ModelRequest{
		InputTokens:      n(10),
		CacheReadTokens:  n(5),
		CacheWriteTokens: n(3),
		OutputTokens:     n(7),
		ReasoningTokens:  n(2),
	})
	if in != 18 || out != 9 {
		t.Errorf("requestTokens = (%d, %d), want (18, 9)", in, out)
	}
	if in, out := requestTokens(Storage.ModelRequest{}); in != 0 || out != 0 {
		t.Errorf("requestTokens of an empty request = (%d, %d), want (0, 0)", in, out)
	}
}
