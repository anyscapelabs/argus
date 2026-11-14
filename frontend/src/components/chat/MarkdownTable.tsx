import type { ReactNode } from 'react'

// A GFM table that keeps its natural column widths. It used to be `w-full`
// inside a shrink-to-fit bubble with `overflow-hidden`, so the browser squeezed
// every column to its narrowest word (breaking words mid-letter) and clipped
// whatever still did not fit. Now the table sizes to its content, never below
// the bubble, and scrolls sideways inside its own frame when it is wider.
export default function MarkdownTable({ children }: { children?: ReactNode }) {
  return (
    <div className="my-2 max-w-full overflow-x-auto rounded-2xl">
      <table className="w-max min-w-full border-separate border-spacing-0 text-left text-xs">{children}</table>
    </div>
  )
}

// True when the markdown holds a GFM table (a delimiter row like |---|:--:|),
// so the bubble can allow itself more room than a plain paragraph needs.
const TABLE_DELIMITER = /^\s*\|?\s*:?-{3,}:?\s*(\|\s*:?-{3,}:?\s*)+\|?\s*$/m

export function hasMarkdownTable(text: string): boolean {
  return TABLE_DELIMITER.test(text)
}
