import { HiOutlineDocumentDuplicate } from "react-icons/hi"
import { HiOutlinePresentationChartBar } from "react-icons/hi"

type ArtifactCardProps = {
  // The stored title, which is the document's own front-matter title or its first
  // heading — the same string the agent was told it saved, and the one the panel
  // lists. There is no default: a card without a document to name should not be
  // on screen at all.
  title: string
  words?: number
  version?: number
  kind?: string
  onClick?: () => void
}

export default function ArtifactCard({ title, words, version = 1, kind = 'document', onClick }: ArtifactCardProps) {
  const isDeck = kind === 'deck'
  return (
    <button
      type="button"
      onClick={onClick}
      className="flex items-center gap-2 w-full h-14 rounded-lg bg-secondary px-2 transition-colors hover:bg-hover-secondary cursor-pointer text-left"
    >
      <div className={`flex h-8 w-8 items-center justify-center rounded-md ${isDeck ? 'bg-amber-500/10' : 'bg-blue-500/10'}`}>
        {isDeck ? (
          <HiOutlinePresentationChartBar size={18} className="text-amber-500" />
        ) : (
          <HiOutlineDocumentDuplicate size={18} className="text-blue-500" />
        )}
      </div>
      <div className="flex min-w-0 flex-col">
        <span className="truncate text-sm font-medium text-primary leading-tight">{title}</span>
        <span className="truncate text-xs font-medium text-secondary leading-tight">
          {/* The version is what tells a re-issued card apart from the original:
              the document keeps its title and its place in the conversation, so
              without the number the two read as duplicates. */}
          {isDeck ? 'Slides' : 'Artifact'} · v{version}{words ? ` · ${words.toLocaleString()} words` : ''}
        </span>
      </div>
    </button>
  )
}
