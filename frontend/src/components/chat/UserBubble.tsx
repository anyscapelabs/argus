import Markdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import type { Block, BlockType } from '../../../bindings/github.com/anyscapelabs/argus/Protocol/models'

type UserBubbleProps = {
  content?: string
  children?: string
  blocks?: Block[] | null
  /** Extra class names for the bubble container. */
  className?: string
}

const imageType = 'image' as BlockType

// attachMarker heads the text block a document attachment rides in — the model
// reads the whole excerpt, but the transcript shows only the file chip.
const attachMarker = '[Attached file: '

// attachedFilename pulls the file's name out of an attachment block's header
// ("[Attached file: report.pdf]\n<contents>"). Returns null for ordinary text.
function attachedFilename(text: string): string | null {
  if (!text.startsWith(attachMarker)) return null
  const end = text.indexOf(']\n')
  if (end === -1) return null
  return text.slice(attachMarker.length, end)
}

export default function UserBubble({
  content,
  children,
  blocks,
  className = '',
}: UserBubbleProps) {
  const raw = content ?? children ?? ''
  const images = (blocks ?? []).filter((b) => b.type === imageType && b.image?.data)
  const files = (blocks ?? [])
    .map((b) => (b.type === 'text' ? attachedFilename(b.text ?? '') : null))
    .filter((name): name is string => !!name)
  // Attachment contents live in the blocks for the model; the visible message
  // is just what the user typed, which ends before the first attachment marker
  // (attachment text was appended after it).
  const markerAt = raw.indexOf(attachMarker)
  const text = markerAt === -1 ? raw : raw.slice(0, markerAt).trim()
  return (
    <div
      className={`ml-auto flex w-fit max-w-xl select-text flex-col gap-2 break-words rounded-xl border border-primary bg-secondary px-2 py-1 text-sm font-medium text-primary ${className}`}
    >
      {images.length > 0 && (
        <div className="flex flex-wrap gap-2 pt-1">
          {images.map((b, i) => (
            <img
              key={i}
              src={`data:${b.image?.media_type ?? 'image/png'};base64,${b.image?.data}`}
              alt="Attached image"
              className="max-h-40 max-w-56 rounded object-contain"
            />
          ))}
        </div>
      )}
      {files.length > 0 && (
        <div className="flex flex-wrap gap-2 pt-1">
          {files.map((name, i) => (
            <div
              key={i}
              className="flex items-center gap-2 rounded-lg border border-secondary bg-primary px-2 py-1 text-xs text-primary"
            >
              <span className="rounded bg-accent px-1 py-0.5 text-[10px] font-semibold uppercase text-black">
                {name.split('.').pop() || 'file'}
              </span>
              <span className="max-w-32 truncate">{name}</span>
            </div>
          ))}
        </div>
      )}
      {text && (
        <div className="markdown">
          <Markdown remarkPlugins={[remarkGfm]}>{text}</Markdown>
        </div>
      )}
    </div>
  )
}
