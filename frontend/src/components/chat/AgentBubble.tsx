import Markdown from 'react-markdown'

type AgentBubbleProps = {
  content?: string
  children?: string
  /** Extra class names for the bubble container. */
  className?: string
}

export default function AgentBubble({
  content,
  children,
  className = '',
}: AgentBubbleProps) {
  const text = content ?? children ?? ''
  return (
    <div
      className={`markdown w-fit max-w-xl rounded-xl border border-primary bg-secondary px-2 py-1 text-sm font-medium ${className}`}
    >
      <Markdown>{text}</Markdown>
    </div>
  )
}