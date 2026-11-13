import { useState } from 'react'
import { AnimatePresence, motion } from 'framer-motion'
import { FiChevronDown, FiChevronRight } from 'react-icons/fi'
import VoiceIcon from './VoiceIcon'
import VoiceTranscript from './VoiceTranscript'
import type { VoiceTurn } from '../../domains/live/api'
import { formatVoiceTime } from '../../lib/voice'

// VoiceMessage is a finished voice call in the transcript: the same notifier
// line the call raised while it was live, now a card that opens. Clicking it
// unfolds the call's transcript inline, written with the chat's own bubbles.
export default function VoiceMessage({ seconds, turns }: { seconds: number; turns: VoiceTurn[] }) {
  const [open, setOpen] = useState(false)
  // A record with nothing readable has no transcript to reveal; the line then
  // stays a plain card.
  const openable = turns.length > 0
  return (
    <div className="flex w-full flex-col items-center">
      <button
        type="button"
        onClick={() => openable && setOpen((v) => !v)}
        className={`mx-auto flex w-fit items-center gap-1.5 rounded-md px-2 py-0.5 text-center text-xs text-secondary transition-colors hover:bg-secondary hover:text-primary ${openable ? 'cursor-pointer' : 'cursor-default'}`}
      >
        <div className="flex h-4 w-4 items-center justify-center rounded-md border border-secondary">
          <VoiceIcon size={9} className="text-secondary" />
        </div>
        Voice message · {formatVoiceTime(seconds)}
        {openable &&
          (open ? (
            <FiChevronDown size={12} className="text-secondary" />
          ) : (
            <FiChevronRight size={12} className="text-secondary" />
          ))}
      </button>
      <AnimatePresence initial={false}>
        {open && openable && (
          <motion.div
            initial={{ height: 0, opacity: 0 }}
            animate={{ height: 'auto', opacity: 1 }}
            exit={{ height: 0, opacity: 0 }}
            transition={{ duration: 0.2, ease: 'easeOut' }}
            className="w-full overflow-hidden"
          >
            <div className="mx-auto mt-2 max-h-80 w-full max-w-2xl overflow-y-auto overscroll-contain rounded-xl border border-secondary bg-primary px-3">
              <VoiceTranscript turns={turns} />
            </div>
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  )
}
