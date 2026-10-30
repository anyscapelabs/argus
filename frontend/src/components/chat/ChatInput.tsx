import { useRef, useState } from 'react'
import { LuPlus } from 'react-icons/lu'
import { FaArrowUp } from 'react-icons/fa'

export default function ChatInput() {
  const textareaRef = useRef<HTMLTextAreaElement | null>(null)
  const [expanded, setExpanded] = useState(false)

  const resize = () => {
    const el = textareaRef.current
    if (!el) return
    el.style.height = 'auto'
    const next = Math.min(el.scrollHeight, 160)
    el.style.height = `${next}px`
    setExpanded(el.scrollHeight > 40)
  }

  return (
    <div
      className={`mx-auto mb-4 flex w-3xl items-end border border-primary bg-transparent p-1 ${
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
        placeholder="Message…"
        className="mx-2 flex-1 resize-none overflow-hidden bg-transparent py-1 text-sm leading-5 text-primary placeholder:text-grey-300 focus:outline-none"
        style={{ minHeight: 28, maxHeight: 160 }}
      />
      <button
        type="button"
        className="flex h-7 w-7 shrink-0 items-center justify-center rounded-full bg-accent text-black transition-colors hover:bg-hover-accent"
      >
        <FaArrowUp size={12} />
      </button>
    </div>
  )
}