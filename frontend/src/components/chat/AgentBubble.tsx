import { memo, useMemo, useState } from 'react'
import Markdown, { defaultUrlTransform } from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { PiArrowBendUpRightFill } from 'react-icons/pi'
import { HiDotsHorizontal } from 'react-icons/hi'
import WebsiteCard, { isHttpUrl } from './WebsiteCard'
import CodeBlock from './CodeBlock'
import MarkdownTable, { hasMarkdownTable } from './MarkdownTable'
import { linkify, linkLabel } from '../../lib/linkify'
import { mentionHandle, mentionify, MENTION_SCHEME } from '../../lib/mentions'
import { useAgents } from '../../domains/models/hooks'
import MentionChip from './MentionChip'
import { ReactionBadge, ReactionPicker } from './ReactionPicker'

type AgentBubbleProps = {
  content?: string
  error?: string
  status?: string
  children?: string
  className?: string
  onReply?: () => void
  onReact?: (emoji: string | null) => void
  onMore?: () => void
  reaction?: string | null
  reactionPending?: boolean
}

function AgentBubble({
  content,
  error,
  status,
  children,
  className = '',
  onReply,
  onReact,
  onMore,
  reaction = null,
  reactionPending = false,
}: AgentBubbleProps) {
  const text = content ?? children ?? ''
  const failed = status === 'error'
  const [reactionOpen, setReactionOpen] = useState(false)
  const { data: agents } = useAgents()
  const agentsByHandle = useMemo(
    () => new Map((agents ?? []).filter((a) => a.handle).map((a) => [a.handle.toLowerCase(), a])),
    [agents],
  )
  const handles = useMemo(() => new Set(agentsByHandle.keys()), [agentsByHandle])

  const actions = (onReply || onReact || onMore) ? (
    <div className={`flex shrink-0 items-center gap-1 transition-opacity ${reactionOpen ? 'opacity-100' : 'opacity-0 group-hover:opacity-100'}`}>
      {onReply && (
      <button
        type="button"
        onClick={onReply}
        aria-label="Reply"
        className="flex h-7 w-7 items-center justify-center rounded-lg text-secondary transition-colors hover:bg-hover-secondary hover:text-primary"
      >
        <PiArrowBendUpRightFill size={14} />
      </button>
      )}
      {onReact && (
      <ReactionPicker
        reaction={reaction}
        onReactionChange={onReact}
        onOpenChange={setReactionOpen}
        disabled={reactionPending}
      />
      )}
      {onMore && (
      <button
        type="button"
        onClick={onMore}
        aria-label="More actions"
        className="flex h-7 w-7 items-center justify-center rounded-lg text-secondary transition-colors hover:bg-hover-secondary hover:text-primary"
      >
        <HiDotsHorizontal size={14} />
      </button>
      )}
    </div>
  ) : null

  const reactionBadge = <ReactionBadge reaction={reaction} />

  if (failed && !text.trim()) {
    return (
      <div className={`group flex w-fit max-w-[70%] items-center gap-2 ${className}`}>
        <div className="relative w-fit max-w-full rounded-xl border border-danger/40 bg-secondary px-2 py-1 text-sm text-danger">
          {error ? <Markdown>{error}</Markdown> : 'The model call failed'}
          {reactionBadge}
        </div>
        {actions}
      </div>
    )
  }
  const wide = hasMarkdownTable(text)
  return (
    <div className={`group flex w-fit max-w-[70%] items-center gap-2 ${className}`}>
      <div className={`markdown relative ${wide ? 'w-full min-w-0' : 'w-fit max-w-full'} select-text break-words rounded-xl border border-primary bg-secondary px-3 py-2 text-sm`}>
        <Markdown
          remarkPlugins={[remarkGfm]}
          urlTransform={(url) => (url.startsWith(MENTION_SCHEME) ? url : defaultUrlTransform(url))}
          components={{
            pre: ({ children }) => <CodeBlock>{children}</CodeBlock>,
            table: ({ children }) => <MarkdownTable>{children}</MarkdownTable>,
            a: ({ href, children }) => {
              const mention = mentionHandle(href)
              if (mention) {
                const agent = agentsByHandle.get(mention)
                if (agent) return <MentionChip agent={agent} />
                return <>{children}</>
              }
              if (!href || !isHttpUrl(href)) {
                return (
                  <a href={href} target="_blank" rel="noreferrer">
                    {children}
                  </a>
                )
              }
              const label = linkLabel(children)
              if (label === null) {
                return (
                  <a href={href} target="_blank" rel="noreferrer">
                    {children}
                  </a>
                )
              }
              return <WebsiteCard url={href}>{label}</WebsiteCard>
            },
          }}
        >
          {mentionify(linkify(text), handles)}
        </Markdown>
        {failed && error && (
          <div className="mt-1 border-t border-danger/40 pt-1 text-xs text-danger">
            <Markdown>{error}</Markdown>
          </div>
        )}
        {reactionBadge}
      </div>
      {actions}
    </div>
  )
}

export default memo(AgentBubble)
