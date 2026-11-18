const GENERIC = 'An error has occurred. Try again later.'

// Model errors can arrive as a raw provider body (the gateway forwards the
// upstream rejection detail), and dumping that JSON into the transcript tells
// the reader nothing. Keep short, human messages; mask everything else.
export function errorText(raw: unknown): string {
  const text = typeof raw === 'string' ? raw.trim() : String(raw ?? '')
  if (!text) return GENERIC
  if (text.length > 160 || text.includes('{') || text.includes('"')) return GENERIC
  return text
}
