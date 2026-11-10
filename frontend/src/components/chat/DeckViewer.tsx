import { useState } from 'react'
import { FaChevronLeft, FaChevronRight } from 'react-icons/fa6'
import type { ArtifactView } from '../../domains/artifacts/hooks'

type Deck = {
  title?: string
  theme?: { background?: string; accent?: string; text?: string }
  slides?: { layout?: string; title?: string; subtitle?: string; bullets?: string[]; left?: string[]; right?: string[] }[]
}

// parseDeck reads the deck JSON out of an artifact body, tolerating a malformed
// body by returning an empty deck rather than crashing the view.
function parseDeck(body: string): Deck {
  try {
    const d = JSON.parse(body)
    return Array.isArray(d?.slides) ? d : { slides: [] }
  } catch {
    return { slides: [] }
  }
}

// DeckViewer is a slide-by-slide preview of a presentation. The artifact's body
// is the deck JSON; the theme colours it; navigation moves between pages. It is
// a preview, not an editor — the user saves the real .pptx separately.
export default function DeckViewer({ artifact }: { artifact: ArtifactView }) {
  const deck = parseDeck(artifact.body ?? '')
  const slides = deck.slides ?? []
  const [index, setIndex] = useState(0)
  const theme = deck.theme ?? {}
  const background = theme.background ?? '#1f2430'
  const accent = theme.accent ?? '#ff9d6c'
  const text = theme.text ?? '#f5f0e6'

  const current = slides[index] ?? {}

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="flex items-center justify-between px-1 py-2 text-xs text-secondary">
        <span>
          {slides.length ? `Slide ${index + 1} of ${slides.length}` : 'Empty deck'}
        </span>
        <span className="truncate max-w-[40%]">{deck.title ?? ''}</span>
      </div>
      <div
        className="relative flex flex-1 min-h-0 items-center justify-center rounded-xl p-4"
        style={{ backgroundColor: '#0b0c10' }}
      >
        {slides.length > 0 ? (
          <div
            className="relative flex h-full max-h-full w-full max-w-3xl select-none flex-col justify-between overflow-hidden rounded-lg p-8 shadow-xl"
            style={{ backgroundColor: background, color: text }}
          >
            <div className="absolute left-0 top-0 h-1 w-full" style={{ backgroundColor: accent }} />
            {current.layout === 'title' ? (
              <div className="flex flex-1 flex-col items-center justify-center text-center">
                <h1 className="text-4xl font-bold">{current.title}</h1>
                {current.subtitle && (
                  <p className="mt-3 text-lg" style={{ color: accent }}>{current.subtitle}</p>
                )}
              </div>
            ) : current.layout === 'section' ? (
              <div className="flex flex-1 flex-col items-center justify-center text-center">
                <h1 className="text-4xl font-bold">{current.title}</h1>
              </div>
            ) : current.layout === 'two_col' ? (
              <div className="flex flex-1 flex-col">
                <h2 className="mb-6 text-2xl font-bold">{current.title}</h2>
                <div className="grid flex-1 grid-cols-2 gap-8">
                  <DeckList items={current.left ?? []} accent={accent} />
                  <DeckList items={current.right ?? []} accent={accent} />
                </div>
              </div>
            ) : (
              <div className="flex flex-1 flex-col">
                <h2 className="mb-6 text-2xl font-bold">{current.title}</h2>
                <DeckList items={current.bullets ?? []} accent={accent} />
              </div>
            )}
            <div className="pt-4 text-right text-xs opacity-60">{index + 1}</div>
          </div>
        ) : (
          <div className="text-sm text-secondary">This deck has no slides.</div>
        )}
      </div>
      <div className="flex items-center justify-center gap-3 py-2">
        <button
          type="button"
          onClick={() => setIndex((i) => Math.max(0, i - 1))}
          disabled={index === 0}
          aria-label="Previous slide"
          className="flex h-7 w-7 items-center justify-center rounded-full bg-secondary text-primary transition-colors hover:bg-hover-secondary disabled:cursor-default disabled:opacity-40"
        >
          <FaChevronLeft size={12} />
        </button>
        <span className="text-xs text-secondary">{slides.length ? `${index + 1} / ${slides.length}` : ''}</span>
        <button
          type="button"
          onClick={() => setIndex((i) => Math.min(slides.length - 1, i + 1))}
          disabled={index >= slides.length - 1}
          aria-label="Next slide"
          className="flex h-7 w-7 items-center justify-center rounded-full bg-secondary text-primary transition-colors hover:bg-hover-secondary disabled:cursor-default disabled:opacity-40"
        >
          <FaChevronRight size={12} />
        </button>
      </div>
    </div>
  )
}

function DeckList({ items, accent }: { items: string[]; accent: string }) {
  if (items.length === 0) return null
  return (
    <ul className="space-y-3 text-lg">
      {items.map((item, i) => (
        <li key={i} className="flex gap-2">
          <span style={{ color: accent }}>•</span>
          <span>{item}</span>
        </li>
      ))}
    </ul>
  )
}
