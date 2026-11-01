// Decides what the thinking indicator shows and for how long.
//
// The model streams tens of tokens a second and a local command can finish in
// a few milliseconds, so wiring the indicator straight to those events makes
// it flicker faster than anyone can read. This scheduler sits in between:
//
//  - every status stays on screen for a minimum time (dwell)
//  - the generic "Thinking…" label waits a beat before appearing, so a reply
//    that starts at once never flashes it
//  - while a status is on screen only the newest request is kept, so a burst
//    of updates collapses into one swap instead of a queue that falls behind
//  - a more important kind (tool > thought > label) replaces a less important
//    one right away, so "Running df -h" is never stuck behind a thought

export type StatusKind = 'label' | 'thought' | 'tool'

const PRIORITY: Record<StatusKind, number> = { label: 0, thought: 1, tool: 2 }

export type StatusTimings = {
  /** How long a label waits before it first appears. */
  labelDelayMs: number
  /** Minimum time each kind stays on screen once shown. */
  labelDwellMs: number
  thoughtDwellMs: number
  toolDwellMs: number
}

export const DEFAULT_TIMINGS: StatusTimings = {
  labelDelayMs: 300,
  labelDwellMs: 500,
  thoughtDwellMs: 1800,
  toolDwellMs: 1500,
}

type Status = { text: string; kind: StatusKind }
type Shown = Status & { at: number }

export class StatusLine {
  private shown: Shown | null = null
  private wanted: Status | null = null
  private wantedAt = 0
  private timer: ReturnType<typeof setTimeout> | null = null

  constructor(
    private readonly onChange: (text: string | null) => void,
    private readonly t: StatusTimings = DEFAULT_TIMINGS,
  ) {}

  /** Ask for a status. The newest request wins; dwell rules decide when it appears. */
  set(text: string, kind: StatusKind) {
    if (this.wanted?.text === text && this.wanted.kind === kind) return
    this.wanted = { text, kind }
    this.wantedAt = Date.now()
    this.tick()
  }

  /** Ask for the status to go away once it has had its minimum time. */
  clear() {
    this.wanted = null
    this.tick()
  }

  /**
   * The answer has started streaming. Labels and thoughts are no longer
   * useful, so they go at once; a tool status still gets its minimum time.
   */
  endThinking() {
    this.wanted = null
    if (this.shown && this.shown.kind !== 'tool') this.hide()
    else this.tick()
  }

  /** Hide immediately and forget everything (run finished or cancelled). */
  reset() {
    this.wanted = null
    this.hide()
  }

  dispose() {
    this.cancelTimer()
  }

  private tick() {
    this.cancelTimer()
    const now = Date.now()
    const { shown, wanted } = this

    if (!shown) {
      if (!wanted) return
      // Real content shows at once. Only the generic label waits a beat.
      const wait = wanted.kind === 'label' ? this.t.labelDelayMs - (now - this.wantedAt) : 0
      if (wait > 0) {
        this.later(wait)
        return
      }
      this.show(wanted, now)
      return
    }

    if (wanted && wanted.text === shown.text) return

    const outranks = wanted !== null && PRIORITY[wanted.kind] > PRIORITY[shown.kind]
    const left = this.dwell(shown.kind) - (now - shown.at)
    if (!outranks && left > 0) {
      this.later(left)
      return
    }
    if (wanted) this.show(wanted, now)
    else this.hide()
  }

  private show(s: Status, now: number) {
    this.shown = { ...s, at: now }
    this.onChange(s.text)
  }

  private hide() {
    this.cancelTimer()
    if (!this.shown) return
    this.shown = null
    this.onChange(null)
  }

  private dwell(kind: StatusKind): number {
    if (kind === 'tool') return this.t.toolDwellMs
    if (kind === 'thought') return this.t.thoughtDwellMs
    return this.t.labelDwellMs
  }

  private later(ms: number) {
    this.timer = setTimeout(() => {
      this.timer = null
      this.tick()
    }, ms)
  }

  private cancelTimer() {
    if (this.timer) {
      clearTimeout(this.timer)
      this.timer = null
    }
  }
}

/**
 * The newest complete sentence of a streaming thought, cleaned for display,
 * or null when no sentence has finished yet. A sentence counts as finished
 * once punctuation is followed by whitespace, or a line break ends it, so a
 * half-written sentence never reaches the screen.
 */
export function latestSentence(thought: string): string | null {
  // Only the tail matters, and scanning less keeps this cheap per token.
  const tail = thought.slice(-700)
  const parts = tail.match(/(?:\d\.\d|[^.!?\n])+[.!?]+(?=\s)|[^\n]+(?=\n)/g)
  const last = parts?.[parts.length - 1]
  if (!last) return null
  const clean = last
    .replace(/\*+|`+/g, '')
    .replace(/\s+/g, ' ')
    .replace(/^[#>\-\s]+/, '')
    .trim()
  return clean.length >= 12 ? clean.slice(0, 200) : null
}
