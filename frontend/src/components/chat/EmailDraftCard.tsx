import { FiCheck, FiX } from 'react-icons/fi'

export type EmailDraft = {
  to: string[]
  cc?: string[]
  subject: string
  body: string
  from?: string
}

export type EmailDraftStatus = 'pending' | 'sending' | 'sent' | 'denied'

export type EmailDraftCardProps = {
  draft: EmailDraft
  agentName?: string
  status?: EmailDraftStatus
  onSend?: () => void
  onDeny?: () => void
}

function Recipients({ label, people }: { label: string; people: string[] }) {
  if (people.length === 0) return null
  return (
    <div className="flex items-start gap-3 px-3 py-1.5">
      <span className="w-12 shrink-0 pt-0.5 text-xs text-secondary">{label}</span>
      <div className="flex min-w-0 flex-1 flex-wrap gap-1">
        {people.map((p) => (
          <span key={p} className="max-w-full truncate rounded-full border border-secondary px-2 py-0.5 text-xs text-primary">
            {p}
          </span>
        ))}
      </div>
    </div>
  )
}

// A drafted email waiting for a human decision. Same shell and buttons as
// PermissionCard: Deny is the red full-radius pill, Send is the accent one. The
// agent never sends on its own; once a choice is made the buttons give way to
// the outcome so the card reads as a record, not a live prompt.
export default function EmailDraftCard({ draft, agentName, status = 'pending', onSend, onDeny }: EmailDraftCardProps) {
  const who = agentName ?? 'The agent'
  const sending = status === 'sending'
  const resolved = status === 'sent' || status === 'denied'
  const disabled = sending || resolved

  return (
    <div className="flex w-full max-w-xl flex-col gap-1 rounded-xl border border-primary bg-secondary p-2">
      <div className="flex items-center gap-2">
        <img src="/gmail.png" alt="" className="h-4 w-4 rounded-md object-contain" draggable={false} />
        <span className="text-xs text-primary">Email draft</span>
      </div>
      <span className="mt-1 text-sm text-primary">
        {status === 'sent'
          ? 'Email sent'
          : status === 'denied'
            ? `You declined to send this email`
            : `${who} wants to send this email. Review it first.`}
      </span>

      <div className="mt-1 divide-y divide-[#363636] rounded-md border border-secondary">
        {draft.from && (
          <div className="flex items-start gap-3 px-3 py-1.5">
            <span className="w-12 shrink-0 text-xs text-secondary">From</span>
            <span className="min-w-0 flex-1 truncate text-xs text-primary">{draft.from}</span>
          </div>
        )}
        <Recipients label="To" people={draft.to} />
        <Recipients label="Cc" people={draft.cc ?? []} />
        <div className="flex items-start gap-3 px-3 py-1.5">
          <span className="w-12 shrink-0 text-xs text-secondary">Subject</span>
          <span className="min-w-0 flex-1 break-words text-xs font-medium text-primary">{draft.subject || '(no subject)'}</span>
        </div>
        <div className="max-h-56 select-text overflow-y-auto whitespace-pre-wrap break-words px-3 py-2 text-sm leading-relaxed text-primary [scrollbar-width:thin]">
          {draft.body}
        </div>
      </div>

      <div className="mt-1 flex items-center justify-end gap-2">
        {resolved ? (
          <span className={`flex items-center gap-1 text-xs ${status === 'sent' ? 'text-primary' : 'text-secondary'}`}>
            {status === 'sent' ? <FiCheck size={12} /> : <FiX size={12} />}
            {status === 'sent' ? 'Sent' : 'Denied'}
          </span>
        ) : (
          <>
            <button
              type="button"
              disabled={disabled}
              onClick={onDeny}
              className="cursor-pointer rounded-full bg-red-500 px-3 py-1 text-xs font-medium text-white transition-colors hover:bg-red-600 disabled:cursor-not-allowed disabled:opacity-50"
            >
              Deny
            </button>
            <button
              type="button"
              disabled={disabled}
              onClick={onSend}
              className="cursor-pointer rounded-full bg-accent px-3 py-1 text-xs font-medium text-black transition-colors hover:bg-hover-accent disabled:cursor-not-allowed disabled:opacity-50"
            >
              {sending ? 'Sending…' : 'Send'}
            </button>
          </>
        )}
      </div>
    </div>
  )
}
