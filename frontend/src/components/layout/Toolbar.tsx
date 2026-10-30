import { ReactNode } from 'react'
import { Window } from '@wailsio/runtime'
import { IoClose, IoSquareOutline } from 'react-icons/io5'
import { FiMinus } from 'react-icons/fi'

type ToolbarProps = {
  children?: ReactNode
  className?: string
}

const controlClass =
  'flex h-8 w-8 items-center justify-center rounded-full p-2 text-grey-100 transition-colors hover:bg-hover-primary'

const closeClass =
  'flex h-8 w-8 items-center justify-center rounded-full p-2 text-grey-100 transition-colors hover:bg-[#ef4444]'

export default function Toolbar({ children, className = '' }: ToolbarProps) {
  return (
    <header
      className={`flex h-12 w-full items-center gap-2 bg-primary p-1 ${className}`}
    >
      <div className="flex flex-1 items-center gap-2">{children}</div>
      <div className="flex flex-1 items-center justify-end gap-0.5">
        <button
          type="button"
          aria-label="Minimize"
          className={controlClass}
          onClick={() => Window.Minimise()}
        >
          <FiMinus size={14} />
        </button>
        <button
          type="button"
          aria-label="Maximize"
          className={controlClass}
          onClick={() => Window.ToggleMaximise()}
        >
          <IoSquareOutline size={14} />
        </button>
        <button
          type="button"
          aria-label="Close"
          className={closeClass}
          onClick={() => Window.Close()}
        >
          <IoClose size={16} />
        </button>
      </div>
    </header>
  )
}