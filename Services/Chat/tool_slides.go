package Chat

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/anyscapelabs/argus/Protocol"
)

// runCreateSlidesTool is the create_slides handler. It validates the deck with
// the slides engine, stores it as an artifact of kind "deck", and tells the
// model what landed so the reply can name the deck and its size without
// repeating the deck itself.
func (s *Service) runCreateSlidesTool(
	call Protocol.ToolCall,
	agentID string,
	result func(text string, isError bool) Protocol.Block,
	activity func(kind, command string),
) Protocol.Block {
	if s.Artifacts == nil {
		return result("slide decks are not available in this build", true)
	}
	var input struct {
		Title  string            `json:"title"`
		Theme  json.RawMessage   `json:"theme"`
		Slides []json.RawMessage `json:"slides"`
	}
	if err := json.Unmarshal([]byte(call.Input), &input); err != nil {
		return result("invalid tool input: could not parse the create_slides arguments", true)
	}
	if strings.TrimSpace(input.Title) == "" {
		return result("invalid tool input: title is required — the name of the deck", true)
	}
	if len(input.Slides) == 0 {
		return result("invalid tool input: slides is required — at least one slide", true)
	}
	activity("tool_started", strings.TrimSpace(input.Title))
	res := s.Artifacts.SaveDeck(agentID, call.Input)
	activity("tool_finished", strings.TrimSpace(input.Title))
	// The deck may be saved even when the .pptx could not be built (no
	// LibreOffice): that case has an artifact id and an error note together.
	if res.Error != nil && res.Artifact.ID == "" {
		return result("could not save the deck: "+res.Error.Message, true)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Saved %q as a slide deck (id %s, %d slides). ",
		res.Artifact.Title, shortID(res.Artifact.ID), res.SlideCount)
	if res.Error != nil {
		// The deck landed but the .pptx could not be built (no LibreOffice).
		b.WriteString("The slides are in the Artifacts panel, but the .pptx could not be generated on this machine. " + res.Error.Message)
	} else {
		b.WriteString("It is in the Artifacts panel, where the user saves it as a .pptx.")
	}
	b.WriteString(" Tell the user in a sentence what you made; do not repeat the deck in chat.")
	return result(b.String(), false)
}
