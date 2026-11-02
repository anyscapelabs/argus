import { motion, useReducedMotion } from 'framer-motion'

export type ThinkingChip = {
  /** Favicon URL for the site currently shown, if one is known. */
  favicon?: string
  title: string
}

export type ThinkingIndicatorProps = {
  /** Live status text to show instead of the static Thinking… label. */
  text?: string | null
  /** A web-search result site: favicon plus title cycling in the indicator. */
  chip?: ThinkingChip | null
}

export default function ThinkingIndicator({ text, chip }: ThinkingIndicatorProps) {
  const reduceMotion = useReducedMotion()
  return (
    <div className="w-fit max-w-xl rounded-xl bg-secondary px-2 py-1 text-sm font-medium">
      {chip ? (
        /* The key makes a changed site fade in instead of snapping. */
        <motion.div
          key={chip.title}
          initial={reduceMotion ? false : { opacity: 0 }}
          animate={{ opacity: 1 }}
          transition={{ duration: 0.25 }}
          className="flex items-center gap-2 truncate"
        >
          {chip.favicon && (
            <img
              src={chip.favicon}
              alt=""
              className="h-4 w-4 shrink-0 rounded-sm"
              onError={(e) => (e.currentTarget.style.display = 'none')}
            />
          )}
          <span className="shimmer-text truncate">{chip.title}</span>
        </motion.div>
      ) : (
        /* The key makes a changed status fade in instead of snapping. */
        <motion.div
          key={text}
          initial={reduceMotion ? false : { opacity: 0 }}
          animate={{ opacity: 1 }}
          transition={{ duration: 0.15 }}
          className="truncate"
        >
          <span className="shimmer-text">{text || 'Thinking…'}</span>
        </motion.div>
      )}
    </div>
  )
}
