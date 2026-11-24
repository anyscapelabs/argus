import { FormEvent, useState } from 'react'
import { FiArrowUp, FiCheck } from 'react-icons/fi'
import type { AskUserRequestView } from '../../domains/askUser/api'

export default function AskUserCard({
  request,
  question,
  answer,
  submitting = false,
  error,
  onAnswer,
}: {
  request?: AskUserRequestView
  question?: string
  answer?: string
  submitting?: boolean
  error?: string
  onAnswer?: (answer: string) => void
}) {
  const [custom, setCustom] = useState('')
  const text = question ?? request?.question ?? ''
  const choices = request?.choices ?? []
  const submit = (event: FormEvent) => {
    event.preventDefault()
    const trimmed = custom.trim()
    if (trimmed) onAnswer?.(trimmed)
  }

  // A settled card is the transcript's record of the exchange: the question and
  // the answer that was picked, with nothing left to click. Rendering the
  // choices again would invite a second answer to a question already resolved.
  if (answer !== undefined) {
    return (
      <div className="w-full max-w-xl rounded-xl bg-secondary p-3">
        <div className="text-sm font-medium text-primary">{text}</div>
        <div className="mt-3 pb-1">
          <div className="flex w-full items-center gap-3 rounded-full px-2 py-1.5 text-left text-sm text-primary">
            <span className="flex h-6 w-6 shrink-0 items-center justify-center rounded-full bg-accent text-accent-foreground">
              <FiCheck size={12} />
            </span>
            <span className="min-w-0 flex-1 break-words">{answer}</span>
          </div>
        </div>
      </div>
    )
  }

  return (
    <div className="w-full max-w-xl rounded-xl bg-secondary p-3">
      <div className="text-sm font-medium text-primary">{text}</div>
      <div className="mt-3 pb-1">
        {choices.map((choice, index) => (
          <button
            key={choice}
            type="button"
            disabled={submitting}
            onClick={() => onAnswer?.(choice)}
            className="flex w-full items-center gap-3 rounded-full px-2 py-1.5 text-left text-sm text-primary transition-colors hover:bg-hover-primary disabled:cursor-not-allowed disabled:opacity-50"
          >
            <span className="flex h-6 w-6 shrink-0 items-center justify-center rounded-full bg-primary text-xs text-secondary">
              {index + 1}
            </span>
            <span className="min-w-0 flex-1">{choice}</span>
          </button>
        ))}
      </div>
      <form onSubmit={submit} className="flex items-center gap-2 rounded-full border border-secondary p-1 pl-2">
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
          className="flex h-7 w-7 items-center justify-center rounded-full bg-accent text-accent-foreground disabled:cursor-not-allowed disabled:opacity-40"
        >
          <FiArrowUp size={14} />
        </button>
      </form>
      {error && <div aria-live="polite" className="mt-2 text-xs text-danger">{error}</div>}
    </div>
  )
}
