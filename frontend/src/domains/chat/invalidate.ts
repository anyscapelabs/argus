import type { QueryClient } from '@tanstack/react-query'

// Mutation key of a chat send; the coalescer holds refetches while one is open.
export const SEND_MUTATION_KEY = ['chat', 'send'] as const

export type CoalescedInvalidator = {
  /** Queue a refetch of `key`. Resolves once the batch it joined has settled. */
  invalidate: (key: string[]) => Promise<void>
  dispose: () => void
}

// One run produces a burst of backend events: two chat:message at send, one
// more plus run_finished at the end, and the send mutation's own onSuccess.
// Each used to refetch the whole history, and each refetch re-rendered the
// transcript, which showed up as flicker on send and on receive. This merges
// a burst into a single refetch per key. While a send mutation is still open
// the flush waits, so the optimistic row is re-keyed (adoptServerMessageID)
// before the server's copy arrives.
export function createCoalescedInvalidator(qc: QueryClient, delayMs = 120): CoalescedInvalidator {
  const keys = new Map<string, string[]>()
  let waiters: Array<() => void> = []
  let timer: ReturnType<typeof setTimeout> | null = null

  const flush = () => {
    timer = null
    if (qc.isMutating({ mutationKey: SEND_MUTATION_KEY }) > 0) {
      timer = setTimeout(flush, 100)
      return
    }
    const batch = [...keys.values()]
    const done = waiters
    keys.clear()
    waiters = []
    void Promise.allSettled(batch.map((queryKey) => qc.invalidateQueries({ queryKey }))).then(() =>
      done.forEach((resolve) => resolve()),
    )
  }

  return {
    invalidate(key) {
      keys.set(key.join('/'), key)
      if (!timer) timer = setTimeout(flush, delayMs)
      return new Promise<void>((resolve) => waiters.push(resolve))
    },
    dispose() {
      if (timer) clearTimeout(timer)
      timer = null
      waiters.forEach((resolve) => resolve())
      waiters = []
    },
  }
}
