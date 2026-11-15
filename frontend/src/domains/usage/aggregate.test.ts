import { describe, expect, it } from 'vitest'
import { aggregateUsage, dayKeyOf, relativeDayLabel, rangeKeyOf } from './aggregate'
import type { ModelRequestView } from '../../../bindings/github.com/anyscapelabs/argus/Services/Shared/models'
import type { UsageRows } from './api'

const DAY = 86_400_000

function row(overrides: Partial<ModelRequestView>): ModelRequestView {
  return {
    id: 'r',
    status: 'success',
    model_ref: 'm',
    input_tokens: 100,
    output_tokens: 0,
    cost_usd: 0.01,
    started_at: 0,
    completed_at: 0,
    ...overrides,
  }
}

describe('rangeKeyOf', () => {
  it('maps dropdown labels to range keys', () => {
    expect(rangeKeyOf('Last 24 hours')).toBe('24h')
    expect(rangeKeyOf('Last 30 days')).toBe('30d')
    expect(rangeKeyOf('Unknown')).toBe('30d')
  })
})

describe('aggregateUsage', () => {
  const now = Date.UTC(2026, 0, 20, 12, 0, 0)

  it('sums tokens, sessions and failures inside the range', () => {
    const data: UsageRows = {
      agents: [{ id: 'a', name: 'A', avatar: '', handle: 'a', role: '', last_message: '', last_message_kind: '', last_message_at: 0 }],
      requestsByAgent: {
        a: [
          row({ started_at: now - 1000, input_tokens: 100, output_tokens: 50 }),
          row({ started_at: now - 1000, status: 'error' }),
          row({ started_at: now - 40 * DAY }), // outside the 30-day window
        ],
      },
    }
    const s = aggregateUsage(data, '30d', now)
    expect(s.tokens).toBe(150)
    expect(s.requests).toBe(1)
    expect(s.failed).toBe(1)
    expect(s.agents[0].tokens).toBe(150)
    expect(s.agents[0].sessions).toBe(1)
  })

  it('counts used day buckets and the peak day', () => {
    const data: UsageRows = {
      agents: [{ id: 'a', name: 'A', avatar: '', handle: 'a', role: '', last_message: '', last_message_kind: '', last_message_at: 0 }],
      requestsByAgent: {
        a: [
          row({ started_at: now - 1 * DAY, input_tokens: 300 }),
          row({ started_at: now - 2 * DAY, input_tokens: 100 }),
          row({ started_at: now - 2 * DAY, input_tokens: 50 }),
          row({ started_at: now - 5 * DAY, input_tokens: 10 }),
        ],
      },
    }
    const s = aggregateUsage(data, '30d', now)
    expect(s.bucketUnit).toBe('day')
    expect(s.used).toBe(3)
    expect(s.total).toBe(30)
    expect(s.peakTokens).toBe(300)
    expect(s.tokensPerBucket).toBeCloseTo(460 / 3)
  })

  it('buckets by hour for the 24-hour range', () => {
    const data: UsageRows = {
      agents: [],
      requestsByAgent: {
        default: [row({ started_at: now - 1800_000, input_tokens: 200 })],
      },
    }
    const s = aggregateUsage(data, '24h', now)
    expect(s.bucketUnit).toBe('hour')
    expect(s.used).toBe(1)
    expect(s.total).toBe(24)
  })

  it('computes a streak of consecutive active days ending today', () => {
    const data: UsageRows = {
      agents: [],
      requestsByAgent: {
        default: [
          row({ started_at: now - 1000 }),
          row({ started_at: now - 1 * DAY }),
          row({ started_at: now - 2 * DAY }),
          row({ started_at: now - 4 * DAY }),
        ],
      },
    }
    expect(aggregateUsage(data, '30d', now).streakDays).toBe(3)
  })

  it('exposes 30-minute and day buckets for the calendar', () => {
    const t = now - 5 * 60_000
    const data: UsageRows = {
      agents: [],
      requestsByAgent: { default: [row({ started_at: t, input_tokens: 500 })] },
    }
    const s = aggregateUsage(data, '7d', now)
    const slot = Math.floor(t / (30 * 60_000)) * (30 * 60_000)
    expect(s.slotTokens.get(slot)).toBe(500)
    expect(s.dayTokens.get(dayKeyOf(t))).toBe(500)
  })
})

describe('relativeDayLabel', () => {
  it('labels recent activity', () => {
    const now = Date.UTC(2026, 0, 20, 12, 0, 0)
    expect(relativeDayLabel(now - 1000, now)).toBe('Today')
    expect(relativeDayLabel(now - DAY, now)).toBe('Yesterday')
    expect(relativeDayLabel(now - 3 * DAY, now)).toBe('3 days ago')
    expect(relativeDayLabel(0, now)).toBe('Never')
  })
})
