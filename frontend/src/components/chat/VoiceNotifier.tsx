import { motion, AnimatePresence } from 'framer-motion'
import { FiPause, FiPlay, FiSquare } from 'react-icons/fi'
import VoiceIcon from './VoiceIcon'
import type { VoiceStatus } from '../../domains/live/useLiveCall'

type VoiceNotifierProps = {
  status: VoiceStatus
  time: string
  paused?: boolean
  onTogglePause?: () => void
  onEnd?: () => void
}

// statusLabel is the short word beside the icon while the call is starting or
// running. "connecting" covers the socket + setup; "live" means the model is
// listening and the user can speak; "paused" means the mic is muted but the
// call is still open.
function statusLabel(status: VoiceStatus, paused: boolean): string {
  if (paused) return 'Paused'
  switch (status) {
    case 'connecting':
      return 'Connecting…'
    case 'live':
      return 'Voice message'
    default:
      return 'Voice'
  }
}

// VoiceNotifier is the floating card for a live voice call: voice icon on the
// left, the status ("Connecting…" then "Voice message"), and a running clock
// like 02:32. Pause mutes the mic while keeping the conversation open; stop
// ends the call.
export default function VoiceNotifier({ status, time, paused = false, onTogglePause, onEnd }: VoiceNotifierProps) {
  const running = status === 'live'
  return (
    <AnimatePresence>
      <motion.div
        initial={{ opacity: 0, y: 8, scale: 0.96 }}
        animate={{ opacity: 1, y: 0, scale: 1 }}
        exit={{ opacity: 0, y: 8, scale: 0.96 }}
        transition={{ duration: 0.18 }}
        className="flex items-center gap-2.5 rounded-full border border-primary bg-secondary px-3 py-1.5 text-sm text-primary"
      >
        <span
          className={`flex h-6 w-6 items-center justify-center rounded-full text-black transition-colors ${
            running && !paused ? 'bg-accent' : 'bg-grey-400'
          }`}
        >
          <VoiceIcon size={14} />
        </span>
        <span className="font-medium">{statusLabel(status, paused)}</span>
        <span className="font-mono text-xs tabular-nums text-secondary">{time}</span>
        <button
          type="button"
          aria-label={paused ? 'Resume the call' : 'Pause the call'}
          onClick={onTogglePause}
          className="flex h-5 w-5 items-center justify-center rounded-full text-secondary transition-colors hover:bg-hover-secondary hover:text-primary"
        >
          {paused ? <FiPlay size={10} /> : <FiPause size={10} />}
        </button>
        <button
          type="button"
          aria-label="End the call"
          onClick={onEnd}
          className="flex h-5 w-5 items-center justify-center rounded-full text-secondary transition-colors hover:bg-hover-secondary hover:text-primary"
        >
          <FiSquare size={10} />
        </button>
      </motion.div>
    </AnimatePresence>
  )
}
