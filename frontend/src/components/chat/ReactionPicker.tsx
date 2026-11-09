import { useEffect, useRef, useState } from 'react'
import data from '@emoji-mart/data'
import Picker from '@emoji-mart/react'
import { init } from 'emoji-mart'
import { FiPlus } from 'react-icons/fi'
import { BsEmojiSunglasses } from 'react-icons/bs'

void init({ data })

const REACTION_EMOJIS = [
  { id: '+1', label: 'Thumbs up', native: '👍' },
  { id: 'heart', label: 'Heart', native: '❤️' },
  { id: 'joy', label: 'Laughing', native: '😂' },
  { id: 'open_mouth', label: 'Surprised', native: '😮' },
  { id: 'cry', label: 'Sad', native: '😢' },
  { id: 'fire', label: 'Fire', native: '🔥' },
]

const normalizeEmoji = (emoji: string) => emoji.replace(/\uFE0F|\u200D/g, '')

type ReactionPickerProps = {
  reaction: string | null
  onReactionChange: (emoji: string | null) => void
  onOpenChange?: (open: boolean) => void
}

export function ReactionPicker({ reaction, onReactionChange, onOpenChange }: ReactionPickerProps) {
  const [open, setOpen] = useState(false)
  const [showEmojiPicker, setShowEmojiPicker] = useState(false)
  const ref = useRef<HTMLDivElement>(null)

  const setOpenState = (next: boolean) => {
    setOpen(next)
    if (!next) setShowEmojiPicker(false)
    onOpenChange?.(next)
  }

  useEffect(() => {
    if (!open) return
    const onPointerDown = (event: MouseEvent) => {
      if (!ref.current?.contains(event.target as Node)) {
        setOpenState(false)
      }
    }
    document.addEventListener('mousedown', onPointerDown)
    return () => document.removeEventListener('mousedown', onPointerDown)
  }, [open])

  const selectReaction = (emoji: string) => {
    const nextReaction =
      reaction && normalizeEmoji(reaction) === normalizeEmoji(emoji) ? null : emoji
    onReactionChange(nextReaction)
    setOpenState(false)
  }

  return (
    <div ref={ref} className="relative">
      <button
        type="button"
        onClick={() => setOpenState(!open)}
        aria-label="React"
        aria-expanded={open}
        className={`flex h-7 w-7 items-center justify-center rounded-lg transition-colors hover:bg-hover-secondary hover:text-primary ${open ? 'bg-hover-secondary text-primary' : 'text-secondary'}`}
      >
        <BsEmojiSunglasses size={14} />
      </button>
      {open && !showEmojiPicker && (
        <div className="absolute bottom-full left-1/2 z-20 mb-2 -translate-x-1/2 rounded-full border border-primary bg-primary px-2 py-1">
          <div className="flex items-center gap-1">
            {REACTION_EMOJIS.map(({ id, label, native }) => {
              const active = reaction != null && normalizeEmoji(reaction) === normalizeEmoji(native)
              return (
                <button
                  key={id}
                  type="button"
                  onClick={() => selectReaction(native)}
                  aria-label={active ? `Remove ${label} reaction` : `React with ${label}`}
                  aria-pressed={active}
                  className={`flex h-8 w-8 items-center justify-center rounded-full transition-colors ${
                    active ? 'bg-hover-secondary' : 'hover:bg-hover-secondary'
                  }`}
                >
                  <em-emoji id={id} size="20" set="native" />
                </button>
              )
            })}
            <button
              type="button"
              onClick={() => setShowEmojiPicker(true)}
              aria-label="More reactions"
              className="flex h-8 w-8 items-center justify-center rounded-full bg-primary text-secondary transition-colors hover:bg-hover-secondary hover:text-primary"
            >
              <FiPlus size={14} />
            </button>
          </div>
        </div>
      )}
      {open && showEmojiPicker && (
        <div className="absolute bottom-full left-1/2 z-20 mb-2 -translate-x-1/2 overflow-hidden rounded-xl border border-primary shadow-lg">
          <Picker
            data={data}
            onEmojiSelect={(emoji: { native?: string }) => {
              if (!emoji?.native) return
              selectReaction(emoji.native)
            }}
            previewPosition="none"
            skinTonePosition="none"
            set="native"
            theme="dark"
          />
        </div>
      )}
    </div>
  )
}

type ReactionBadgeProps = {
  reaction: string | null
}

export function ReactionBadge({ reaction }: ReactionBadgeProps) {
  if (!reaction) return null
  return (
    <span className="absolute -bottom-2 -right-2 z-10 flex items-center justify-center rounded-full bg-primary p-1 text-sm">
      {reaction}
    </span>
  )
}
