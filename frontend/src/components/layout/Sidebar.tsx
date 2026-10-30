import { BsLayoutSidebarInset } from 'react-icons/bs'
import { IoSearch } from 'react-icons/io5'
import { GrSettingsOption } from 'react-icons/gr'
import { LuPlug } from 'react-icons/lu'
import { FiChevronLeft } from 'react-icons/fi'

type SidebarProps = {
  mode?: 'main' | 'settings'
  onToggleSettings?: () => void
}

export default function Sidebar({ mode = 'main', onToggleSettings }: SidebarProps) {
  if (mode === 'settings') {
    return (
      <aside className="flex w-64 shrink-0 flex-col border-r border-primary bg-sidebar self-stretch">
        <div className="flex items-center p-1">
          <button
            type="button"
            onClick={onToggleSettings}
            className="flex h-8 items-center gap-1 rounded-lg px-1 text-grey-300 transition-colors hover:bg-hover-primary hover:text-primary"
          >
            <FiChevronLeft size={16} />
            <span className="text-sm">Back</span>
          </button>
        </div>
        <div className="flex-1" />
      </aside>
    )
  }

  return (
    <aside className="flex w-64 shrink-0 flex-col border-r border-primary bg-sidebar self-stretch">
      <div className="flex items-center justify-between p-1">
        <button
          type="button"
          className="flex h-8 w-8 items-center justify-center rounded-lg text-grey-300 transition-colors hover:bg-hover-primary"
        >
          <BsLayoutSidebarInset size={16} />
        </button>
        <button
          type="button"
          className="flex h-8 w-8 cursor-pointer items-center justify-center rounded-full border border-primary bg-secondary text-primary transition-colors hover:bg-hover-secondary"
        >
          <IoSearch size={16} />
        </button>
      </div>
      <div className="mt-auto flex items-center gap-1 p-1">
        <button
          type="button"
          onClick={onToggleSettings}
          className="flex cursor-pointer items-center justify-center rounded-full border border-primary bg-secondary p-3 text-grey-300 transition-colors hover:bg-hover-secondary hover:text-primary"
        >
          <GrSettingsOption size={16} />
        </button>
        <button
          type="button"
          className="flex cursor-pointer items-center gap-1 rounded-full border border-primary bg-secondary px-3 py-2 text-sm text-primary transition-colors hover:bg-hover-secondary"
        >
          <LuPlug size={14} />
          Connect Apps
        </button>
      </div>
    </aside>
  )
}
