import Markdown from 'react-markdown'
import WebsiteCard, { isHttpUrl } from './WebsiteCard'

type AgentBubbleProps = {
  content?: string
  error?: string
  status?: string
  children?: string
  /** Extra class names for the bubble container. */
  className?: string
}

// Wrap bare URLs in CommonMark autolinks so they render as website cards.
// Code fences, inline code, images and existing links are stashed first so
// their contents are never touched.
function linkify(text: string): string {
  const stash: string[] = []
  const keep = (value: string) => {
    stash.push(value)
    return `\u0000${stash.length - 1}\u0000`
  }
  let out = text
  out = out.replace(/```[\s\S]*?```/g, keep)
  out = out.replace(/`[^`\n]+`/g, keep)
  out = out.replace(/!\[[^\]]*\]\([^)]*\)/g, keep)
  out = out.replace(/\[[^\]]+\]\([^)]+\)/g, keep)
  out = out.replace(/(^|[\s>])(https?:\/\/[^\s<]+)/g, (m, pre: string, url: string) => {
    const clean = url.replace(/[.,;:!?'"]+$/, '')
    return clean ? `${pre}<${clean}>` : m
  })
  out = out.replace(/\u0000(\d+)\u0000/g, (_m, i: string) => stash[Number(i)])
  return out
}

export default function AgentBubble({
  content,
  error,
  status,
  children,
  className = '',
}: AgentBubbleProps) {
  const text = content ?? children ?? ''
  const failed = status === 'error'
  if (failed && !text.trim() && !error) {
    return (
      <div
        className={`w-fit max-w-xl rounded-xl border border-red-400/40 bg-secondary px-2 py-1 text-sm text-red-400 ${className}`}
      >
        The model call failed
      </div>
    )
  }
  if (failed && !text.trim()) {
    return (
      <div
        className={`w-fit max-w-xl rounded-xl border border-red-400/40 bg-secondary px-2 py-1 text-sm text-red-400 ${className}`}
      >
        <Markdown>{error ?? ''}</Markdown>
      </div>
    )
  }
  return (
    <div
      className={`markdown w-fit max-w-xl rounded-xl border border-primary bg-secondary px-2 py-1 text-sm font-medium ${className}`}
    >
      <Markdown
        components={{
          a: ({ href, children }) =>
            href && isHttpUrl(href) && typeof children === 'string' ? (
              <WebsiteCard url={href}>{children}</WebsiteCard>
            ) : (
              <a href={href} target="_blank" rel="noreferrer">
                {children}
              </a>
            ),
        }}
      >
        {linkify(text)}
      </Markdown>
      {failed && error && (
        <div className="mt-1 border-t border-red-400/40 pt-1 text-xs text-red-400">
          <Markdown>{error}</Markdown>
        </div>
      )}
    </div>
  )
}
