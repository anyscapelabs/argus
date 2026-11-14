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

export default function VoiceNotifier({ status, time, paused = false, onTogglePause, onEnd }: VoiceNotifierProps) {
  const running = status === 'live'
  return (
    <AnimatePresence>
      <motion.div
        initial={{ opacity: 0, y: -8, scale: 0.96 }}
        animate={{ opacity: 1, y: 0, scale: 1 }}
        exit={{ opacity: 0, y: -8, scale: 0.96 }}
        transition={{ duration: 0.18 }}
        className="flex items-center gap-3 rounded-full border border-primary bg-secondary py-2 pl-3 pr-2.5 text-base text-primary shadow-md"
      >
        <span
          className={`flex h-9 w-9 items-center justify-center rounded-full text-black transition-colors ${
            running && !paused ? 'bg-accent' : 'bg-grey-400'
          }`}
        >
          <VoiceIcon size={20} />
        </span>
        <span className="font-medium">{statusLabel(status, paused)}</span>
        {running && <span className="font-mono text-sm tabular-nums text-secondary">{time}</span>}
        <button
          type="button"
          aria-label={paused ? 'Resume the call' : 'Pause the call'}
          onClick={onTogglePause}
          disabled={!running}
          className="flex h-9 w-9 items-center justify-center rounded-full text-secondary transition-colors hover:bg-hover-secondary hover:text-primary disabled:opacity-40 disabled:hover:bg-transparent"
        >
          {paused ? <FiPlay size={18} /> : <FiPause size={18} />}
        </button>
        <button
          type="button"
          aria-label="End the call"
          onClick={onEnd}
          className="flex h-9 w-9 items-center justify-center rounded-full text-secondary transition-colors hover:bg-hover-secondary hover:text-primary"
        >
          <FiSquare size={18} />
        </button>
      </motion.div>
    </AnimatePresence>
  )
}
