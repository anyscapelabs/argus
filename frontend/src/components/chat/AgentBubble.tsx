import { memo, useState } from 'react'
import Markdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { PiArrowBendUpRightFill } from 'react-icons/pi'
import { HiDotsHorizontal } from 'react-icons/hi'
import WebsiteCard, { isHttpUrl } from './WebsiteCard'
import CodeBlock from './CodeBlock'
import { linkify, linkLabel } from '../../lib/linkify'
import { ReactionBadge, ReactionPicker } from './ReactionPicker'

type AgentBubbleProps = {
  content?: string
  error?: string
  status?: string
  children?: string
  /** Extra class names for the bubble container. */
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

  if (failed && !text.trim() && !error) {
    return (
      <div className={`group flex w-fit max-w-full items-center gap-2 ${className}`}>
        <div className="relative max-w-xl rounded-xl border border-red-400/40 bg-secondary px-2 py-1 text-sm text-red-400">
          The model call failed
          {reactionBadge}
        </div>
        {actions}
      </div>
    )
  }
  if (failed && !text.trim()) {
    return (
      <div className={`group flex w-fit max-w-full items-center gap-2 ${className}`}>
        <div className="relative max-w-xl rounded-xl border border-red-400/40 bg-secondary px-2 py-1 text-sm text-red-400">
          <Markdown>{error ?? ''}</Markdown>
          {reactionBadge}
        </div>
        {actions}
      </div>
    )
  }
  return (
    <div className={`group flex w-fit max-w-full items-start gap-2 ${className}`}>
      <div className="markdown relative max-w-xl select-text break-words rounded-xl border border-primary bg-secondary px-3 py-2 text-sm">
        <Markdown
          remarkPlugins={[remarkGfm]}
          components={{
            pre: ({ children }) => <CodeBlock>{children}</CodeBlock>,
            table: ({ children }) => (
              <div className="my-2 w-full max-w-full overflow-hidden rounded-2xl">
                <table className="w-full border-separate border-spacing-0 text-left text-xs">{children}</table>
              </div>
            ),
            a: ({ href, children }) => {
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
          {linkify(text)}
        </Markdown>
        {failed && error && (
          <div className="mt-1 border-t border-red-400/40 pt-1 text-xs text-red-400">
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
