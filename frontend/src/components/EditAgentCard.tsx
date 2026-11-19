import { useEffect, useRef, useState } from 'react'
import { IoClose } from 'react-icons/io5'
import AgentGem, { AVATAR_COLORS, isAvatarColor } from './AgentGem'
import { useSaveAgentProfile } from '../domains/models/hooks'
import type { AgentProfile } from '../../bindings/github.com/anyscapelabs/argus/Services/Shared/models.js'

type EditAgentCardProps = {
  profile: AgentProfile
  onClose?: () => void
  onSaved?: () => void
}

export default function EditAgentCard({ profile, onClose, onSaved }: EditAgentCardProps) {
  const [name, setName] = useState(profile.agent_name ?? '')
  const [selectedColor, setSelectedColor] = useState(
    isAvatarColor(profile.avatar) ? (profile.avatar as string) : AVATAR_COLORS[0],
  )
  const [error, setError] = useState<string | null>(null)
  const saveProfile = useSaveAgentProfile()
  const nameRef = useRef<HTMLInputElement | null>(null)

  const trimmed = name.trim()
  const canSubmit = trimmed.length > 0 && !saveProfile.isPending

  useEffect(() => {
    nameRef.current?.focus()
    nameRef.current?.select()
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
      await saveProfile.mutateAsync({
        ...profile,
        agent_name: trimmed,
        avatar: selectedColor,
      })
      onSaved?.()
      onClose?.()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Could not save the agent')
    }
  }

  return (
    <div
      role="dialog"
      aria-label="Edit Agent"
      className="pointer-events-auto w-[500px] max-w-full rounded-xl border border-primary bg-secondary p-2"
    >
      <div className="flex items-center justify-between gap-2">
        <h2 className="text-sm font-medium text-primary">Edit Agent</h2>
        <button
          type="button"
          onClick={onClose}
          aria-label="Close"
          className="flex h-7 w-7 items-center justify-center rounded-full text-secondary transition-colors hover:bg-hover-secondary hover:text-primary"
        >
          <IoClose size={16} />
        </button>
      </div>
      <form
        onSubmit={(e) => {
          e.preventDefault()
          void submit()
        }}
      >
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
          <p role="alert" className="pt-2 text-center text-xs text-danger">
            {error}
          </p>
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
            {saveProfile.isPending ? 'Saving…' : 'Save'}
          </button>
        </div>
      </form>
    </div>
  )
}
