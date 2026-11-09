package Chat

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/anyscapelabs/argus/Protocol"
	"github.com/anyscapelabs/argus/Services/Chat/Present"
)

// artifactEchoFallback stands in for a reply that contained nothing but a copy
// of the saved document.
const artifactEchoFallback = "I've saved that as an artifact."

// invalidCallMessage is the tool result for a call whose arguments did not parse.
// For write_artifact the usual cause is a body cut off by the output limit, and
// the model's instinct after a failure is to paste the document into the chat, so
// the message says what to do instead.
func invalidCallMessage(tool string) string {
	if tool == writeArtifactToolDef.Name {
		return "invalid tool call: the write_artifact arguments were not valid JSON, most likely because the document was too long and got cut off. " +
			"Nothing was saved. Call write_artifact again with a shorter first part, then continue with artifact_id and append=true. " +
			"Do not paste the document into the chat."
	}
	return "invalid tool call: the arguments were not valid JSON"
}

// runArtifactTool is the write_artifact handler. It stays out of runTool so the
// artifact rules (what is saved, what the model is told, what the card shows) can
// change without touching the dispatcher every other tool shares.
func (s *Service) runArtifactTool(
	call Protocol.ToolCall,
	agentID string,
	result func(text string, isError bool) Protocol.Block,
	activity func(kind, command string),
) Protocol.Block {
	if s.Artifacts == nil {
		return result("documents are not available in this build", true)
	}
	var input struct {
		Title      string `json:"title"`
		Body       string `json:"body"`
		ArtifactID string `json:"artifact_id"`
		Append     bool   `json:"append"`
	}
	if err := json.Unmarshal([]byte(call.Input), &input); err != nil {
		return result("invalid tool input: could not parse the write_artifact arguments", true)
	}
	if strings.TrimSpace(input.Body) == "" {
		return result("invalid tool input: body is required — the document text itself", true)
	}
	label := strings.TrimSpace(input.Title)
	if label == "" {
		label = strings.TrimSpace(input.ArtifactID)
	}
	activity("tool_started", label)
	res := s.Artifacts.Save(agentID, input.ArtifactID, input.Title, input.Body, input.Append)
	activity("tool_finished", label)
	if res.Error != nil {
		return result("could not save the document: "+res.Error.Message, true)
	}
	if res.Rejected {
		// The document was not stored, so the finding list is the whole
		// answer: line numbers and the rule, enough to fix in this turn.
		return result("The document was NOT saved. It broke the document rules:\n"+
			res.Findings+"\nCorrect those lines and call write_artifact again. "+
			"Do not paste the document into the chat instead.", true)
	}
	verb := "Saved"
	if input.Append {
		verb = "Extended"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s %q as an artifact (id %s, version %d, %d words). ",
		verb, res.Artifact.Title, shortID(res.Artifact.ID), res.Artifact.Version, res.Artifact.Words)
	b.WriteString("It is in the Artifacts panel, where the user exports it as .md, .html, .pdf or .docx. ")
	b.WriteString("Tell the user in a sentence what you wrote; do not repeat the document in chat.")
	if res.Findings != "" {
		b.WriteString("\n" + res.Findings)
	}
	if input.Append {
		fmt.Fprintf(&b, "\nTo continue this document, call write_artifact with artifact_id %q and append=true.", res.Artifact.ID)
	} else {
		fmt.Fprintf(&b, "\nTo revise or extend it later, call write_artifact with artifact_id %q.", res.Artifact.ID)
	}
	return result(b.String(), false)
}

// artifactBodiesSince returns the stored text of every document this agent saved
// or extended at or after since. The artifacts table is the source of truth for
// what the card shows, so it is also the source of truth for what the chat text
// must not repeat.
func (s *Service) artifactBodiesSince(agentID string, since int64) []string {
	list, err := s.Core.DB.ListArtifacts(agentID, 20)
	if err != nil {
		log.Printf("chat: echo check could not list artifacts: %v", err)
		return nil
	}
	var bodies []string
	for _, a := range list {
		if a.UpdatedAt < since {
			continue
		}
		full, found, err := s.Core.DB.GetArtifact(a.ID)
		if err != nil || !found {
			continue
		}
		bodies = append(bodies, full.Body)
	}
	return bodies
}

// withoutArtifactEcho removes from text blocks any paragraph that copies a
// document saved during this turn. It returns the blocks and how many
// paragraphs it dropped. A block left empty is removed; non-text blocks (tool
// calls, thinking) pass through untouched. where names the call site in the log.
func (s *Service) withoutArtifactEcho(agentID string, since int64, blocks []Protocol.Block, where string) ([]Protocol.Block, int) {
	docs := s.artifactBodiesSince(agentID, since)
	out, dropped := stripEchoFromBlocks(blocks, docs)
	if dropped > 0 {
		log.Printf("chat: removed %d pasted artifact paragraph(s) from the %s text", dropped, where)
	}
	return out, dropped
}

// stripEchoFromBlocks is the pure half of withoutArtifactEcho.
func stripEchoFromBlocks(blocks []Protocol.Block, docs []string) ([]Protocol.Block, int) {
	if len(docs) == 0 {
		return blocks, 0
	}
	out := make([]Protocol.Block, 0, len(blocks))
	total := 0
	for _, b := range blocks {
		if b.Type == Protocol.BlockText {
			clean, n := Present.StripArtifactEcho(b.Text, docs)
			if n > 0 {
				total += n
				b.Text = clean
				if strings.TrimSpace(clean) == "" {
					continue
				}
			}
		}
		out = append(out, b)
	}
	return out, total
}
