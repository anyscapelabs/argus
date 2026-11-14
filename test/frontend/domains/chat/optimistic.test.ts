import type { InfiniteData } from '@tanstack/react-query'
import { describe, expect, it } from 'vitest'
import type { GetChatResult } from '../../../../frontend/bindings/github.com/anyscapelabs/argus/Services/Shared/models'
import { adoptServerMessageID } from '../../../../frontend/src/domains/chat/optimistic'

function history(ids: string[]): InfiniteData<GetChatResult> {
  return {
    pages: [{
      messages: ids.map((id, i) => ({
        id, agent_id: 'default', seq: i, role: i % 2 ? 'assistant' : 'user', text: id,
        status: 'complete', created_at: i,
      })),
    }],
    pageParams: [0],
  } as unknown as InfiniteData<GetChatResult>
}

describe('adoptServerMessageID', () => {
  it('renames the pending row so its React key survives the refetch', () => {
    const out = adoptServerMessageID(history(['u1', 'a1', 'pending-user-123']), 'u2')
    expect(out?.pages[0].messages?.map((m) => m.id)).toEqual(['u1', 'a1', 'u2'])
  })

  it('renames only the newest pending row', () => {
    const out = adoptServerMessageID(history(['pending-user-1', 'pending-user-2']), 'real')
    expect(out?.pages[0].messages?.map((m) => m.id)).toEqual(['pending-user-1', 'real'])
  })

  it('returns the same object when nothing is pending', () => {
    const data = history(['u1', 'a1'])
    expect(adoptServerMessageID(data, 'u2')).toBe(data)
  })

  it('ignores an empty server id and missing data', () => {
    const data = history(['pending-user-1'])
    expect(adoptServerMessageID(data, '')).toBe(data)
    expect(adoptServerMessageID(undefined, 'x')).toBeUndefined()
  })

  it('does not mutate its input', () => {
    const data = history(['pending-user-1'])
    adoptServerMessageID(data, 'real')
    expect(data.pages[0].messages?.[0].id).toBe('pending-user-1')
  })
})
