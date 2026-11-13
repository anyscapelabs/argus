import { FiCheck, FiGlobe, FiMousePointer } from 'react-icons/fi'
import type { TakeoverRequestView, TakeoverState } from '../../domains/browser/api'

type Props = {
  request: TakeoverRequestView
  state: TakeoverState
  error?: string
  busy?: boolean
  onTakeover: () => void
  onDone: () => void
  onDecline: () => void
}

// caption turns the reason into the sentence the user reads. "captcha" is the
// one word the backend raises itself, from a pattern match on the page; every
// other reason is the agent's own explanation and is shown as it was written.
function caption(request: TakeoverRequestView, state: TakeoverState, error?: string): string {
  if (error) return error
  if (state === 'active') return 'Click into Chrome and finish the step, then hand it back'
  const reason = (request.reason ?? '').trim()
  if (!reason) return 'The agent needs something only you can do on this page'
  if (reason.toLowerCase() === 'captcha') return "A page is asking you to prove you're human"
  return reason
}

export default function TakeoverCard({ request, state, error, busy = false, onTakeover, onDone, onDecline }: Props) {
  const driving = state === 'active'
  const settled = state === 'done' || state === 'declined'
  const where = request.host || 'the page'
  return (
    <div className="flex w-full max-w-2xl flex-col items-stretch gap-3 rounded-xl border border-secondary bg-secondary p-3">
      {/* The page as the agent sees it. Without this the ask is a sentence
          about a page; with it the user knows which tab and which wall. */}
      <div className="aspect-[64/45] w-full overflow-hidden rounded-lg border border-secondary bg-tertiary">
        {request.screenshot ? (
          <img src={request.screenshot} alt={`${request.page_title || where} browser preview`} className="h-full w-full object-contain object-top" />
        ) : (
          <div className="flex h-full w-full items-center justify-center">
            <FiGlobe size={28} className="text-secondary" />
          </div>
        )}
      </div>
      <div className="min-w-0">
        <div className="truncate text-sm font-medium text-primary">
          {/* Named by the page rather than by the verb: two cards from the same
              run are told apart by where they were raised, not by why. */}
          {driving ? `You're driving ${where}` : where}
        </div>
        {request.page_title && <div className="truncate text-xs text-secondary">{request.page_title}</div>}
        <div className="mt-1 break-words text-sm text-secondary">
          {caption(request, state, error)}
        </div>
      </div>
      <div className="flex w-full items-center justify-end gap-2">
      {settled ? (
        <span className="flex shrink-0 items-center gap-1 px-3 py-1 rounded-full bg-accent text-black text-sm font-medium">
          <FiCheck size={12} />
          {state === 'done' ? 'Handed back' : 'Declined'}
        </span>
      ) : driving ? (
        <button
          onClick={onDone}
          type="button"
          disabled={busy}
          className="shrink-0 px-3 py-1 rounded-full bg-accent text-black text-sm font-medium hover:bg-hover-accent cursor-pointer"
        >
          I'm done
        </button>
      ) : (
        <div className="flex items-center gap-2">
          <button
            type="button"
            onClick={onDecline}
            disabled={busy}
            className="px-3 py-1 rounded-full text-sm font-medium text-secondary hover:text-primary cursor-pointer"
          >
            Deny
          </button>
          <button
            type="button"
            onClick={onTakeover}
            disabled={busy}
            className="flex items-center gap-1 px-3 py-1 rounded-full bg-accent text-black text-sm font-medium hover:bg-hover-accent cursor-pointer"
          >
            <FiMousePointer size={12} />
            Take Over
          </button>
        </div>
      )}
      </div>
    </div>
  )
}
