import type { ReactNode } from 'react'

// Frame for a GFM table. The grid itself is styled once, in index.css under
// "GFM tables". The frame is a fixed-width, bordered, rounded box that clips the
// corners and scrolls sideways when the table is wider than the bubble, so the
// bubble never has to resize to fit its data.
export default function MarkdownTable({ children }: { children?: ReactNode }) {
  return (
    <div className="my-2 max-w-full overflow-x-auto overscroll-x-contain rounded-xl border border-tertiary bg-primary [scrollbar-width:thin]">
      <table className="text-left text-xs">{children}</table>
    </div>
  )
}

// A GFM delimiter row (|---|:--:|) or, while the reply is still streaming and
// the delimiter has not arrived yet, a pipe-delimited header row. Either one
// tells the bubble to take its table measure now, so it does not jump wider
// the moment the delimiter row lands.
const TABLE_DELIMITER = /^\s*\|?\s*:?-{3,}:?\s*(\|\s*:?-{3,}:?\s*)+\|?\s*$/m
const TABLE_ROW = /^\s*\|[^|\n]*\|[^|\n]*\|\s*$/m

export function hasMarkdownTable(text: string): boolean {
  return TABLE_DELIMITER.test(text) || TABLE_ROW.test(text)
}
