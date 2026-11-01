import { isValidElement, type ReactNode } from 'react'
import type { ReactElement } from 'react'
import Markdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import WebsiteCard, { isHttpUrl } from './WebsiteCard'

type AgentBubbleProps = {
  content?: string
  error?: string
  status?: string
  children?: string
  /** Extra class names for the bubble container. */
  className?: string
}

function linkify(text: string): string {
  const NUL = String.fromCharCode(0)
  const placeholder = () => new RegExp(`${NUL}(\\d+)${NUL}`, 'g')
  const stash: string[] = []
  const keep = (value: string) => {
    stash.push(value)
    return `${NUL}${stash.length - 1}${NUL}`
  }
  const resolve = (value: string): string =>
    value.replace(placeholder(), (_m, i: string) => resolve(stash[Number(i)]))
  let out = text
  out = out.replace(/```[\s\S]*?```/g, keep)
  out = out.replace(/`[^`\n]+`/g, keep)
  out = out.replace(/!\[[^\]]*\]\([^)]*\)/g, keep)
  out = out.replace(/\[[^\]]+\]\([^)]+\)/g, keep)
  out = out.replace(/(^|[\s>])(https?:\/\/[^\s<]+)/g, (m, pre: string, url: string) => {
    const clean = url.replace(/[.,;:!?'"]+$/, '')
    return clean ? `${pre}<${clean}>` : m
  })
  return out.replace(placeholder(), (_m, i: string) => resolve(stash[Number(i)]))
}

function linkLabel(children: ReactNode): string | null {
  let label = ''
  let image = false
  const walk = (node: ReactNode) => {
    if (node == null || typeof node === 'boolean') return
    if (typeof node === 'string' || typeof node === 'number') {
      label += String(node)
      return
    }
    if (Array.isArray(node)) {
      node.forEach(walk)
      return
    }
    if (isValidElement(node)) {
      const el = node as ReactElement<{ src?: string; children?: ReactNode }>
      if (el.type === 'img') {
        image = true
        return
      }
      walk(el.props.children)
    }
  }
  walk(children)
  if (image) return null
  return label
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
        remarkPlugins={[remarkGfm]}
        components={{
          table: ({ children }) => (
            <div className="my-2 w-full max-w-full overflow-x-auto rounded-lg">
              <table className="w-full border-collapse text-left text-xs">{children}</table>
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
    </div>
  )
}
