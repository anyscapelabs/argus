import { motion, useReducedMotion } from 'framer-motion'

export type ThinkingIndicatorProps = {
  /** Live status text to show instead of the static Thinking… label. */
  text?: string | null
}

export default function ThinkingIndicator({ text }: ThinkingIndicatorProps) {
  const reduceMotion = useReducedMotion()
  return (
    <div className="w-fit max-w-xl rounded-xl bg-secondary px-2 py-1 text-sm font-medium">
      {/* The key makes a changed status fade in instead of snapping. */}
      <motion.div
        key={text}
        initial={reduceMotion ? false : { opacity: 0 }}
        animate={{ opacity: 1 }}
        transition={{ duration: 0.15 }}
        className="truncate"
      >
        <span className="shimmer-text">{text || 'Thinking…'}</span>
      </motion.div>
    </div>
  )
}
