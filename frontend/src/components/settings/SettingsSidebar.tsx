import { useMemo, useState } from 'react'
import { FaArrowLeft, FaBell } from 'react-icons/fa'
import { IoCardOutline, IoGlobeOutline, IoPersonCircleOutline, IoSearch, IoShieldCheckmarkOutline, IoSunnyOutline } from 'react-icons/io5'
import { LuCircleGauge } from 'react-icons/lu'
import type { ComponentType } from 'react'

export type SettingsTab = 'usage' | 'billing' | 'appearance' | 'profile' | 'notifications' | 'browser' | 'permissions'

type SettingsItem = {
  tab: SettingsTab
  label: string
  icon: ComponentType<{ size?: number; className?: string }>
}

const SECTIONS: { title: string; items: SettingsItem[] }[] = [
  {
    title: 'General',
    items: [
      { tab: 'profile', label: 'Profile', icon: IoPersonCircleOutline },
      { tab: 'appearance', label: 'Appearance', icon: IoSunnyOutline },
      { tab: 'notifications', label: 'Notifications', icon: FaBell },
    ],
  },
  {
    title: 'Agents',
    items: [
      { tab: 'browser', label: 'Browser', icon: IoGlobeOutline },
      { tab: 'permissions', label: 'Permissions', icon: IoShieldCheckmarkOutline },
    ],
  },
  {
    title: 'Account',
    items: [
      { tab: 'usage', label: 'Usage', icon: LuCircleGauge },
      { tab: 'billing', label: 'Billing', icon: IoCardOutline },
    ],
  },
]

export default function SettingsSidebar({
  onBack,
  activeTab,
  onSelectTab,
}: {
  onBack?: () => void
  activeTab?: SettingsTab
  onSelectTab?: (tab: SettingsTab) => void
}) {
  const [query, setQuery] = useState('')
  const trimmed = query.trim().toLowerCase()
  const sections = useMemo(() => {
    if (!trimmed) return SECTIONS
    return SECTIONS.map((section) => ({
      ...section,
      items: section.items.filter((item) => item.label.toLowerCase().includes(trimmed)),
    })).filter((section) => section.items.length > 0)
  }, [trimmed])

  return (
    <aside className="flex w-64 max-w-[30vw] shrink-0 flex-col border-r border-primary bg-sidebar self-stretch">
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
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Escape') setQuery('')
            }}
            className="w-full bg-transparent text-sm text-primary placeholder:text-secondary focus:outline-none"
          />
        </div>
      </div>
      {sections.length === 0 ? (
        <p className="px-4 pt-6 text-sm text-secondary">No settings found</p>
      ) : (
        sections.map((section) => (
          <div key={section.title} className="flex flex-col gap-1 px-2 pt-4">
            <span className="px-2 text-xs font-medium uppercase tracking-wide text-secondary">{section.title}</span>
            {section.items.map((item) => {
              const Icon = item.icon
              return (
                <button
                  key={item.tab}
                  type="button"
                  onClick={() => onSelectTab?.(item.tab)}
                  className={`flex w-full cursor-pointer items-center gap-2 rounded-lg px-2 py-1.5 text-left text-sm text-primary transition-colors hover:bg-hover-primary ${
                    activeTab === item.tab ? 'bg-hover-primary' : ''
                  }`}
                >
                  <Icon size={14} />
                  {item.label}
                </button>
              )
            })}
          </div>
        ))
      )}
      <div className="mt-auto p-3">
        <span className="inline-flex rounded-full border border-primary px-2 py-0.5 text-xs text-secondary">
          v0.1.0
        </span>
      </div>
    </aside>
  )
}
