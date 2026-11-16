import type { UsageRows } from './api'

export type RangeKey = '24h' | '7d' | '30d' | '6mo' | '365d'

export const RANGE_LABELS: Record<RangeKey, string> = {
  '24h': 'Last 24 hours',
  '7d': 'Last 7 days',
  '30d': 'Last 30 days',
  '6mo': 'Last 6 months',
  '365d': 'Last 365 days',
}

export function rangeKeyOf(label: string): RangeKey {
  switch (label) {
    case 'Last 24 hours':
      return '24h'
    case 'Last 7 days':
      return '7d'
    case 'Last 6 months':
      return '6mo'
    case 'Last 365 days':
      return '365d'
    default:
      return '30d'
  }
}

// Days each range spans; 24h buckets by hour instead.
const RANGE_DAYS: Record<Exclude<RangeKey, '24h'>, number> = {
  '7d': 7,
  '30d': 30,
  '6mo': 182,
  '365d': 365,
}

const SLOT_MS = 30 * 60 * 1000

export function dayKeyOf(ms: number): string {
  const d = new Date(ms)
  return `${d.getFullYear()}-${d.getMonth() + 1}-${d.getDate()}`
}

export interface AgentUsage {
  id: string
  name: string
  tokens: number
  sessions: number
  lastUsedAt: number
}

export interface UsageSummary {
  tokens: number
  requests: number
  failed: number
  costUSD: number
  // 'hour' for the 24-hour range, 'day' otherwise; used/total count filled
  // buckets of that unit.
  bucketUnit: 'hour' | 'day'
  used: number
  total: number
  tokensPerBucket: number
  peakTokens: number
  streakDays: number
  // 30-minute bucket start (ms, epoch) -> tokens, for the hour/day grids.
  slotTokens: Map<number, number>
  // Local day (see dayKeyOf) -> tokens, for the day grids.
  dayTokens: Map<string, number>
  agents: AgentUsage[]
}

// Buckets are local-time aligned so the calendar grids (which build local
// Dates) can look rows up directly.
export function aggregateUsage(data: UsageRows | undefined, range: RangeKey, now = Date.now()): UsageSummary {
  const empty: UsageSummary = {
    tokens: 0, requests: 0, failed: 0, costUSD: 0,
    bucketUnit: range === '24h' ? 'hour' : 'day',
    used: 0, total: range === '24h' ? 24 : range === '7d' ? 7 : RANGE_DAYS[range as Exclude<RangeKey, '24h'>],
    tokensPerBucket: 0, peakTokens: 0, streakDays: 0,
    slotTokens: new Map(), dayTokens: new Map(), agents: [],
  }
  if (!data) return empty

  const startMs = range === '24h' ? now - 24 * 3600_000 : now - RANGE_DAYS[range as Exclude<RangeKey, '24h'>] * 86_400_000

  const slotTokens = new Map<number, number>()
  const dayTokens = new Map<string, number>()
  const hourTokens = new Map<number, number>()
  let tokens = 0
  let requests = 0
  let failed = 0
  let costUSD = 0

  // Rows are keyed by agent, and agents may exist without rows, so walk the
  // union; unknown ids fall back to a placeholder name.
  const ids = new Set<string>([...data.agents.map((a) => a.id), ...Object.keys(data.requestsByAgent)])
  const names = new Map(data.agents.map((a) => [a.id, a.name] as const))
  const agents: AgentUsage[] = [...ids].map((id) => {
    const rows = data.requestsByAgent[id] ?? []
    let agentTokens = 0
    let sessions = 0
    let lastUsedAt = 0
    for (const r of rows) {
      if (r.started_at < startMs || r.started_at > now) {
        if (r.started_at > lastUsedAt) lastUsedAt = r.started_at
        continue
      }
      if (r.started_at > lastUsedAt) lastUsedAt = r.started_at
      if (r.status === 'error') {
        failed++
        continue
      }
      const rowTokens = r.input_tokens + r.output_tokens
      tokens += rowTokens
      agentTokens += rowTokens
      costUSD += r.cost_usd
      requests++
      sessions++
      const slot = Math.floor(r.started_at / SLOT_MS) * SLOT_MS
      slotTokens.set(slot, (slotTokens.get(slot) ?? 0) + rowTokens)
      hourTokens.set(Math.floor(r.started_at / 3600_000) * 3600_000, (hourTokens.get(Math.floor(r.started_at / 3600_000) * 3600_000) ?? 0) + rowTokens)
      const dayKey = dayKeyOf(r.started_at)
      dayTokens.set(dayKey, (dayTokens.get(dayKey) ?? 0) + rowTokens)
    }
    return { id, name: names.get(id) ?? 'Agent', tokens: agentTokens, sessions, lastUsedAt }
  })

  const hourly = range === '24h'
  const buckets = hourly ? hourTokens : dayTokens
  let used = 0
  let peakTokens = 0
  for (const tokensOf of buckets.values()) {
    if (tokensOf > 0) used++
    if (tokensOf > peakTokens) peakTokens = tokensOf
  }
  const total = hourly ? 24 : RANGE_DAYS[range as Exclude<RangeKey, '24h'>]

  let streakDays = 0
  {
    const today = new Date(now)
    today.setHours(0, 0, 0, 0)
    let cursor = today.getTime()
    if ((dayTokens.get(dayKeyOf(cursor)) ?? 0) === 0) cursor -= 86_400_000
    while ((dayTokens.get(dayKeyOf(cursor)) ?? 0) > 0) {
      streakDays++
      cursor -= 86_400_000
    }
  }

  agents.sort((a, b) => b.tokens - a.tokens)
  return {
    tokens, requests, failed, costUSD,
    bucketUnit: hourly ? 'hour' : 'day',
    used, total,
    tokensPerBucket: used > 0 ? tokens / used : 0,
    peakTokens, streakDays,
    slotTokens, dayTokens, agents,
  }
}

export function relativeDayLabel(ms: number, now = Date.now()): string {
  if (!ms) return 'Never'
  const dayOf = (t: number) => {
    const d = new Date(t)
    d.setHours(0, 0, 0, 0)
    return d.getTime()
  }
  const diffDays = Math.round((dayOf(now) - dayOf(ms)) / 86_400_000)
  if (diffDays <= 0) return 'Today'
  if (diffDays === 1) return 'Yesterday'
  if (diffDays < 30) return `${diffDays} days ago`
  return new Date(ms).toLocaleDateString(undefined, { day: 'numeric', month: 'short' })
}
