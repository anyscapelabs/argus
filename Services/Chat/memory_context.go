package Chat

import (
	"github.com/anyscapelabs/argus/Protocol"
	"github.com/anyscapelabs/argus/Services/Memory"
)

func prependMemoryContext(messages []Protocol.Message, selection Memory.Selection) []Protocol.Message {
	if selection.Rendered == "" {
		return messages
	}
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != Protocol.RoleUser {
			continue
		}
		messages[i].Blocks = append(
			[]Protocol.Block{{Type: Protocol.BlockText, Text: selection.Rendered}},
			messages[i].Blocks...,
		)
		break
	}
	return messages
}
