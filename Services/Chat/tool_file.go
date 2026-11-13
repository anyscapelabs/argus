package Chat

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/anyscapelabs/argus/Protocol"
	"github.com/anyscapelabs/argus/Services/Permission"
	filesystem "github.com/anyscapelabs/argus/tools/filesystem"
	"strings"
)

// runFileTool is the read/write/grep path all three filesystem tools share.
// The gate comes first: no byte moves on disk until the user answers through
// the permission card — or until a stored grant answers on their behalf, and
// an unattended run is refused with a notice instead of hanging.
func (s *Service) runFileTool(
	ctx context.Context,
	call Protocol.ToolCall,
	result func(text string, isError bool) Protocol.Block,
	activity func(kind, command string),
	agentID, agentName string,
) Protocol.Block {
	// gate asks through the permission card and converts the answer into an
	// early error result (skip=true).
	gate := func(summary string) (bool, Protocol.Block) {
		decision, err := Permission.Request(ctx, s.Permissions, agentID, agentName, Permission.FileAccessTool, summary)
		if err != nil {
			return true, result("the permission request was cancelled", true)
		}
		if decision == Permission.Deny {
			return true, result("The user declined file access. Do not retry this call; continue without reading, writing or searching files.", true)
		}
		return false, Protocol.Block{}
	}

	switch call.Name {
	case "read_file":
		var input struct {
			Path   string `json:"path"`
			Offset int    `json:"offset"`
			Limit  int    `json:"limit"`
		}
		if err := json.Unmarshal([]byte(call.Input), &input); err != nil {
			return result("invalid tool input: could not parse the read_file arguments", true)
		}
		summary := "read file: " + strings.TrimSpace(input.Path)
		if skip, denied := gate(summary); skip {
			return denied
		}
		activity("tool_started", summary)
		res, rerr := filesystem.Read(input.Path, input.Offset, input.Limit)
		activity("tool_finished", summary)
		if rerr != nil {
			return result("could not read the file: "+rerr.Error(), true)
		}
		var b strings.Builder
		b.WriteString(res.Content)
		fmt.Fprintf(&b, "\n\nfile %s, lines %d-%d of %d total", res.Path, res.Offset, res.Offset+res.Lines-1, res.TotalLines)
		if res.Truncated {
			b.WriteString("\nThe page was cut short. Call again with a later offset to continue, or pass a higher limit.")
		}
		return result(b.String(), false)

	case "write_file":
		var input struct {
			Path      string `json:"path"`
			Content   string `json:"content"`
			Overwrite bool   `json:"overwrite"`
			Append    bool   `json:"append"`
		}
		if err := json.Unmarshal([]byte(call.Input), &input); err != nil {
			return result("invalid tool input: could not parse the write_file arguments", true)
		}
		verb := "write"
		if input.Append {
			verb = "append"
		} else if input.Overwrite {
			verb = "overwrite"
		}
		summary := fmt.Sprintf("%s file: %s (%d bytes)", verb, strings.TrimSpace(input.Path), len(input.Content))
		if skip, denied := gate(summary); skip {
			return denied
		}
		activity("tool_started", summary)
		res, werr := filesystem.Write(input.Path, input.Content, input.Overwrite, input.Append)
		activity("tool_finished", summary)
		if werr != nil {
			return result("could not write the file: "+werr.Error(), true)
		}
		block := result(fmt.Sprintf("%s %s: %d bytes, %d lines written.", res.Action, res.Path, res.Bytes, res.Lines), false)
		block.FileWrite = &Protocol.FileWrite{
			Path: res.Path, Action: res.Action, Bytes: res.Bytes, Lines: res.Lines,
		}
		return block

	case "edit_file":
		var input struct {
			Path                 string `json:"path"`
			OldText              string `json:"old_text"`
			NewText              string `json:"new_text"`
			ExpectedReplacements int    `json:"expected_replacements"`
		}
		if err := json.Unmarshal([]byte(call.Input), &input); err != nil {
			return result("invalid tool input: could not parse edit_file arguments", true)
		}
		summary := "edit file: " + strings.TrimSpace(input.Path)
		if skip, denied := gate(summary); skip {
			return denied
		}
		activity("tool_started", summary)
		res, editErr := filesystem.Edit(input.Path, input.OldText, input.NewText, input.ExpectedReplacements)
		activity("tool_finished", summary)
		if editErr != nil {
			return result("could not edit the file: "+editErr.Error(), true)
		}
		block := result(fmt.Sprintf("changed %s: %d bytes, %d lines.", res.Path, res.Bytes, res.Lines), false)
		block.FileWrite = &Protocol.FileWrite{Path: res.Path, Action: res.Action, Bytes: res.Bytes, Lines: res.Lines}
		return block

	case "grep_files":
		var input struct {
			Pattern string `json:"pattern"`
			Include string `json:"include"`
			Path    string `json:"path"`
		}
		if err := json.Unmarshal([]byte(call.Input), &input); err != nil {
			return result("invalid tool input: could not parse the grep_files arguments", true)
		}
		summary := fmt.Sprintf("search files for %q in %s", strings.TrimSpace(input.Pattern), strings.TrimSpace(input.Path))
		if skip, denied := gate(summary); skip {
			return denied
		}
		activity("tool_started", summary)
		res, gerr := filesystem.Grep(input.Pattern, strings.TrimSpace(input.Include), input.Path)
		activity("tool_finished", summary)
		if gerr != nil {
			return result("could not search the files: "+gerr.Error(), true)
		}
		var b strings.Builder
		for _, m := range res.Matches {
			fmt.Fprintf(&b, "%s:%d:%s\n", m.Path, m.Line, m.Text)
		}
		fmt.Fprintf(&b, "\n%d match(es) across %d file(s).", len(res.Matches), res.FilesScan)
		if res.Limited {
			b.WriteString(" The result cap of 200 was hit: narrow the pattern or search a narrower folder for the rest.")
		}
		if len(res.Matches) == 0 && res.FilesScan == 0 {
			b.WriteString(" Nothing matched — the filter or folder may have caught too much.")
		}
		return result(b.String(), false)
	}
	return result("unknown file tool: "+call.Name, true)
}
