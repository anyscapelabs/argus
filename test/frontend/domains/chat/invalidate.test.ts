import { QueryClient } from '@tanstack/react-query'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createCoalescedInvalidator } from '../../../../frontend/src/domains/chat/invalidate'

describe('createCoalescedInvalidator', () => {
  let qc: QueryClient
  beforeEach(() => {
    vi.useFakeTimers()
    qc = new QueryClient()
    vi.spyOn(qc, 'invalidateQueries').mockResolvedValue(undefined)
  })
  afterEach(() => {
    vi.useRealTimers()
    vi.restoreAllMocks()
  })

  const history = ['chat', 'default', 'history']
  const usage = ['chat', 'default', 'usage']

  it('turns a burst of events into one refetch per key', async () => {
    const c = createCoalescedInvalidator(qc, 120)
    void c.invalidate(history)
    void c.invalidate(usage)
    void c.invalidate(history)
    void c.invalidate(history)
    await vi.advanceTimersByTimeAsync(120)
    expect(qc.invalidateQueries).toHaveBeenCalledTimes(2)
  })

  it('resolves the caller once the batch has settled', async () => {
    const c = createCoalescedInvalidator(qc, 120)
    let done = false
    void c.invalidate(history).then(() => { done = true })
    await vi.advanceTimersByTimeAsync(119)
    expect(done).toBe(false)
    await vi.advanceTimersByTimeAsync(2)
    expect(done).toBe(true)
  })

  it('waits while a send mutation is still open', async () => {
    const mutating = vi.spyOn(qc, 'isMutating').mockReturnValue(1)
    const c = createCoalescedInvalidator(qc, 120)
    void c.invalidate(history)
    await vi.advanceTimersByTimeAsync(500)
    expect(qc.invalidateQueries).not.toHaveBeenCalled()
    mutating.mockReturnValue(0)
    await vi.advanceTimersByTimeAsync(120)
    expect(qc.invalidateQueries).toHaveBeenCalledTimes(1)
  })

  it('releases waiters on dispose so nothing hangs', async () => {
    const c = createCoalescedInvalidator(qc, 120)
    let done = false
    void c.invalidate(history).then(() => { done = true })
    c.dispose()
    await vi.advanceTimersByTimeAsync(0)
    expect(done).toBe(true)
    expect(qc.invalidateQueries).not.toHaveBeenCalled()
  })
})
