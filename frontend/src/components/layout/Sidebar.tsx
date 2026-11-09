import { useEffect, useRef, useState } from 'react'
import { BsLayoutSidebarInset } from 'react-icons/bs'
import { IoSearch, IoLogOutOutline, IoExtensionPuzzleOutline, IoAdd } from 'react-icons/io5'
import { FiChevronLeft } from 'react-icons/fi'
import { GrSettingsOption } from 'react-icons/gr'
import { CgArrowsScrollV } from 'react-icons/cg'
import { useAccount } from '../../domains/account/hooks'
import { useAgentProfile } from '../../domains/models/hooks'
import { useChatMessages } from '../../domains/chat/hooks'
import AgentListItem from './AgentListItem'

type SidebarProps = {
  mode?: 'main' | 'settings'
  onToggleSettings?: () => void
  onSignOut?: () => void
}

export default function Sidebar({ mode = 'main', onToggleSettings, onSignOut }: SidebarProps) {
  const { data: account } = useAccount()
  const { data: agentProfile } = useAgentProfile('default')
  const { data: messages = [] } = useChatMessages('default')
  const avatar = account?.user?.image ?? '/argus-logo.svg'
  const [open, setOpen] = useState(false)
  const agentAvatar = '/argus-logo.svg'
  const lastMsg = messages.length ? messages[messages.length - 1] : null
  const lastText = lastMsg?.text ?? ''
  const lastTime = lastMsg?.created_at ? new Date(lastMsg.created_at).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }) : ''
  const agentName = agentProfile?.agent_name || 'Argus'
  const containerRef = useRef<HTMLDivElement | null>(null)

  useEffect(() => {
    if (!open) return
    const handler = (e: MouseEvent) => {
      if (containerRef.current && !containerRef.current.contains(e.target as Node)) {
        setOpen(false)
      }
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [open])

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
          <IoAdd size={18} />
        </button>
      </div>
      <div className="px-2 pb-2">
        <div className="flex items-center gap-2 rounded-xl border border-primary px-2 py-1">
          <IoSearch size={18} className="text-secondary" />
          <input
            type="text"
            placeholder="Search"
            className="w-full bg-transparent text-sm text-primary placeholder:text-secondary focus:outline-none"
          />
        </div>
      </div>
      <div className="flex flex-col gap-1 px-2">
        <AgentListItem
          avatar={agentAvatar}
          name={agentName}
          lastMessage={lastText}
          time={lastTime}
          active
        />
      </div>
      <div ref={containerRef} className="relative mt-auto p-1">
        {open && (
          <div className="absolute bottom-full left-1 right-1 mb-2 rounded-xl border border-primary bg-primary p-1 shadow-lg">
            <div className="flex items-center gap-2 rounded-xl px-2 py-1.5">
              <img src={avatar} alt="" className="h-10 w-10 rounded-full" />
              <div className="flex flex-col items-start leading-tight">
                <span className="text-sm font-medium text-primary">{account?.user?.name ?? 'User'}</span>
                <span className="text-xs text-secondary">{account?.plan?.id ?? ''}</span>
              </div>
            </div>
            <div className="my-1 border-t border-primary" />
            <button
              type="button"
              onClick={() => { onToggleSettings?.(); setOpen(false) }}
              className="flex w-full items-center gap-2 rounded-xl px-2 py-1.5 text-sm text-primary transition-colors hover:bg-hover-primary"
            >
              <GrSettingsOption size={14} />
              Settings
            </button>
            <button
              type="button"
              className="flex w-full items-center gap-2 rounded-xl px-2 py-1.5 text-sm text-primary transition-colors hover:bg-hover-primary"
            >
              <IoExtensionPuzzleOutline size={14} />
              Integrations
            </button>
            {onSignOut && (
              <>
                <div className="my-1 border-t border-primary" />
                <button
                  type="button"
                  onClick={() => { onSignOut(); setOpen(false) }}
                  className="flex w-full items-center gap-2 rounded-xl px-2 py-1.5 text-sm text-primary transition-colors hover:bg-hover-primary"
                >
                  <IoLogOutOutline size={14} />
                  Sign out
                </button>
              </>
            )}
          </div>
        )}
        <button
          type="button"
          onClick={() => setOpen((v) => !v)}
          className="flex h-10 w-full cursor-pointer items-center gap-2 rounded-full border border-primary bg-secondary pl-0 pr-3 text-sm text-primary transition-colors hover:bg-hover-secondary"
        >
          <img src={avatar} alt="" className="h-10 w-10 rounded-full" />
          <div className="flex flex-col items-start leading-tight">
            <span className="text-sm font-medium text-primary">{account?.user?.name ?? 'User'}</span>
            <span className="text-xs text-secondary">{account?.user?.email ?? ''}</span>
          </div>
          <CgArrowsScrollV size={16} className="ml-auto text-grey-300" />
        </button>
      </div>
    </aside>
  )
}
