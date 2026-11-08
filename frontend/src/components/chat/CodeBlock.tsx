import { useEffect, useRef, useState, type ReactNode } from 'react'
import { FiCheck, FiCopy } from 'react-icons/fi'
import 'highlight.js/styles/atom-one-dark.css'

export default function CodeBlock({ children }: { children?: ReactNode }) {
  const containerRef = useRef<HTMLDivElement>(null)
  const codeRef = useRef<HTMLElement>(null)
  const [copied, setCopied] = useState(false)
  const child = (Array.isArray(children) ? children[0] : children) as any
  const className = child?.props?.className || ''
  const match = /language-([\w-]+)/.exec(className)
  const language = match?.[1] || 'text'
  const code = String(child?.props?.children ?? '').replace(/\n$/, '')

  useEffect(() => {
    let cancelled = false
    ;(async () => {
      const hljs = (await import('highlight.js/lib/core')).default
      const langMap: Record<string, () => Promise<any>> = {
        javascript: () => import('highlight.js/lib/languages/javascript'),
        js: () => import('highlight.js/lib/languages/javascript'),
        typescript: () => import('highlight.js/lib/languages/typescript'),
        ts: () => import('highlight.js/lib/languages/typescript'),
        bash: () => import('highlight.js/lib/languages/bash'),
        sh: () => import('highlight.js/lib/languages/bash'),
        shell: () => import('highlight.js/lib/languages/bash'),
        json: () => import('highlight.js/lib/languages/json'),
        python: () => import('highlight.js/lib/languages/python'),
        py: () => import('highlight.js/lib/languages/python'),
        go: () => import('highlight.js/lib/languages/go'),
        css: () => import('highlight.js/lib/languages/css'),
        html: () => import('highlight.js/lib/languages/xml'),
        xml: () => import('highlight.js/lib/languages/xml'),
        markdown: () => import('highlight.js/lib/languages/markdown'),
        md: () => import('highlight.js/lib/languages/markdown'),
        sql: () => import('highlight.js/lib/languages/sql'),
        yaml: () => import('highlight.js/lib/languages/yaml'),
        yml: () => import('highlight.js/lib/languages/yaml'),
        rust: () => import('highlight.js/lib/languages/rust'),
        rs: () => import('highlight.js/lib/languages/rust'),
        java: () => import('highlight.js/lib/languages/java'),
      }
      const loader = langMap[language.toLowerCase()]
      if (loader) {
        const mod = await loader()
        const fn = mod.default ?? mod
        if (!hljs.getLanguage(language)) {
          hljs.registerLanguage(language, fn)
        }
      }
      if (cancelled || !codeRef.current) return
      codeRef.current.innerHTML = hljs.highlight(code, { language: hljs.getLanguage(language) ? language : 'plaintext' }).value
      codeRef.current.className = `hljs language-${language}`
    })()
    return () => { cancelled = true }
  }, [code, language])

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(code)
      setCopied(true)
      setTimeout(() => setCopied(false), 1500)
    } catch {}
  }

  return (
    <div ref={containerRef} className="group relative my-2 overflow-hidden rounded-xl border border-secondary bg-[#24292e]">
      <pre className="m-0 overflow-x-auto p-3 text-xs leading-5">
        <code ref={codeRef} className={`hljs language-${language}`}>
          {code}
        </code>
      </pre>
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