import type { ReactNode } from 'react'

export type AgentListItemProps = {
  avatar?: string
  name: string
  lastMessage?: string
  time?: string
  active?: boolean
  onClick?: () => void
  trailing?: ReactNode
}

export default function AgentListItem({
  avatar = '/argus-logo.svg',
  name,
  lastMessage,
  time,
  active = false,
  onClick,
}: AgentListItemProps) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={`flex w-full items-center gap-2 rounded-xl px-2 py-2 text-left transition-colors ${
        active ? 'bg-hover-primary' : 'hover:bg-hover-primary'
      }`}
    >
      <div className={`flex h-10 w-10 shrink-0 items-center justify-center rounded-full border ${active ? 'border-accent bg-accent/10' : 'border-primary bg-secondary'}`}>
        <img src={avatar} alt="" className="h-8 w-8 rounded-full" />
      </div>
      <div className="min-w-0 flex-1">
        <div className={`truncate text-sm font-medium ${active ? 'text-accent' : 'text-primary'}`}>{name}</div>
        <div className="truncate text-xs text-secondary">{lastMessage || 'No messages yet'}</div>
      </div>
      <span className="shrink-0 text-xs text-secondary">{time}</span>
    </button>
  )
}