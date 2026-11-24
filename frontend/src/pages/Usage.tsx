import { useEffect, useMemo, useRef, useState } from 'react'
import { IoChevronDownOutline } from 'react-icons/io5'
import UsageCalendar from '../components/usage/UsageCalendar'
import UsageLimits from '../components/usage/UsageLimits'
import { useUsageOverview } from '../domains/usage/hooks'
import {
  aggregateUsage,
  rangeKeyOf,
  relativeDayLabel,
  type RangeKey,
} from '../domains/usage/aggregate'

const RANGES = [
  'Last 24 hours',
  'Last 7 days',
  'Last 30 days',
  'Last 6 months',
  'Last 365 days',
]

function formatTokens(n: number): string {
  if (n >= 1_000_000_000) return `${(n / 1_000_000_000).toFixed(1).replace(/\.0$/, '')}B`
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1).replace(/\.0$/, '')}M`
  if (n >= 1_000) return `${(n / 1_000).toFixed(1).replace(/\.0$/, '')}K`
  return `${Math.round(n)}`
}

export default function Usage() {
  const [range, setRange] = useState('Last 30 days')
  const [open, setOpen] = useState(false)
  const menuRef = useRef<HTMLDivElement | null>(null)
  const { data: rows } = useUsageOverview()

  useEffect(() => {
    if (!open) return
    const handler = (e: MouseEvent) => {
      if (menuRef.current && !menuRef.current.contains(e.target as Node)) {
        setOpen(false)
      }
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [open])

  const rangeKey: RangeKey = rangeKeyOf(range)
  const summary = useMemo(() => aggregateUsage(rows, rangeKey), [rows, rangeKey])

  const streakLabel = summary.streakDays > 0 ? `${summary.streakDays} Day Streak` : 'No Streak Yet'
  const bucketLabel = summary.bucketUnit === 'hour' ? 'Hours you used agent for' : 'Days you used agent for'
  const perLabel = summary.bucketUnit === 'hour' ? 'per hour' : 'per day'
  const peakLabel = summary.bucketUnit === 'hour' ? 'Peak hour' : 'Peak day'

  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-y-auto bg-primary">
      <div className="mx-auto flex w-full max-w-3xl flex-1 flex-col gap-6 px-6 pb-8 pt-8">
        <div className="flex flex-col gap-1">
          <h1 className="text-xl font-medium text-primary">Usage</h1>
          <p className="text-sm text-secondary">Track your account usage and limits.</p>
        </div>
        <UsageLimits />

        <div className="flex items-center justify-between gap-4">
          <div className="flex flex-col gap-1">
            <p className="text-2xl font-medium text-primary">{streakLabel}</p>
            <p className="text-sm text-secondary">
              {formatTokens(summary.tokens)} tokens in the {range.toLowerCase()}
            </p>
          </div>
          <div ref={menuRef} className="relative">
            <button
              type="button"
              onClick={() => setOpen((v) => !v)}
              className="flex cursor-pointer items-center gap-1 rounded-full border border-secondary bg-transparent px-2 py-1 text-sm text-primary transition-colors hover:bg-hover-secondary"
            >
              <span>{range}</span>
              <IoChevronDownOutline size={14} className="text-secondary" />
            </button>
            {open && (
              <div className="absolute right-0 z-20 mt-1 min-w-[10rem] rounded-xl border border-primary bg-primary p-1 shadow-lg">
                {RANGES.map((option) => (
                  <button
                    key={option}
                    type="button"
                    onClick={() => {
                      setRange(option)
                      setOpen(false)
                    }}
                    className={`flex w-full cursor-pointer items-center rounded-lg px-2 py-1 text-left text-sm transition-colors hover:bg-hover-primary ${
                      option === range ? 'text-primary' : 'text-secondary'
                    }`}
                  >
                    {option}
                  </button>
                ))}
              </div>
            )}
          </div>
        </div>

        <UsageCalendar range={range} slotTokens={summary.slotTokens} dayTokens={summary.dayTokens} />
        <div className="border-t border-primary" />
        <div className="flex w-full items-start justify-between gap-4">
          <div className="flex flex-col gap-1">
            <p className="text-2xl text-primary">
              <span className="font-mono">{summary.used}</span> of <span className="font-mono">{summary.total}</span>
            </p>
            <p className="text-sm text-secondary">{bucketLabel}</p>
          </div>
          <div className="flex flex-col gap-1">
            <p className="text-2xl text-primary">
              <span className="font-mono">{formatTokens(summary.tokensPerBucket)}</span>
            </p>
            <p className="text-sm text-secondary">Tokens {perLabel}</p>
          </div>
          <div className="flex flex-col gap-1">
            <p className="text-2xl text-primary">
              <span className="font-mono">{formatTokens(summary.peakTokens)}</span>
            </p>
            <p className="text-sm text-secondary">{peakLabel}</p>
          </div>
        </div>

        <div className="flex flex-col gap-3">
          <h2 className="text-sm font-medium text-primary">By Agent</h2>
          <div className="w-full">
            <div className="grid grid-cols-4 gap-4 border-b border-primary pb-2 text-sm text-secondary">
              <div>Agent</div>
              <div>Tokens</div>
              <div>Sessions</div>
              <div>Last used</div>
            </div>
            {summary.agents.map((agent) => (
              <div key={agent.id} className="grid grid-cols-4 gap-4 border-b border-primary py-2 text-sm">
                <div className="text-primary">{agent.name}</div>
                <div className="font-mono text-primary">{formatTokens(agent.tokens)}</div>
                <div className="font-mono text-primary">{agent.sessions}</div>
                <div className="text-secondary">{relativeDayLabel(agent.lastUsedAt)}</div>
              </div>
            ))}
          </div>
        </div>
      </div>
    </div>
  )
}
