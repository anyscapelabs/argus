import Markdown from 'react-markdown'
import remarkGfm from 'remark-gfm'

type UserBubbleProps = {
  content?: string
  children?: string
  /** Extra class names for the bubble container. */
  className?: string
}

export default function UserBubble({
  content,
  children,
  className = '',
}: UserBubbleProps) {
  const text = content ?? children ?? ''
  return (
    <div
      className={`markdown ml-auto w-fit max-w-xl select-text break-words rounded-xl border border-primary bg-secondary px-2 py-1 text-sm font-medium text-primary ${className}`}
    >
      <Markdown remarkPlugins={[remarkGfm]}>{text}</Markdown>
    </div>
  )
}
