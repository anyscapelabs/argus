import { useAccount } from '../../domains/account/hooks'
import type { UsageLimit } from '../../../bindings/github.com/anyscapelabs/argus/Services/Shared/models'

function formatValue(n: number): string {
  if (n >= 1_000_000_000) return `${(n / 1_000_000_000).toFixed(1).replace(/\.0$/, '')}B`
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1).replace(/\.0$/, '')}M`
  if (n >= 1_000) return `${(n / 1_000).toFixed(1).replace(/\.0$/, '')}K`
  return `${Math.round(n)}`
}

function resetLabel(value?: string): string {
  if (!value) return ''
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return ''
  return new Intl.DateTimeFormat('en-GB', { day: '2-digit', month: 'short' }).format(date)
}

function LimitBar({ label, value }: { label: string; value?: UsageLimit | null }) {
  const used = value?.used ?? 0
  const limit = value?.limit ?? 0
  const percent = limit > 0 ? Math.min(Math.round((used / limit) * 100), 100) : 0
  const reset = resetLabel(value?.reset_at)

  return (
    <div className="flex flex-col gap-2 rounded-xl border border-primary p-3">
      <div className="flex items-center justify-between gap-2">
        <span className="text-sm text-secondary">{label}</span>
        <span className="text-sm font-medium text-primary">
          {formatValue(used)} / {formatValue(limit)}
        </span>
      </div>
      <div
        className="h-1.5 w-full overflow-hidden rounded-full bg-secondary"
        role="progressbar"
        aria-label={label}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={percent}
      >
        <div className="h-full rounded-full bg-accent" style={{ width: `${percent}%` }} />
      </div>
      {reset && <span className="text-xs text-secondary">Resets {reset}</span>}
    </div>
  )
}

export default function UsageLimits() {
  const { data: account } = useAccount()
  const usage = account?.usage

  if (!usage) return null

  return (
    <div className="flex flex-col gap-2">
      <div className="flex flex-col gap-1">
        <h2 className="text-base font-medium text-primary">Usage limits</h2>
        <p className="text-sm text-secondary">Your current plan limits and reset dates.</p>
      </div>
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
        <LimitBar label="Weekly tokens" value={usage.weekly} />
        <LimitBar label="Monthly tokens" value={usage.monthly} />
        <LimitBar label="Voice minutes" value={usage.voice} />
        <LimitBar label="Web searches" value={usage.search} />
      </div>
    </div>
  )
}
