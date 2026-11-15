import { useState } from 'react'
import { dayKeyOf } from '../../domains/usage/aggregate'

// Cells read real token counts out of the page's aggregated buckets; an
// absent bucket is an idle slot. Levels scale against the busiest cell.
function tokensForSlot(date: Date, slotTokens?: Map<number, number>): number {
  const slot = Math.floor(date.getTime() / (30 * 60 * 1000)) * (30 * 60 * 1000)
  return slotTokens?.get(slot) ?? 0
}

function tokensForDay(date: Date, dayTokens?: Map<string, number>): number {
  return dayTokens?.get(dayKeyOf(date.getTime())) ?? 0
}

function levelFor(tokens: number, max: number): number {
  if (tokens <= 0 || max <= 0) return 0
  return Math.min(4, 1 + Math.floor((tokens / max) * 4))
}

function formatTokens(n: number): string {
  if (n >= 1_000_000_000) return `${Math.round(n / 1_000_000_000)}B`
  if (n >= 1_000_000) return `${Math.round(n / 1_000_000)}M`
  if (n >= 1_000) return `${Math.round(n / 1_000)}K`
  return `${n}`
}

function formatHourLabel(date: Date): string {
  const h = date.getHours()
  const ampm = h >= 12 ? 'PM' : 'AM'
  const hour = h % 12 === 0 ? 12 : h % 12
  return `${hour}${ampm}`
}

function formatHoverDate(date: Date): string {
  return `${date.toLocaleDateString(undefined, { weekday: 'short', day: 'numeric', month: 'short' })}, ${formatHourLabel(date)}`
}

function formatDayHoverDate(date: Date): string {
  return date.toLocaleDateString(undefined, { weekday: 'short', day: 'numeric', month: 'short' })
}

const WEEKDAYS = ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun']

function buildDaySlots(day: Date): Date[] {
  const start = new Date(day)
  start.setHours(0, 0, 0, 0)
  return Array.from({ length: 48 }, (_, i) => {
    const d = new Date(start)
    d.setMinutes(i * 30)
    return d
  })
}

function buildWeekDays(): Date[] {
  const today = new Date()
  today.setHours(0, 0, 0, 0)
  const day = today.getDay() // 0 Sun
  const diffToMon = day === 0 ? -6 : 1 - day
  const monday = new Date(today)
  monday.setDate(today.getDate() + diffToMon)
  return Array.from({ length: 7 }, (_, i) => {
    const d = new Date(monday)
    d.setDate(monday.getDate() + i)
    return d
  })
}

function buildLast30Weeks(): (Date | null)[][] {
  const today = new Date()
  today.setHours(0, 0, 0, 0)
  const start = new Date(today)
  start.setDate(today.getDate() - 29)

  const cells: (Date | null)[] = []
  const startDow = start.getDay() === 0 ? 6 : start.getDay() - 1 // Mon=0
  for (let i = 0; i < startDow; i++) cells.push(null)
  for (let i = 0; i < 30; i++) {
    const d = new Date(start)
    d.setDate(start.getDate() + i)
    cells.push(d)
  }
  while (cells.length % 7 !== 0) cells.push(null)

  const weeks: (Date | null)[][] = []
  for (let i = 0; i < cells.length; i += 7) weeks.push(cells.slice(i, i + 7))
  return weeks
}

export default function UsageCalendar({
  range,
  slotTokens,
  dayTokens,
}: {
  range: string
  slotTokens?: Map<number, number>
  dayTokens?: Map<string, number>
}) {
  const [hovered, setHovered] = useState<{ date: Date; tokens: number } | null>(null)

  if (range === 'Last 24 hours') {
    const now = new Date()
    now.setMinutes(now.getMinutes() < 30 ? 0 : 30, 0, 0)
    const start = new Date(now)
    start.setMinutes(now.getMinutes() - 47 * 30)
    const slots = Array.from({ length: 48 }, (_, i) => {
      const d = new Date(start)
      d.setMinutes(start.getMinutes() + i * 30)
      return d
    })
    const slotList = slots.map((d) => ({ date: d, tokens: tokensForSlot(d, slotTokens) }))
    const maxTokens = Math.max(0, ...slotList.map((s) => s.tokens))

    return (
      <div className="flex w-full flex-col gap-2" onMouseLeave={() => setHovered(null)}>
        <div className="flex w-full">
          {slots.map((d, i) => (
            <div key={i} className="flex-1 text-center text-xs text-secondary">
              {i % 6 === 0 ? formatHourLabel(d) : ''}
            </div>
          ))}
        </div>
        <div className="flex w-full">
          {slotList.map((slot, i) => {
            const level = levelFor(slot.tokens, maxTokens)
            return (
              <div key={i} className="flex flex-1 justify-center">
                <div
                  onMouseEnter={() => setHovered({ date: slot.date, tokens: slot.tokens })}
                  className="h-5 w-2.5 rounded-sm transition-shadow hover:ring-2 hover:ring-accent-secondary hover:ring-offset-1 hover:ring-offset-primary"
                  style={{
                    backgroundColor:
                      level === 0 ? 'var(--color-tertiary, #363636)' : 'var(--color-accent-secondary, #7B61FF)',
                    opacity: level === 0 ? 1 : [0, 0.25, 0.5, 0.75, 1][level],
                  }}
                />
              </div>
            )
          })}
        </div>
        <div className="flex items-center justify-between text-xs text-secondary">
          <span>{hovered ? `${formatHoverDate(hovered.date)} , ${formatTokens(hovered.tokens)} tokens` : ''}</span>
          <div className="flex items-center gap-1">
            <span>Less</span>
            <div className="h-3 w-3 rounded-sm" style={{ backgroundColor: 'var(--color-tertiary, #363636)' }} />
            <div className="h-3 w-3 rounded-sm" style={{ backgroundColor: 'var(--color-accent-secondary, #7B61FF)', opacity: 0.25 }} />
            <div className="h-3 w-3 rounded-sm" style={{ backgroundColor: 'var(--color-accent-secondary, #7B61FF)', opacity: 0.5 }} />
            <div className="h-3 w-3 rounded-sm" style={{ backgroundColor: 'var(--color-accent-secondary, #7B61FF)', opacity: 0.75 }} />
            <div className="h-3 w-3 rounded-sm" style={{ backgroundColor: 'var(--color-accent-secondary, #7B61FF)' }} />
            <span>More</span>
          </div>
        </div>
      </div>
    )
  }

  if (range === 'Last 7 days') {
    const days = buildWeekDays()
    const slotList = days.flatMap((day) =>
      buildDaySlots(day).map((d) => ({ date: d, tokens: tokensForSlot(d, slotTokens) })),
    )
    const maxTokens = Math.max(0, ...slotList.map((s) => s.tokens))
    return (
      <div className="flex w-full flex-col gap-2" onMouseLeave={() => setHovered(null)}>
        <div className="flex flex-col gap-2">
          <div className="flex items-center gap-2">
            <div className="w-8 shrink-0" />
            <div className="flex flex-1">
              {buildDaySlots(days[0]).map((d, i) => (
                <div key={i} className="flex-1 text-center text-xs text-secondary">
                  {i % 6 === 0 ? formatHourLabel(d) : ''}
                </div>
              ))}
            </div>
          </div>
          {days.map((day, di) => {
            const slots = buildDaySlots(day)
            return (
              <div key={di} className="flex items-center gap-2">
                <div className="w-8 shrink-0 text-xs text-secondary">{WEEKDAYS[di]}</div>
                <div className="flex flex-1">
                  {slots.map((d, i) => {
                    const tokens = tokensForSlot(d, slotTokens)
                    const level = levelFor(tokens, maxTokens)
                    return (
                      <div key={i} className="flex flex-1 justify-center">
                        <div
                          onMouseEnter={() => setHovered({ date: d, tokens })}
                          className="h-3 w-3 rounded-full transition-shadow hover:ring-2 hover:ring-accent-secondary hover:ring-offset-1 hover:ring-offset-primary"
                          style={{
                            backgroundColor:
                              level === 0 ? 'var(--color-tertiary, #363636)' : 'var(--color-accent-secondary, #7B61FF)',
                            opacity: level === 0 ? 1 : [0, 0.25, 0.5, 0.75, 1][level],
                          }}
                        />
                      </div>
                    )
                  })}
                </div>
              </div>
            )
          })}
        </div>
        <div className="flex items-center justify-between text-xs text-secondary">
          <span>{hovered ? `${formatHoverDate(hovered.date)} , ${formatTokens(hovered.tokens)} tokens` : ''}</span>
          <div className="flex items-center gap-1">
            <span>Less</span>
            <div className="h-3 w-3 rounded-sm" style={{ backgroundColor: 'var(--color-tertiary, #363636)' }} />
            <div className="h-3 w-3 rounded-sm" style={{ backgroundColor: 'var(--color-accent-secondary, #7B61FF)', opacity: 0.25 }} />
            <div className="h-3 w-3 rounded-sm" style={{ backgroundColor: 'var(--color-accent-secondary, #7B61FF)', opacity: 0.5 }} />
            <div className="h-3 w-3 rounded-sm" style={{ backgroundColor: 'var(--color-accent-secondary, #7B61FF)', opacity: 0.75 }} />
            <div className="h-3 w-3 rounded-sm" style={{ backgroundColor: 'var(--color-accent-secondary, #7B61FF)' }} />
            <span>More</span>
          </div>
        </div>
      </div>
    )
  }

  if (range === 'Last 30 days') {
    const weeks = buildLast30Weeks()
    const maxTokens = Math.max(0, ...(weeks.flat().filter((d): d is Date => d !== null).map((d) => tokensForDay(d, dayTokens))))
    return (
      <div className="flex w-full flex-col gap-2" onMouseLeave={() => setHovered(null)}>
        <div className="flex flex-col gap-1">
          <div className="grid grid-cols-7 gap-1">
            {WEEKDAYS.map((d) => (
              <div key={d} className="text-center text-xs text-secondary">
                {d}
              </div>
            ))}
          </div>
          {weeks.map((week, wi) => (
            <div key={wi} className="grid grid-cols-7 gap-1">
              {week.map((day, di) => {
                if (!day) return <div key={di} className="h-10 w-full rounded-md" />
                const tokens = tokensForDay(day, dayTokens)
                const level = levelFor(tokens, maxTokens)
                return (
                  <div
                    key={di}
                    onMouseEnter={() => setHovered({ date: day, tokens })}
                    className="flex h-10 w-full cursor-default items-start justify-start rounded-md p-1.5 text-xs transition-shadow hover:ring-2 hover:ring-accent-secondary hover:ring-offset-1 hover:ring-offset-primary"
                    style={{
                      backgroundColor:
                        level === 0 ? 'var(--color-tertiary, #363636)' : 'var(--color-accent-secondary, #7B61FF)',
                      opacity: level === 0 ? 1 : [0, 0.25, 0.5, 0.75, 1][level],
                    }}
                  >
                    <span className="text-primary">{day.getDate()}</span>
                  </div>
                )
              })}
            </div>
          ))}
        </div>
        <div className="flex items-center justify-between text-xs text-secondary">
          <span>{hovered ? `${formatDayHoverDate(hovered.date)} , ${formatTokens(hovered.tokens)} tokens` : ''}</span>
          <div className="flex items-center gap-1">
            <span>Less</span>
            <div className="h-3 w-3 rounded-sm" style={{ backgroundColor: 'var(--color-tertiary, #363636)' }} />
            <div className="h-3 w-3 rounded-sm" style={{ backgroundColor: 'var(--color-accent-secondary, #7B61FF)', opacity: 0.25 }} />
            <div className="h-3 w-3 rounded-sm" style={{ backgroundColor: 'var(--color-accent-secondary, #7B61FF)', opacity: 0.5 }} />
            <div className="h-3 w-3 rounded-sm" style={{ backgroundColor: 'var(--color-accent-secondary, #7B61FF)', opacity: 0.75 }} />
            <div className="h-3 w-3 rounded-sm" style={{ backgroundColor: 'var(--color-accent-secondary, #7B61FF)' }} />
            <span>More</span>
          </div>
        </div>
      </div>
    )
  }

  if (range === 'Last 6 months' || range === 'Last 365 days') {
    const daysBack = range === 'Last 365 days' ? 364 : 181
    const today = new Date()
    today.setHours(0, 0, 0, 0)
    const start = new Date(today)
    start.setDate(today.getDate() - daysBack)
    const cursor = new Date(start)
    const dow = cursor.getDay() === 0 ? 6 : cursor.getDay() - 1
    cursor.setDate(cursor.getDate() - dow)

    const weeks: (Date | null)[][] = []
    let week: (Date | null)[] = []
    const iter = new Date(cursor)
    while (iter <= today || week.length > 0) {
      const copy = new Date(iter)
      const inRange = copy >= start && copy <= today
      week.push(inRange ? copy : null)
      if (week.length === 7) {
        weeks.push(week)
        week = []
        if (copy > today) break
      }
      iter.setDate(iter.getDate() + 1)
    }

    const monthLabels: string[] = []
    const maxTokens = Math.max(0, ...(weeks.flat().filter((d): d is Date => d !== null).map((d) => tokensForDay(d, dayTokens))))
    let lastMonth = -1
    let lastYear = -1
    for (let wi = 0; wi < weeks.length; wi++) {
      const first = weeks[wi].find((d) => d !== null)
      if (!first) {
        monthLabels.push('')
        continue
      }
      const prev = wi > 0 ? weeks[wi - 1].find((d) => d !== null) : null
      const changed = !prev || first.getMonth() !== prev.getMonth() || first.getFullYear() !== prev.getFullYear()
      if (changed && (first.getMonth() !== lastMonth || first.getFullYear() !== lastYear)) {
        monthLabels.push(first.toLocaleDateString(undefined, { month: 'short' }))
        lastMonth = first.getMonth()
        lastYear = first.getFullYear()
      } else {
        monthLabels.push('')
      }
    }

    return (
      <div className="flex w-full flex-col gap-2" onMouseLeave={() => setHovered(null)}>
        <div className="flex w-full gap-2">
          <div className="flex flex-col gap-1 text-[10px] text-secondary">
            <div className="h-4 leading-4"></div>
            <div className="h-3 leading-3">Mon</div>
            <div className="h-3 leading-3"></div>
            <div className="h-3 leading-3">Wed</div>
            <div className="h-3 leading-3"></div>
            <div className="h-3 leading-3">Fri</div>
            <div className="h-3 leading-3"></div>
            <div className="h-3 leading-3">Sun</div>
          </div>
          <div className="flex min-w-0 flex-1 flex-col gap-1">
            <div className="flex h-4 w-full justify-between">
              {weeks.map((_, wi) => (
                <div key={wi} className="w-3 overflow-visible whitespace-nowrap text-[10px] leading-4 text-secondary">
                  {monthLabels[wi]}
                </div>
              ))}
            </div>
            <div className="flex w-full justify-between">
              {weeks.map((w, wi) => (
                <div key={wi} className="flex flex-col gap-1">
                  {w.map((day, di) => {
                    if (!day) return <div key={di} className="h-3 w-3" />
                    const tokens = tokensForDay(day, dayTokens)
                    const level = levelFor(tokens, maxTokens)
                    return (
                      <div
                        key={di}
                        onMouseEnter={() => setHovered({ date: day, tokens })}
                        className="h-3 w-3 rounded-full transition-shadow hover:ring-2 hover:ring-accent-secondary hover:ring-offset-1 hover:ring-offset-primary"
                        style={{
                          backgroundColor:
                            level === 0 ? 'var(--color-tertiary, #363636)' : 'var(--color-accent-secondary, #7B61FF)',
                          opacity: level === 0 ? 1 : [0, 0.25, 0.5, 0.75, 1][level],
                        }}
                      />
                    )
                  })}
                </div>
              ))}
            </div>
          </div>
        </div>
        <div className="flex items-center justify-between text-xs text-secondary">
          <span>{hovered ? `${formatDayHoverDate(hovered.date)} , ${formatTokens(hovered.tokens)} tokens` : ''}</span>
          <div className="flex items-center gap-1">
            <span>Less</span>
            <div className="h-3 w-3 rounded-sm" style={{ backgroundColor: 'var(--color-tertiary, #363636)' }} />
            <div className="h-3 w-3 rounded-sm" style={{ backgroundColor: 'var(--color-accent-secondary, #7B61FF)', opacity: 0.25 }} />
            <div className="h-3 w-3 rounded-sm" style={{ backgroundColor: 'var(--color-accent-secondary, #7B61FF)', opacity: 0.5 }} />
            <div className="h-3 w-3 rounded-sm" style={{ backgroundColor: 'var(--color-accent-secondary, #7B61FF)', opacity: 0.75 }} />
            <div className="h-3 w-3 rounded-sm" style={{ backgroundColor: 'var(--color-accent-secondary, #7B61FF)' }} />
            <span>More</span>
          </div>
        </div>
      </div>
    )
  }

  return <div className="w-full" />
}
