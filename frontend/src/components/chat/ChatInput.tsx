import { useRef, useState } from 'react'
import { LuPlus } from 'react-icons/lu'
import { FaArrowUp } from 'react-icons/fa'
import { FiCircle, FiCpu } from 'react-icons/fi'
import ModelPicker from './ModelPicker'

function formatUsage(n: number): string {
  if (n >= 1e9) return `${(n / 1e9).toFixed(1)}B`
  if (n >= 1e6) return `${(n / 1e6).toFixed(1)}M`
  if (n >= 1e3) return `${(n / 1e3).toFixed(1)}K`
  return `${n}`
}

const TOTAL_USAGE = 1234

type ChatInputProps = {
  onSend?: () => void
}

export default function ChatInput({ onSend }: ChatInputProps) {
  const textareaRef = useRef<HTMLTextAreaElement | null>(null)
  const [expanded, setExpanded] = useState(false)
  const [modelOpen, setModelOpen] = useState(false)
  const [selectedModel, setSelectedModel] = useState('GPT-4o')

  const handleSend = () => {
    onSend?.()
  }

  const resize = () => {
    const el = textareaRef.current
    if (!el) return
    el.style.height = 'auto'
    const next = Math.min(el.scrollHeight, 160)
    el.style.height = `${next}px`
    setExpanded(el.scrollHeight > 40)
  }

  return (
    <div className="mx-auto mb-2 flex w-3xl flex-col gap-1">
      <div
        className={`flex items-end border border-primary bg-transparent p-1 ${
          expanded ? 'rounded-2xl' : 'rounded-full'
        }`}
      >
        <button
          type="button"
          className="flex h-7 w-7 shrink-0 items-center justify-center rounded-full bg-secondary text-primary transition-colors hover:bg-hover-secondary"
        >
          <LuPlus size={14} />
        </button>
        <textarea
          ref={textareaRef}
          rows={1}
          onInput={resize}
          onKeyDown={(e) => {
            if (e.key === 'Enter' && !e.shiftKey) {
              e.preventDefault()
              handleSend()
            }
          }}
          placeholder="Message…"
          className="mx-2 flex-1 resize-none overflow-hidden bg-transparent py-1 text-sm leading-5 text-primary placeholder:text-grey-300 focus:outline-none"
          style={{ minHeight: 28, maxHeight: 160 }}
        />
        <button
          type="button"
          onClick={handleSend}
          className="flex h-7 w-7 shrink-0 items-center justify-center rounded-full bg-accent text-black transition-colors hover:bg-hover-accent"
        >
          <FaArrowUp size={12} />
        </button>
      </div>
      <div className="relative flex items-center justify-between pr-2">
        <span className="flex cursor-pointer items-center gap-1 rounded-md px-2 py-1 text-xs text-secondary transition-colors hover:bg-hover-secondary hover:text-primary">
          <FiCircle size={12} />
          {formatUsage(TOTAL_USAGE)} tokens
        </span>
        <button
          type="button"
          onClick={() => setModelOpen((v) => !v)}
          className={`flex items-center gap-1 rounded-md px-2 py-1 text-xs transition-colors ${
            modelOpen
              ? 'bg-secondary text-primary'
              : 'text-secondary hover:bg-hover-secondary hover:text-primary'
          }`}
        >
          <FiCpu size={12} />
          {selectedModel}
        </button>
        {modelOpen && (
          <div className="absolute bottom-full right-2 mb-1">
            <ModelPicker onSelect={setSelectedModel} onClose={() => setModelOpen(false)} />
          </div>
        )}
      </div>
    </div>
  )
}