import { FaArrowLeft } from 'react-icons/fa'
import { IoSearch } from 'react-icons/io5'

export default function SettingsSidebar({ onBack }: { onBack?: () => void }) {
  return (
    <aside className="flex w-64 shrink-0 flex-col border-r border-primary bg-sidebar self-stretch">
      <div className="flex items-center gap-2 p-1">
        <button
          type="button"
          onClick={onBack}
          className="flex h-8 w-8 items-center justify-center rounded-lg text-secondary transition-colors hover:bg-hover-primary hover:text-primary"
        >
          <FaArrowLeft size={14} />
        </button>
        <span className="text-sm text-primary">Settings</span>
      </div>
      <div className="px-2 pt-2">
        <div className="flex items-center gap-2 rounded-xl border border-primary px-2 py-1">
          <IoSearch size={14} className="text-secondary" />
          <input
            type="text"
            placeholder="Search"
            className="w-full bg-transparent text-sm text-primary placeholder:text-secondary focus:outline-none"
          />
        </div>
      </div>
    </aside>
  )
}
