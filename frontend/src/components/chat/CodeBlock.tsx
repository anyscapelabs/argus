import { useRef, useState, type ReactNode } from 'react'
import { FiCheck, FiCopy } from 'react-icons/fi'

export default function CodeBlock({ children }: { children?: ReactNode }) {
  const preRef = useRef<HTMLPreElement>(null)
  const [copied, setCopied] = useState(false)
  const child = (Array.isArray(children) ? children[0] : children) as any
  const lang = /language-([\w-]+)/.exec(child?.props?.className ?? '')?.[1]
  const copy = async () => {
    try {
      await navigator.clipboard.writeText((preRef.current?.innerText ?? '').replace(/\n$/, ''))
      setCopied(true)
      setTimeout(() => setCopied(false), 1500)
    } catch {}
  }
  return (
    <div className="my-2 overflow-hidden rounded-lg border border-secondary bg-black/40">
      <div className="flex items-center justify-between border-b border-secondary px-3 py-1 text-xs text-secondary">
        <span>{lang ?? 'text'}</span>
        <button type="button" onClick={copy} className="flex cursor-pointer items-center gap-1 hover:text-primary">
          {copied ? <FiCheck size={12} /> : <FiCopy size={12} />}
          {copied ? 'Copied' : 'Copy'}
        </button>
      </div>
      <pre ref={preRef} className="select-text overflow-x-auto p-3 text-xs leading-5">{children}</pre>
    </div>
  )
}
