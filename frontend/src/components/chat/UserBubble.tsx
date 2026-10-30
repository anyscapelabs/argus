import Markdown from 'react-markdown'

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
      className={`markdown ml-auto w-fit max-w-xl rounded-xl border border-primary bg-secondary px-2 py-1 text-sm font-medium text-primary ${className}`}
    >
      <Markdown>{text}</Markdown>
    </div>
  )
}
