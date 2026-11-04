import { motion } from 'framer-motion'
import { FiX } from 'react-icons/fi'
import { TfiDownload } from "react-icons/tfi"
import { FaChevronDown } from "react-icons/fa"
import { SiMarkdown } from 'react-icons/si'
import { FaFileCode, FaFileWord, FaFilePdf, FaFileLines } from 'react-icons/fa6'
import { isValidElement, useEffect, type ReactElement, type ReactNode, useState } from 'react'
import Markdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import CodeBlock from '../components/chat/CodeBlock'
import WebsiteCard, { isHttpUrl } from '../components/chat/WebsiteCard'
import { useArtifact, useExportArtifact, useExportFormats, type ExportFormatView } from '../domains/artifacts/hooks'

type ArtifactViewerProps = {
  onBack?: () => void
  // The document to open. The viewer is a window onto one row of the artifacts
  // table, so it takes the id and reads the whole row itself — which is also how
  // a rewrite the agent makes while this is on screen lands without anyone
  // closing and reopening it.
  artifactID?: string | null
}

// One glyph per export target, keyed by the id the backend reports. An unknown
// id still gets a page mark, so a format added in Go shows up as a menu row
// instead of silently vanishing.
function FormatIcon({ id, className }: { id: string; className: string }) {
  switch (id) {
    case 'markdown':
      return <SiMarkdown size={14} className={className} />
    case 'html':
      return <FaFileCode size={14} className={className} />
    case 'pdf':
      return <FaFilePdf size={14} className={className} />
    case 'docx':
      return <FaFileWord size={14} className={className} />
    default:
      return <FaFileLines size={14} className={className} />
  }
}

// baseName is what the confirmation quotes back. The user chose the folder, so
// the file name is the only part they need reminding of.
function baseName(path: string): string {
  const parts = path.split(/[/\\]/)
  return parts[parts.length - 1] || path
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
  out = out.replace(/(^|[\s(>])(https?:\/\/[^\s<]+)/g, (m, pre: string, url: string, offset: number, string: string) => {
    const clean = url.replace(/[.,;:!?'"]+$/, '')
    if (!clean) return m
    const after = string.slice(offset + m.length)
    if (after === '') {
      return `${pre}<${clean}>`
    }
    if (/^[\s)\],;.!?\n\r]/.test(after)) {
      return `${pre}<${clean}>`
    }
    return m
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

export default function ArtifactViewer({ onBack, artifactID }: ArtifactViewerProps) {
  const { data: artifact, isLoading, error } = useArtifact(artifactID)
  const { data: formats } = useExportFormats()
  const exportArtifact = useExportArtifact()
  const [exportOpen, setExportOpen] = useState(false)
  const [notice, setNotice] = useState<{ text: string; tone: 'ok' | 'bad' } | null>(null)

  // The menu belongs to a document, not to the app: opening another one without
  // closing this view must not leave the previous menu hanging open.
  useEffect(() => {
    setExportOpen(false)
    setNotice(null)
  }, [artifactID])

  // A file that landed announces itself briefly; a failed export stays until it
  // is dismissed, because silence reads as a file that was never written.
  useEffect(() => {
    if (notice?.tone !== 'ok') return
    const t = setTimeout(() => setNotice(null), 6000)
    return () => clearTimeout(t)
  }, [notice])

  // Formats are the machine's answer, so a target it cannot produce is offered
  // greyed with what to install rather than hidden: the user can fix that, and
  // an option that simply is not there looks like a feature that does not exist.
  const runExport = async (format: ExportFormatView) => {
    if (!artifact || !format.available) return
    setExportOpen(false)
    try {
      const res = await exportArtifact.mutateAsync({
        artifactID: artifact.id,
        format,
        title: artifact.title,
      })
      // A cancelled dialog resolves with nothing. That is the user changing
      // their mind, not a failure, so it says nothing at all.
      if (!res) return
      setNotice({ text: `Exported ${baseName(res.path)}`, tone: 'ok' })
    } catch (err) {
      setNotice({ text: `Could not export: ${(err as Error).message}`, tone: 'bad' })
    }
  }

  const body = artifact?.body ?? ''

  return (
    <motion.div
      initial={{ opacity: 0 }}
      animate={{ opacity: 1 }}
      exit={{ opacity: 0 }}
      transition={{ duration: 0.2 }}
      className="relative flex h-full w-full flex-col bg-primary"
      onClick={onBack}
    >
      <div className="flex items-center justify-between px-2 py-2">
        <button
          type="button"
          className="flex items-center gap-1 rounded-md px-2 py-1 text-sm font-medium text-primary transition-colors hover:bg-hover-secondary"
        >
          <span className="max-w-[50vw] truncate">{artifact?.title || 'Document'}</span>
          <FaChevronDown size={10} />
        </button>
        <div className="relative flex items-center gap-1" onClick={(e) => e.stopPropagation()}>
          <button
            type="button"
            disabled={!artifact}
            onClick={(e) => { e.stopPropagation(); setExportOpen((v) => !v); }}
            className="flex items-center gap-1 rounded-lg bg-accent px-2 py-1 text-sm font-medium transition-opacity hover:opacity-90 cursor-pointer disabled:cursor-default disabled:opacity-40"
          >
            <TfiDownload size={12} className="text-foreground" />
            <span className="text-foreground">Export</span>
          </button>
          {exportOpen && (
            <div className="absolute right-0 top-8 z-50 w-44 overflow-hidden rounded-lg border border-secondary bg-secondary p-1 shadow-lg">
              {(formats ?? []).map((f) => (
                <button
                  key={f.id}
                  type="button"
                  disabled={!f.available || exportArtifact.isPending}
                  title={f.available ? f.hint : `${f.label} needs ${f.required || 'another tool'}`}
                  onClick={(e) => { e.stopPropagation(); void runExport(f) }}
                  className={`flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm transition-colors hover:bg-hover-secondary ${
                    f.available ? 'cursor-pointer text-primary' : 'cursor-default text-grey-500'
                  }`}
                >
                  <FormatIcon id={f.id} className="shrink-0 text-primary" />
                  <span className="flex-1 truncate">{f.label}</span>
                  {exportArtifact.isPending && exportArtifact.variables?.format.id === f.id && (
                    <span className="shrink-0 text-xs text-secondary">…</span>
                  )}
                </button>
              ))}
              {!formats?.length && <div className="px-2 py-1.5 text-sm text-secondary">No export targets</div>}
            </div>
          )}
          <button
            type="button"
            onClick={onBack}
            className="flex h-7 w-7 items-center justify-center rounded-full bg-secondary p-1 text-primary transition-colors hover:bg-hover-primary cursor-pointer"
          >
            <FiX size={14} />
          </button>
        </div>
      </div>
      <div className="flex-1 min-h-0 overflow-auto px-4 py-4" onClick={(e) => e.stopPropagation()}>
        <div className="mx-auto max-w-3xl select-text">
          {notice && (
            <div
              className={`mb-3 flex items-center justify-between gap-3 rounded-lg border px-3 py-2 text-sm ${
                notice.tone === 'ok'
                  ? 'border-secondary bg-secondary text-primary'
                  : 'border-red-400/40 bg-secondary text-red-400'
              }`}
            >
              <span className="min-w-0 truncate">{notice.text}</span>
              <button
                type="button"
                onClick={() => setNotice(null)}
                className="shrink-0 cursor-pointer text-secondary transition-colors hover:text-primary"
              >
                <FiX size={14} />
              </button>
            </div>
          )}
          {isLoading && <div className="text-sm text-secondary">Opening…</div>}
          {!isLoading && error && (
            <div className="text-sm text-red-400">{String((error as Error).message ?? error)}</div>
          )}
          {!isLoading && !error && body === '' && (
            <div className="text-sm text-secondary">This document is empty.</div>
          )}
          {body !== '' && (
            <div className="markdown doc">
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
                {linkify(body)}
              </Markdown>
            </div>
          )}
        </div>
      </div>
    </motion.div>
  )
}
