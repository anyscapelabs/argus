import { FaArrowLeft, FaBell } from 'react-icons/fa'
import { IoCardOutline, IoGlobeOutline, IoPersonCircleOutline, IoSearch, IoShieldCheckmarkOutline, IoSunnyOutline } from 'react-icons/io5'
import { LuCircleGauge } from 'react-icons/lu'

export type SettingsTab = 'usage' | 'billing' | 'appearance' | 'profile' | 'notifications' | 'browser' | 'permissions'

export default function SettingsSidebar({
  onBack,
  activeTab,
  onSelectTab,
}: {
  onBack?: () => void
  activeTab?: SettingsTab
  onSelectTab?: (tab: SettingsTab) => void
}) {
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
      <div className="flex flex-col gap-1 px-2 pt-4">
        <span className="px-2 text-xs font-medium uppercase tracking-wide text-secondary">General</span>
        <button
          type="button"
          onClick={() => onSelectTab?.('profile')}
          className={`flex w-full cursor-pointer items-center gap-2 rounded-lg px-2 py-1.5 text-left text-sm text-primary transition-colors hover:bg-hover-primary ${
            activeTab === 'profile' ? 'bg-hover-primary' : ''
          }`}
        >
          <IoPersonCircleOutline size={14} />
          Profile
        </button>
        <button
          type="button"
          onClick={() => onSelectTab?.('appearance')}
          className={`flex w-full cursor-pointer items-center gap-2 rounded-lg px-2 py-1.5 text-left text-sm text-primary transition-colors hover:bg-hover-primary ${
            activeTab === 'appearance' ? 'bg-hover-primary' : ''
          }`}
        >
          <IoSunnyOutline size={14} />
          Appearance
        </button>
        <button
          type="button"
          onClick={() => onSelectTab?.('notifications')}
          className={`flex w-full cursor-pointer items-center gap-2 rounded-lg px-2 py-1.5 text-left text-sm text-primary transition-colors hover:bg-hover-primary ${
            activeTab === 'notifications' ? 'bg-hover-primary' : ''
          }`}
        >
          <FaBell size={14} />
          Notifications
        </button>
      </div>
      <div className="flex flex-col gap-1 px-2 pt-4">
        <span className="px-2 text-xs font-medium uppercase tracking-wide text-secondary">Agents</span>
        <button
          type="button"
          onClick={() => onSelectTab?.('browser')}
          className={`flex w-full cursor-pointer items-center gap-2 rounded-lg px-2 py-1.5 text-left text-sm text-primary transition-colors hover:bg-hover-primary ${
            activeTab === 'browser' ? 'bg-hover-primary' : ''
          }`}
        >
          <IoGlobeOutline size={14} />
          Browser
        </button>
        <button
          type="button"
          onClick={() => onSelectTab?.('permissions')}
          className={`flex w-full cursor-pointer items-center gap-2 rounded-lg px-2 py-1.5 text-left text-sm text-primary transition-colors hover:bg-hover-primary ${
            activeTab === 'permissions' ? 'bg-hover-primary' : ''
          }`}
        >
          <IoShieldCheckmarkOutline size={14} />
          Permissions
        </button>
      </div>
      <div className="flex flex-col gap-1 px-2 pt-4">
        <span className="px-2 text-xs font-medium uppercase tracking-wide text-secondary">Account</span>
        <button
          type="button"
          onClick={() => onSelectTab?.('usage')}
          className={`flex w-full cursor-pointer items-center gap-2 rounded-lg px-2 py-1.5 text-left text-sm text-primary transition-colors hover:bg-hover-primary ${
            activeTab === 'usage' ? 'bg-hover-primary' : ''
          }`}
        >
          <LuCircleGauge size={14} />
          Usage
        </button>
        <button
          type="button"
          onClick={() => onSelectTab?.('billing')}
          className={`flex w-full cursor-pointer items-center gap-2 rounded-lg px-2 py-1.5 text-left text-sm text-primary transition-colors hover:bg-hover-primary ${
            activeTab === 'billing' ? 'bg-hover-primary' : ''
          }`}
        >
          <IoCardOutline size={14} />
          Billing
        </button>
      </div>
      <div className="mt-auto p-3">
        <span className="inline-flex rounded-full border border-primary px-2 py-0.5 text-xs text-secondary">
          v0.1.0
        </span>
      </div>
    </aside>
  )
}
