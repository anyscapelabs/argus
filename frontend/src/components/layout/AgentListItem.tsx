import type { MouseEvent, ReactNode } from 'react'
import Markdown, { type Components } from 'react-markdown'
import remarkGfm from 'remark-gfm'
import AgentGem, { isAvatarColor } from '../AgentGem'

const previewMarkdownComponents: Components = {
  p: ({ children }) => <span>{children}</span>,
  a: ({ children }) => <span>{children}</span>,
  br: () => ' ',
}

export type AgentListItemProps = {
  avatar?: string
  name: string
  lastMessage?: string
  lastMessageIcon?: ReactNode
  time?: string
  unread?: number
  active?: boolean
  collapsed?: boolean
  onClick?: () => void
  onContextMenu?: (e: MouseEvent<HTMLButtonElement>) => void
  trailing?: ReactNode
}

export default function AgentListItem({
  avatar,
  name,
  lastMessage,
  lastMessageIcon,
  time,
  unread = 0,
  active = false,
  collapsed = false,
  onClick,
  onContextMenu,
}: AgentListItemProps) {
  return (
    <button
      type="button"
      onClick={onClick}
      onContextMenu={onContextMenu}
      aria-label={collapsed ? name : undefined}
      title={collapsed ? name : undefined}
      className={`flex items-center rounded-xl text-left transition-colors ${
        collapsed ? 'h-12 w-12 justify-center p-1' : 'w-full gap-2 px-2 py-2'
      } ${
        active ? 'bg-hover-primary' : 'hover:bg-hover-primary'
      }`}
    >
      {avatar && (
        <div className={`relative flex h-10 w-10 shrink-0 items-center justify-center border ${collapsed ? 'rounded-xl' : 'rounded-full'} ${active ? 'border-accent-secondary bg-accent-secondary/10' : 'border-primary bg-secondary'}`}>
          {isAvatarColor(avatar) ? (
            <AgentGem color={avatar} className="h-8 w-8" />
          ) : (
            <img src={avatar} alt="" className={`h-8 w-8 ${collapsed ? 'rounded-xl' : 'rounded-full'}`} />
          )}
          {collapsed && unread > 0 && (
            <span aria-label={`${unread} unread messages`} className="absolute -right-1 -top-1 flex h-4 min-w-4 items-center justify-center rounded-full bg-accent px-0.5 text-[10px] font-semibold leading-none text-accent-foreground">
              {unread > 99 ? '99+' : unread}
            </span>
          )}
        </div>
      )}
      {!collapsed && (
        <>
          <div className="min-w-0 flex-1">
            <div className={`truncate text-sm font-medium ${active ? 'text-primary' : 'text-primary'}`}>{name}</div>
            <div className="flex min-w-0 items-center gap-1.5 text-xs text-secondary">
              {lastMessageIcon}
              <span className="markdown truncate">
                {lastMessage ? (
                  <Markdown
                    remarkPlugins={[remarkGfm]}
                    allowedElements={['p', 'strong', 'em', 'del', 'code', 'a', 'br']}
                    unwrapDisallowed
                    components={previewMarkdownComponents}
                  >
                    {lastMessage}
                  </Markdown>
                ) : 'No messages yet'}
              </span>
            </div>
          </div>
          <div className="flex shrink-0 flex-col items-end gap-0.5">
            <span className="text-xs leading-5 text-secondary">{time}</span>
            {unread > 0 ? (
              <span aria-label={`${unread} unread messages`} className="flex h-5 min-w-5 items-center justify-center rounded-full bg-accent px-1 text-[11px] font-semibold leading-none text-accent-foreground">
                {unread > 99 ? '99+' : unread}
              </span>
            ) : (
              <span className="h-5" />
            )}
          </div>
        </>
      )}
    </button>
  )
}
