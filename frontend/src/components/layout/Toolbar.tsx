import { ReactNode } from 'react'
import { Window } from '@wailsio/runtime'
import { IoClose, IoSquareOutline } from 'react-icons/io5'
import { FiMinus } from 'react-icons/fi'
import AgentPill from '../chat/AgentPill'

type ToolbarProps = {
  children?: ReactNode
  className?: string
  agentAvatar?: string | null
  agentName?: string | null
  onAgentClick?: () => void
}

const controlClass =
  'flex h-8 w-8 items-center justify-center rounded-full p-2 text-grey-100 transition-colors'

// Semi-transparent bar: lets the chat show through while keeping controls legible.
// Three grid columns keep the agent pill dead-center while the left and right
// sections share the remaining space equally, so nothing overlaps when the
// window is narrow.
export default function Toolbar({ children, className = '', agentAvatar, agentName, onAgentClick }: ToolbarProps) {
  return (
    <header className={`relative z-20 grid h-12 w-full grid-cols-[1fr_auto_1fr] items-center gap-2 bg-primary/20 p-1 ${className}`}>
      <div className="flex min-w-0 items-center gap-2">{children}</div>
      {onAgentClick ? (
        <div className="max-w-[40vw] min-w-0">
          <AgentPill avatar={agentAvatar} name={agentName} onClick={onAgentClick} />
        </div>
      ) : (
        <div />
      )}
      <div className="flex min-w-0 items-center justify-end gap-0.5">
        <button
          type="button"
          aria-label="Minimize"
          className={`${controlClass} hover:bg-hover-primary`}
          onClick={() => Window.Minimise()}
        >
          <FiMinus size={14} />
        </button>
        <button
          type="button"
          aria-label="Maximize"
          className={`${controlClass} hover:bg-hover-primary`}
          onClick={() => Window.ToggleMaximise()}
        >
          <IoSquareOutline size={14} />
        </button>
        <button
          type="button"
          aria-label="Close"
          className={`${controlClass} hover:bg-danger`}
          onClick={() => Window.Close()}
        >
          <IoClose size={16} />
        </button>
      </div>
    </header>
  )
}