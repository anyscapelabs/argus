import { useEffect, useState } from 'react'
import { Events } from '@wailsio/runtime'
import { useQueryClient } from '@tanstack/react-query'
import {
  asTakeoverState,
  pendingTakeovers,
  respondTakeover,
  type TakeoverDecision,
  type TakeoverRequestView,
  type TakeoverState,
  type TakeoverStatus,
} from './api'

// The browser handover flow: the live card the backend holds, its events, and
// the decide handler. The card never decides for itself: every button tells
// the backend, and the backend answers with the state the card then shows.
// That is also what makes a failed hand-over visible — the settled status
// arrives with its reason, and the conversation keeps a notice row for it
// whether or not this window is still open.
export function useTakeoverFlow() {
  const [takeover, setTakeover] = useState<TakeoverRequestView | null>(null)
  const [takeoverState, setTakeoverState] = useState<TakeoverState>('requested')
  const [takeoverError, setTakeoverError] = useState<string>()
  const qc = useQueryClient()
  const noticesKey = ['chat', 'default', 'notices']

  useEffect(() => {
    pendingTakeovers('default').then((l) => {
      const v = l && l[0]
      if (!v) return
      setTakeover(v)
      setTakeoverState(v.state)
      setTakeoverError(undefined)
    }).catch(() => {
      // A backend that is not up yet has no card to restore, and the events
      // will raise one the moment it does.
    })
    const unsubs = [
      Events.On('browser:takeover:requested', (event: any) => {
        const payload = (event?.data ?? event) as TakeoverRequestView
        if (!payload?.request_id) return
        // The raise carries the page: screenshot, host and the reason the agent
        // gave, all of which the card needs before the user decides anything.
        setTakeover(payload)
        setTakeoverState(asTakeoverState(payload.state))
        setTakeoverError(undefined)
      }),
      Events.On('browser:takeover:status', (event: any) => {
        const payload = (event?.data ?? event) as TakeoverStatus
        if (!payload?.request_id) return
        const state = asTakeoverState(payload.state)
        // A state the UI does not know is treated as still asking, so the card
        // stays where the user can answer it instead of vanishing on them.
        if (state === 'requested') return
        if (state === 'active') {
          setTakeoverState('active')
          setTakeoverError(payload.error)
          return
        }
        // done | declined. The card's job ends here rather than staying up with
        // a settled pill, because the backend writes a notice row at the same
        // instant and the weave puts it exactly where the ask was: two rows
        // about one handover is one more than the user needs.
        setTakeover(null)
        setTakeoverState('requested')
        setTakeoverError(undefined)
        qc.invalidateQueries({ queryKey: noticesKey })
      }),
    ]
    return () => unsubs.forEach((u) => u && u())
  }, [qc])

  const decide = (decision: TakeoverDecision) => {
    if (!takeover) return
    respondTakeover(takeover.request_id, decision).catch((err) => {
      setTakeoverError(err.message)
    })
  }

  return { takeover, takeoverState, takeoverError, decide }
}
