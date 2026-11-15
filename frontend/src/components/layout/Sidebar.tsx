import { useEffect, useRef, useState } from 'react'
import { BsLayoutSidebarInset } from 'react-icons/bs'
import { IoSearch, IoLogOutOutline, IoExtensionPuzzleOutline, IoAdd, IoTrash } from 'react-icons/io5'
import { FiChevronLeft } from 'react-icons/fi'
import { GrSettingsOption } from 'react-icons/gr'
import { CgArrowsScrollV } from 'react-icons/cg'
import { GoBellSlash, GoPin, GoStarFill } from 'react-icons/go'
import { VscUnpin } from 'react-icons/vsc'
import { useAccount } from '../../domains/account/hooks'
import { useAgents } from '../../domains/models/hooks'
import { voiceMessagePreview } from '../../lib/voice'
import VoiceIcon from '../chat/VoiceIcon'
import AgentListItem from './AgentListItem'

type SidebarProps = {
  mode?: 'main' | 'settings'
  onToggleSettings?: () => void
  onOpenIntegrations?: () => void
  onOpenChat?: () => void
  onSignOut?: () => void
  agentActive?: boolean
  onOpenNewAgent?: () => void
  activeAgent?: string
  onSelectAgent?: (agentID: string) => void
}

export default function Sidebar({
  mode = 'main',
  onToggleSettings,
  onOpenIntegrations,
  onOpenChat,
  onSignOut,
  agentActive = false,
  onOpenNewAgent,
  activeAgent,
  onSelectAgent,
}: SidebarProps) {
  const { data: account } = useAccount()
  const { data: agents = [] } = useAgents()
  const avatar = account?.user?.image ?? '/argus-logo.svg'
  const [open, setOpen] = useState(false)
  const [search, setSearch] = useState('')
  const [agentMenu, setAgentMenu] = useState<{ x: number; y: number; agentId: string } | null>(null)
  const [pinnedAgents, setPinnedAgents] = useState<Set<string>>(new Set())
  const listedAgents = agents.length > 0 ? agents : [{
    id: 'default',
    name: 'Argus',
    avatar: '/argus-logo.svg',
    handle: 'argus',
    role: '',
    last_message: '',
    last_message_kind: '',
    last_message_at: 0,
  }]
  const containerRef = useRef<HTMLDivElement | null>(null)
  const agentMenuRef = useRef<HTMLDivElement | null>(null)

  const query = search.trim().toLowerCase()
  const filteredAgents = query
    ? listedAgents.filter((agent) => {
        const preview =
          agent.last_message_kind === 'voice'
            ? voiceMessagePreview(agent.last_message)
            : agent.last_message
        return [agent.name, agent.handle, agent.role, preview].some((value) =>
          (value || '').toLowerCase().includes(query),
        )
      })
    : listedAgents

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

  useEffect(() => {
    if (!agentMenu) return
    const close = () => setAgentMenu(null)
    const handleMouseDown = (e: MouseEvent) => {
      if (agentMenuRef.current && !agentMenuRef.current.contains(e.target as Node)) {
        setAgentMenu(null)
      }
    }
    document.addEventListener('mousedown', handleMouseDown)
    document.addEventListener('scroll', close, true)
    window.addEventListener('resize', close)
    return () => {
      document.removeEventListener('mousedown', handleMouseDown)
      document.removeEventListener('scroll', close, true)
      window.removeEventListener('resize', close)
    }
  }, [agentMenu])

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
      <div className="flex items-center justify-between rounded-full p-1">
        <button
          type="button"
          className="flex h-8 w-8 items-center justify-center rounded-full text-grey-300 transition-colors hover:bg-hover-primary"
        >
          <BsLayoutSidebarInset size={16} />
        </button>
        <button
          type="button"
          onClick={onOpenNewAgent}
          aria-label="New agent"
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
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            className="w-full bg-transparent text-sm text-primary placeholder:text-secondary focus:outline-none"
          />
        </div>
      </div>
      <div className="flex flex-col gap-1 px-2">
        {filteredAgents.length === 0 ? (
          <p className="px-2 py-1 text-sm text-secondary">No agents found</p>
        ) : (
          filteredAgents.map((agent) => {
            const voice = agent.last_message_kind === 'voice'
            const preview = voice
              ? voiceMessagePreview(agent.last_message)
              : agent.last_message
            const time = agent.last_message_at
              ? new Date(agent.last_message_at).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
              : ''
            return (
              <AgentListItem
                key={agent.id}
                avatar={agent.avatar || '/argus-logo.svg'}
                name={agent.name}
                lastMessage={preview}
                lastMessageIcon={voice ? <VoiceIcon size={11} className="shrink-0" /> : undefined}
                time={time}
                active={(agentActive && (activeAgent || 'default') === agent.id) || agentMenu?.agentId === agent.id}
                onClick={() => {
                  if (onSelectAgent) onSelectAgent(agent.id)
                  else if (agent.id === 'default') onOpenChat?.()
                }}
                onContextMenu={(e) => {
                  e.preventDefault()
                  setAgentMenu({ x: e.clientX, y: e.clientY, agentId: agent.id })
                }}
              />
            )
          })
        )}
      </div>
      {agentMenu && (
        <div
          ref={agentMenuRef}
          className="fixed z-50 min-w-[11rem] rounded-xl border border-primary bg-primary p-1 shadow-lg"
          style={{ top: agentMenu.y, left: agentMenu.x }}
        >
          <button
            type="button"
            onClick={() => setAgentMenu(null)}
            className="flex w-full cursor-pointer items-center gap-2 rounded-lg px-2 py-1 text-left text-sm text-primary transition-colors hover:bg-hover-primary"
          >
            <GoBellSlash size={14} />
            Mute
          </button>
          {(() => {
            const isPinned = pinnedAgents.has(agentMenu.agentId)
            return (
              <button
                type="button"
                onClick={() => {
                  setPinnedAgents((prev) => {
                    const next = new Set(prev)
                    if (next.has(agentMenu.agentId)) next.delete(agentMenu.agentId)
                    else next.add(agentMenu.agentId)
                    return next
                  })
                  setAgentMenu(null)
                }}
                className="flex w-full cursor-pointer items-center gap-2 rounded-lg px-2 py-1 text-left text-sm text-primary transition-colors hover:bg-hover-primary"
              >
                {isPinned ? <VscUnpin size={14} /> : <GoPin size={14} />}
                {isPinned ? 'Unpin' : 'Pin'}
              </button>
            )
          })()}
          <button
            type="button"
            onClick={() => setAgentMenu(null)}
            className="flex w-full cursor-pointer items-center gap-2 rounded-lg px-2 py-1 text-left text-sm text-primary transition-colors hover:bg-hover-primary"
          >
            <GoStarFill size={14} />
            Set as Default
          </button>
          <div className="my-1 border-t border-primary" />
          <button
            type="button"
            onClick={() => setAgentMenu(null)}
            className="flex w-full cursor-pointer items-center gap-2 rounded-lg px-2 py-1 text-left text-sm text-red-400 transition-colors hover:bg-hover-primary"
          >
            <IoTrash size={14} />
            Delete Agent
          </button>
        </div>
      )}
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
              onClick={() => { onOpenIntegrations?.(); setOpen(false) }}
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
