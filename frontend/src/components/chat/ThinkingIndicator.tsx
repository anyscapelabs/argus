export type ThinkingIndicatorProps = {
  /** Live status text to show instead of the static Thinking… label. */
  text?: string | null
}

export default function ThinkingIndicator({ text }: ThinkingIndicatorProps) {
  return (
    <div className="w-fit rounded-xl bg-secondary px-2 py-1 text-sm font-medium">
      <span className="shimmer-text">{text || 'Thinking…'}</span>
    </div>
  )
}
