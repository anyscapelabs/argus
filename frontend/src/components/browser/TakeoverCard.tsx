import { FiCheck, FiGlobe, FiMousePointer } from 'react-icons/fi'
import type { TakeoverRequestView, TakeoverState } from '../../domains/browser/api'

type Props = {
  request: TakeoverRequestView
  state: TakeoverState
  error?: string
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

export default function TakeoverCard({ request, state, error, onTakeover, onDone, onDecline }: Props) {
  const driving = state === 'active'
  const settled = state === 'done' || state === 'declined'
  const where = request.host || 'the page'
  return (
    <div className="w-full max-w-sm min-h-10 p-2 rounded-xl border border-secondary bg-secondary flex items-center gap-3">
      {/* The page as the agent sees it. Without this the ask is a sentence
          about a page; with it the user knows which tab and which wall. */}
      <div className="h-14 w-20 shrink-0 rounded-lg border border-secondary overflow-hidden bg-tertiary">
        {request.screenshot ? (
          <img src={request.screenshot} alt={where} className="h-full w-full object-cover object-top" />
        ) : (
          <div className="flex h-full w-full items-center justify-center">
            <FiGlobe size={16} className="text-secondary" />
          </div>
        )}
      </div>
      <div className="flex min-w-0 flex-1 flex-col">
        <span className="truncate text-sm font-medium text-primary">
          {/* Named by the page rather than by the verb: two cards from the same
              run are told apart by where they were raised, not by why. */}
          {driving ? `You're driving ${where}` : where}
        </span>
        <span className="truncate text-xs font-medium text-secondary" title={request.page_title || undefined}>
          {caption(request, state, error)}
        </span>
      </div>
      {settled ? (
        <span className="flex shrink-0 items-center gap-1 px-3 py-1 rounded-full bg-accent text-black text-sm font-medium">
          <FiCheck size={12} />
          {state === 'done' ? 'Handed back' : 'Declined'}
        </span>
      ) : driving ? (
        <button
          onClick={onDone}
          className="shrink-0 px-3 py-1 rounded-full bg-accent text-black text-sm font-medium hover:bg-hover-accent cursor-pointer"
        >
          I'm done
        </button>
      ) : (
        <div className="flex shrink-0 items-center gap-2">
          <button
            onClick={onDecline}
            className="px-3 py-1 rounded-full text-sm font-medium text-secondary hover:text-primary cursor-pointer"
          >
            Not now
          </button>
          <button
            onClick={onTakeover}
            className="flex items-center gap-1 px-3 py-1 rounded-full bg-accent text-black text-sm font-medium hover:bg-hover-accent cursor-pointer"
          >
            <FiMousePointer size={12} />
            Take over
          </button>
        </div>
      )}
    </div>
  )
}
