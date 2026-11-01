import { useRef, useState, type ReactNode } from 'react'
import { FiCheck, FiCopy } from 'react-icons/fi'

export default function CodeBlock({ children }: { children?: ReactNode }) {
  const preRef = useRef<HTMLPreElement>(null)
  const [copied, setCopied] = useState(false)
  const copy = async () => {
    try {
      await navigator.clipboard.writeText((preRef.current?.innerText ?? '').replace(/\n$/, ''))
      setCopied(true)
      setTimeout(() => setCopied(false), 1500)
    } catch {}
  }
  return (
    <div className="group relative my-2 overflow-hidden rounded-lg border border-secondary bg-black/40">
      <pre ref={preRef} className="select-text overflow-x-auto p-3 text-xs leading-5">{children}</pre>
      <button
        type="button"
        onClick={copy}
        className="absolute right-2 top-2 flex cursor-pointer items-center gap-1 rounded bg-secondary/80 px-1.5 py-0.5 text-xs text-secondary opacity-0 transition-opacity hover:text-primary group-hover:opacity-100"
      >
        {copied ? <FiCheck size={12} /> : <FiCopy size={12} />}
        {copied ? 'Copied' : 'Copy'}
      </button>
    </div>
  )
}
