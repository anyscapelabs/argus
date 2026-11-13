import { FormEvent, useState } from 'react'
import { FiArrowUp, FiHelpCircle } from 'react-icons/fi'
import type { AskUserRequestView } from '../../domains/askUser/api'

export default function AskUserCard({
  request,
  submitting,
  error,
  onAnswer,
}: {
  request: AskUserRequestView
  submitting: boolean
  error?: string
  onAnswer: (answer: string) => void
}) {
  const [custom, setCustom] = useState('')
  const submit = (event: FormEvent) => {
    event.preventDefault()
    const answer = custom.trim()
    if (answer) onAnswer(answer)
  }
  return (
    <div className="w-full max-w-xl rounded-xl border border-secondary bg-secondary p-3">
      <div className="flex items-start gap-2">
        <div className="mt-0.5 flex h-7 w-7 shrink-0 items-center justify-center rounded-lg border border-secondary bg-primary">
          <FiHelpCircle size={14} className="text-secondary" />
        </div>
        <div className="min-w-0 flex-1">
          <div className="text-sm font-medium text-primary">{request.question}</div>
          <div className="mt-3 flex flex-wrap gap-2">
            {(request.choices ?? []).map((choice) => (
              <button
                key={choice}
                type="button"
                disabled={submitting}
                onClick={() => onAnswer(choice)}
                className="rounded-full border border-primary bg-primary px-3 py-1.5 text-xs text-primary transition-colors hover:bg-hover-primary disabled:cursor-not-allowed disabled:opacity-50"
              >
                {choice}
              </button>
            ))}
          </div>
          <form onSubmit={submit} className="mt-3 flex items-center gap-2 rounded-lg border border-primary bg-primary p-1 pl-2">
            <input
              value={custom}
              onChange={(event) => setCustom(event.target.value)}
              disabled={submitting}
              placeholder="Type your own answer"
              aria-label="Custom answer"
              className="min-w-0 flex-1 bg-transparent text-sm text-primary placeholder:text-secondary focus:outline-none"
            />
            <button
              type="submit"
              disabled={submitting || !custom.trim()}
              aria-label="Send answer"
              className="flex h-7 w-7 items-center justify-center rounded-md bg-accent text-black disabled:cursor-not-allowed disabled:opacity-40"
            >
              <FiArrowUp size={14} />
            </button>
          </form>
          {error && <div aria-live="polite" className="mt-2 text-xs text-red-400">{error}</div>}
        </div>
      </div>
    </div>
  )
}
