import Markdown from 'react-markdown'

type AgentBubbleProps = {
  content?: string
  error?: string
  status?: string
  children?: string
  /** Extra class names for the bubble container. */
  className?: string
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
      <Markdown>{text}</Markdown>
      {failed && error && (
        <div className="mt-1 border-t border-red-400/40 pt-1 text-xs text-red-400">
          <Markdown>{error}</Markdown>
        </div>
      )}
    </div>
  )
}
