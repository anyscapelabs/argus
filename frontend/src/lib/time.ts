// Transcript time chips: a day changes, or hours pass between messages, the
// chat shows when — always with the day context ("Today at 3:47pm",
// "Yesterday at 6:03pm"), never a bare clock.

export const HOUR_MS = 60 * 60 * 1000

type Stamped = { created_at?: number; completed_at?: number }

// msgStamp prefers the finished time so an assistant reply is labeled when it
// actually landed, not when its turn began.
export function msgStamp(m: Stamped): number {
  return m.completed_at || m.created_at || 0
}

export function clockLabel(ms: number): string {
  const d = new Date(ms)
  const h = d.getHours()
  const m = String(d.getMinutes()).padStart(2, '0')
  const ampm = h >= 12 ? 'pm' : 'am'
  return `${h % 12 || 12}:${m}${ampm}`
}

// Local calendar day, not UTC: the user's "new day" is the one they live in.
export function dayKey(ms: number): string {
  const d = new Date(ms)
  return `${d.getFullYear()}-${d.getMonth()}-${d.getDate()}`
}

// Whole local days between the message and today (negative for future).
function daysAgo(ms: number): number {
  const now = new Date()
  const then = new Date(ms)
  const a = new Date(then.getFullYear(), then.getMonth(), then.getDate()).getTime()
  const b = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime()
  return Math.round((b - a) / (24 * 60 * 60 * 1000))
}

export function dayLabel(ms: number): string {
  const n = daysAgo(ms)
  if (n <= 0) return 'Today'
  if (n === 1) return 'Yesterday'
  const d = new Date(ms)
  // This week: the weekday name reads better than a date.
  if (n < 7) return d.toLocaleDateString(undefined, { weekday: 'long' })
  const md = d.toLocaleDateString(undefined, { month: 'short', day: 'numeric' })
  return n < 365 ? md : `${md}, ${d.getFullYear()}`
}

export function dayChipLabel(ms: number): string {
  return `${dayLabel(ms)} at ${clockLabel(ms)}`
}

// When a schedule fires next, with the same day context the chat chips use:
// "Today at 8:00am", "Tomorrow at 8:00am", then the weekday inside a week and
// the date beyond it. A fire time already spent is called what it is — the
// engine catches up on its next sweep, it does not silently reschedule.
export function nextFireLabel(ms: number, now = Date.now()): string {
  if (ms <= now) return 'Due now'
  const n = -daysAgo(ms)
  const d = new Date(ms)
  const time = clockLabel(ms)
  if (n === 0) return `Today at ${time}`
  if (n === 1) return `Tomorrow at ${time}`
  if (n < 7) return `${d.toLocaleDateString(undefined, { weekday: 'long' })} at ${time}`
  const md = d.toLocaleDateString(undefined, { month: 'short', day: 'numeric' })
  const dated = d.getFullYear() === new Date(now).getFullYear() ? md : `${md}, ${d.getFullYear()}`
  return `${dated} at ${time}`
}

// Details tab labels. Last active reads like activity feeds do — "Just now"
// while it is fresh, then coarse — while uptime counts the whole session.
export function lastActiveLabel(ms: number | undefined, now = Date.now()): string {
  if (!ms) return '—'
  const ago = now - ms
  if (ago < 2 * 60 * 1000) return 'Just now'
  if (ago < 60 * 60 * 1000) return `${Math.floor(ago / (60 * 1000))}m ago`
  if (ago < 24 * HOUR_MS) return `${Math.floor(ago / HOUR_MS)}h ago`
  return dayChipLabel(ms)
}
