import { motion } from 'framer-motion'
import { FiChevronRight } from 'react-icons/fi'
import AgentAvatar from '../AgentAvatar'
import { AVATAR_COLORS } from '../AgentGem'

type AgentPillProps = {
  avatar?: string | null
  name?: string | null
  onClick: () => void
}

// The agent's avatar and name, floating at the top of a page. Clicking it opens
// the agent's side panel; the chevron slides in on hover.
export default function AgentPill({ avatar, name, onClick }: AgentPillProps) {
  const label = name || 'Argus'
  return (
    <motion.button
      type="button"
      onClick={onClick}
      transition={{ type: 'spring', stiffness: 300, damping: 20 }}
      className="group flex max-w-full min-w-0 cursor-pointer items-center gap-2 rounded-full bg-[rgba(38,38,38,0.72)] px-3 py-1 shadow-sm backdrop-blur-xl backdrop-saturate-150 transition-colors hover:bg-[rgba(38,38,38,0.85)]"
    >
      <AgentAvatar avatar={avatar || AVATAR_COLORS[0]} name={label} size={28} />
      <span className="truncate text-sm font-medium text-primary">{label}</span>
      <FiChevronRight
        size={14}
        className="w-0 -translate-x-1 overflow-hidden opacity-0 transition-[width,transform,opacity] duration-300 ease-out group-hover:w-4 group-hover:translate-x-0 group-hover:opacity-100"
      />
    </motion.button>
  )
}
