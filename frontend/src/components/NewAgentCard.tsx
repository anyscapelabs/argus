import { useEffect, useRef, useState } from 'react'
import { IoClose } from 'react-icons/io5'
import AgentGem, { AVATAR_COLORS } from './AgentGem'
import { useCreateAgent } from '../domains/models/hooks'

export type NewAgentCardProps = {
  onClose?: () => void
  onCreated?: (agentID: string) => void
}

export function NewAgentCard({ onClose, onCreated }: NewAgentCardProps) {
  const [name, setName] = useState('')
  const [selectedColor, setSelectedColor] = useState(AVATAR_COLORS[0])
  const [error, setError] = useState<string | null>(null)
  const createAgent = useCreateAgent()
  const nameRef = useRef<HTMLInputElement | null>(null)

  const trimmed = name.trim()
  const canSubmit = trimmed.length > 0 && !createAgent.isPending

  useEffect(() => {
    nameRef.current?.focus()
  }, [])

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose?.()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])

  const submit = async () => {
    if (!canSubmit) return
    setError(null)
    try {
      const created = await createAgent.mutateAsync({ name: trimmed, avatar: selectedColor })
      if (!created.agent_id) throw new Error('The agent was created without an ID')
      onCreated?.(created.agent_id)
      if (!onCreated) onClose?.()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Could not create the agent')
    }
  }

  return (
    <div
      role="dialog"
      aria-label="Create new Agent"
      className="pointer-events-auto w-[500px] max-w-full rounded-xl border border-primary bg-secondary p-2"
    >
      <div className="flex items-center justify-between gap-2">
        <h2 className="text-sm font-medium text-primary">Create new Agent</h2>
        <button
          type="button"
          onClick={onClose}
          aria-label="Close"
          className="flex h-7 w-7 items-center justify-center rounded-full text-secondary transition-colors hover:bg-hover-secondary hover:text-primary"
        >
          <IoClose size={16} />
        </button>
      </div>
      <form onSubmit={(e) => { e.preventDefault(); void submit() }}>
        <div className="flex justify-center pt-4">
          <div className="rounded-full border border-tertiary p-1">
            <AgentGem color={selectedColor} className="h-16 w-16" />
          </div>
        </div>
        <div className="flex justify-center pt-4">
          <input
            ref={nameRef}
            type="text"
            aria-label="Agent name"
            placeholder="Agent Name"
            maxLength={40}
            value={name}
            onChange={(e) => setName(e.target.value)}
            className="w-full max-w-xs rounded-xl border border-tertiary bg-transparent px-3 py-2 text-center text-sm text-primary placeholder:text-secondary outline-none transition-colors focus:border-accent-secondary"
          />
        </div>
        {error && (
          <p role="alert" className="pt-2 text-center text-xs text-danger">{error}</p>
        )}
        <div className="flex justify-center pt-6 pb-4">
          <div className="grid grid-cols-5 gap-3">
            {AVATAR_COLORS.map((color) => {
              const selected = selectedColor === color
              return (
                <button
                  key={color}
                  type="button"
                  aria-label={`Select avatar color ${color}`}
                  aria-pressed={selected}
                  onClick={() => setSelectedColor(color)}
                  className={`h-8 w-8 rounded-full transition-transform ${
                    selected ? 'ring-2 ring-accent ring-offset-2 ring-offset-secondary' : 'hover:scale-110'
                  }`}
                  style={{ backgroundColor: color }}
                />
              )
            })}
          </div>
        </div>
        <div className="flex items-center justify-end gap-2">
          <button
            type="button"
            onClick={onClose}
            className="cursor-pointer rounded-full border border-tertiary px-3 py-1.5 text-sm font-medium text-primary transition-colors hover:bg-hover-secondary"
          >
            Cancel
          </button>
          <button
            type="submit"
            disabled={!canSubmit}
            className="cursor-pointer rounded-full bg-accent px-3 py-1.5 text-sm font-medium text-accent-foreground transition-colors hover:bg-hover-accent disabled:cursor-not-allowed disabled:opacity-50"
          >
            {createAgent.isPending ? 'Creating…' : 'Create'}
          </button>
        </div>
      </form>
    </div>
  )
}
