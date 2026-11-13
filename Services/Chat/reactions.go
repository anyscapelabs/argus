package Chat

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/anyscapelabs/argus/Services/Shared"
)

func (s *Service) SetMessageReaction(agentID, messageID, reaction string) Shared.MessageReactionResult {
	if aerr := s.Core.Ready(); aerr != nil {
		return Shared.MessageReactionResult{Error: aerr}
	}
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		agentID = "default"
	}
	message, found, err := s.Core.DB.GetMessage(strings.TrimSpace(messageID))
	if err != nil {
		return Shared.MessageReactionResult{Error: Shared.NewError(Shared.CodeStorage, err.Error())}
	}
	if !found || message.AgentID != agentID {
		return Shared.MessageReactionResult{Error: Shared.NewError(Shared.CodeNotFound, "That message was not found")}
	}
	if message.Role != "assistant" || message.Kind != "" || message.Status == "streaming" || (strings.TrimSpace(message.Text) == "" && strings.TrimSpace(message.ErrorMessage) == "") {
		return Shared.MessageReactionResult{Error: Shared.NewError(Shared.CodeBadInput, "Only completed assistant messages can be reacted to")}
	}
	reaction = strings.TrimSpace(reaction)
	if utf8.RuneCountInString(reaction) > 16 {
		return Shared.MessageReactionResult{Error: Shared.NewError(Shared.CodeBadInput, "Reaction is too long")}
	}
	for _, r := range reaction {
		if unicode.IsControl(r) || r == '\n' || r == '\r' {
			return Shared.MessageReactionResult{Error: Shared.NewError(Shared.CodeBadInput, "Reaction contains invalid characters")}
		}
	}
	if err := s.Core.DB.SetMessageReaction(message.ID, reaction, reactionSignal(reaction)); err != nil {
		return Shared.MessageReactionResult{Error: Shared.NewError(Shared.CodeStorage, err.Error())}
	}
	return Shared.MessageReactionResult{Ok: true, MessageID: message.ID, Reaction: reaction}
}

func reactionSignal(reaction string) int {
	normalized := strings.ReplaceAll(reaction, "\ufe0f", "")
	switch normalized {
	case "👍", "👍🏻", "👍🏼", "👍🏽", "👍🏾", "👍🏿":
		return 1
	case "👎", "👎🏻", "👎🏼", "👎🏽", "👎🏾", "👎🏿":
		return -1
	default:
		return 0
	}
}
