package Chat

import (
	"strings"
	"testing"

	"github.com/anyscapelabs/argus/Protocol"
)

const echoDoc = "# Q3 plan\n\nShip the importer in September and retire the legacy sync by October.\n\n- Owner: platform team\n- Risk: vendor API rate limits"

func TestStripEchoFromBlocksDropsCopyKeepsToolUse(t *testing.T) {
	blocks := []Protocol.Block{
		{Type: Protocol.BlockText, Text: echoDoc},
		{Type: Protocol.BlockToolUse, ToolCall: &Protocol.ToolCall{ID: "1", Name: "write_artifact", Valid: true}},
	}
	out, n := stripEchoFromBlocks(blocks, []string{echoDoc})
	if n == 0 || len(out) != 1 || out[0].Type != Protocol.BlockToolUse {
		t.Fatalf("got %+v (dropped %d)", out, n)
	}
}

func TestStripEchoFromBlocksKeepsPreamble(t *testing.T) {
	blocks := []Protocol.Block{{Type: Protocol.BlockText, Text: "Writing that up now.\n\n" + echoDoc}}
	out, _ := stripEchoFromBlocks(blocks, []string{echoDoc})
	if len(out) != 1 || out[0].Text != "Writing that up now." {
		t.Fatalf("got %+v", out)
	}
}

func TestStripEchoFromBlocksNoDocsIsIdentity(t *testing.T) {
	blocks := []Protocol.Block{{Type: Protocol.BlockText, Text: echoDoc}}
	out, n := stripEchoFromBlocks(blocks, nil)
	if n != 0 || out[0].Text != echoDoc {
		t.Fatalf("got %+v,%d", out, n)
	}
}

func TestInvalidCallMessageForArtifactSaysDoNotPaste(t *testing.T) {
	if m := invalidCallMessage("write_artifact"); !strings.Contains(m, "append=true") || !strings.Contains(m, "Do not paste") {
		t.Fatalf("message lacks recovery guidance: %q", m)
	}
	if m := invalidCallMessage("read_file"); strings.Contains(m, "append") {
		t.Fatalf("other tools keep the generic message: %q", m)
	}
}