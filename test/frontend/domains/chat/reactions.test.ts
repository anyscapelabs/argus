import type { InfiniteData } from '@tanstack/react-query'
import { describe, expect, it } from 'vitest'
import type { GetChatResult } from '../../../../frontend/bindings/github.com/anyscapelabs/argus/Services/Shared/models'
import { messageReaction, patchMessageReaction, rollbackMessageReaction } from '../../../../frontend/src/domains/chat/reactions'

function history(reaction = ''): InfiniteData<GetChatResult> {
  return {
    pages: [{
      messages: [{
        id: 'answer-1', agent_id: 'default', seq: 2, role: 'assistant', text: 'Answer',
        status: 'complete', created_at: 1, reaction,
      }],
    }],
    pageParams: [0],
  }
}

describe('optimistic message reactions', () => {
  it('rolls a failed mutation back to the backend value', () => {
    const optimistic = patchMessageReaction(history('❤️'), 'answer-1', '👍')
    const rolledBack = rollbackMessageReaction(optimistic, 'answer-1', '👍', '❤️')
    expect(messageReaction(rolledBack, 'answer-1')).toBe('❤️')
  })

  it('does not overwrite a newer optimistic choice', () => {
    const first = patchMessageReaction(history(), 'answer-1', '👍')
    const newer = patchMessageReaction(first, 'answer-1', '👎')
    const staleRollback = rollbackMessageReaction(newer, 'answer-1', '👍', '')
    expect(staleRollback).toBe(newer)
    expect(messageReaction(staleRollback, 'answer-1')).toBe('👎')
  })
})
