import { lazy, Suspense, useRef, useState, type ReactNode } from 'react'
import { FiCheck, FiCopy } from 'react-icons/fi'

const Highlighter = lazy(async () => {
  const [{ Prism: SyntaxHighlighter }, styleMod] = await Promise.all([
    import('react-syntax-highlighter'),
    import('react-syntax-highlighter/dist/esm/styles/prism'),
  ])
  const vscDarkPlus = styleMod.vscDarkPlus
  function Comp({ code, language }: { code: string; language: string }) {
    return (
      <SyntaxHighlighter
        language={language}
        style={vscDarkPlus}
        customStyle={{
          margin: 0,
          padding: '0.75rem',
          background: 'transparent',
          fontSize: '0.75rem',
          lineHeight: '1.25rem',
        }}
        codeTagProps={{
          style: {
            fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", "Courier New", monospace',
          },
        }}
        wrapLongLines={false}
      >
        {code}
      </SyntaxHighlighter>
    )
  }
  return { default: Comp }
})

export default function CodeBlock({ children }: { children?: ReactNode }) {
  const containerRef = useRef<HTMLDivElement>(null)
  const [copied, setCopied] = useState(false)
  const child = (Array.isArray(children) ? children[0] : children) as any
  const className = child?.props?.className || ''
  const match = /language-([\w-]+)/.exec(className)
  const language = match?.[1] || 'text'
  const code = String(child?.props?.children ?? '').replace(/\n$/, '')

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(code)
      setCopied(true)
      setTimeout(() => setCopied(false), 1500)
    } catch {}
  }

  return (
    <div ref={containerRef} className="group relative my-2 overflow-hidden rounded-xl border border-secondary bg-[#1e1e1e]">
      <Suspense fallback={<pre className="m-0 p-3 text-xs text-grey-100">{code}</pre>}>
        <Highlighter code={code} language={language} />
      </Suspense>
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