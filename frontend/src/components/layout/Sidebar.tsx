import { BsLayoutSidebarInset } from 'react-icons/bs'
import { IoSearch } from 'react-icons/io5'

export default function Sidebar() {
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
    </aside>
  )
}