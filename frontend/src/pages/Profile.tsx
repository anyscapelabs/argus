import { useMemo } from 'react'
import { useAccount, usePairing } from '../domains/account/hooks'
import { useIntegrationCatalog } from '../domains/mcp/hooks'
import UsageLimits from '../components/usage/UsageLimits'
import { aggregateUsage } from '../domains/usage/aggregate'
import { useUsageOverview } from '../domains/usage/hooks'

function formatTokens(n: number): string {
  if (n >= 1_000_000_000) return `${(n / 1_000_000_000).toFixed(1).replace(/\.0$/, '')}B`
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1).replace(/\.0$/, '')}M`
  if (n >= 1_000) return `${(n / 1_000).toFixed(1).replace(/\.0$/, '')}K`
  return `${Math.round(n)}`
}

export default function Profile({ onManageBilling }: { onManageBilling?: () => void }) {
  const { data: account } = useAccount()
  const { leave } = usePairing(true)
  const { data: usageRows } = useUsageOverview()
  const { data: catalog = [] } = useIntegrationCatalog()

  const avatar = account?.user?.image ?? '/argus-logo.svg'
  const usage = useMemo(() => aggregateUsage(usageRows, '30d'), [usageRows])
  const connectedAccounts = useMemo(
    () => catalog.reduce((n, item) => n + (item.connections?.length ?? 0), 0),
    [catalog],
  )

  const stats = [
    { label: 'Plan', value: account?.plan?.id ?? '—' },
    { label: 'Email', value: account?.user?.email ?? '—' },
    { label: 'Current streak', value: usage.streakDays > 0 ? `${usage.streakDays} days` : 'No streak' },
    { label: 'Tokens (30 days)', value: formatTokens(usage.tokens) },
    { label: 'Peak day (30 days)', value: formatTokens(usage.peakTokens) },
    { label: 'Connected accounts', value: `${connectedAccounts}` },
  ]

  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-y-auto bg-primary">
      <div className="mx-auto flex w-full max-w-3xl flex-1 flex-col gap-6 px-6 pb-8">
        <div className="flex flex-col gap-1">
          <h1 className="text-xl font-medium text-primary">My Profile</h1>
          <p className="text-sm text-secondary">Manage your profile information.</p>
        </div>
        <div className="flex flex-col items-center gap-2">
          <img src={avatar} alt="" className="h-20 w-20 rounded-xl object-cover" />
          <div className="flex flex-col items-center gap-1">
            <div className="flex items-center justify-center gap-2">
              <p className="text-2xl text-primary">{account?.user?.name ?? 'User'}</p>
              {account?.plan?.id && (
                <span className="inline-flex items-center rounded-full border border-secondary bg-secondary px-2 py-0.5 text-xs leading-none text-secondary">
                  {account.plan.id}
                </span>
              )}
            </div>
            <p className="text-sm text-secondary">{account?.user?.email ?? ''}</p>
          </div>
        </div>

        <div className="rounded-xl border border-primary p-4">
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            {stats.map((stat) => (
              <div key={stat.label} className="flex flex-col gap-1">
                <span className="text-sm text-secondary">{stat.label}</span>
                <span className="text-sm font-medium text-primary">{stat.value}</span>
              </div>
            ))}
          </div>
        </div>

        <UsageLimits />

        <div className="flex flex-col gap-4">
          <div className="flex items-center justify-between gap-4 rounded-xl border border-primary p-4">
            <div className="flex flex-col gap-1">
              <span className="text-sm font-medium text-primary">Billing</span>
              <span className="text-sm text-secondary">Manage your plan and payment details.</span>
            </div>
            <button
              type="button"
              onClick={onManageBilling}
              className="shrink-0 cursor-pointer rounded-full border border-secondary bg-secondary px-3 py-1.5 text-sm font-medium text-primary transition-colors hover:bg-hover-secondary"
            >
              Manage billing
            </button>
          </div>

          <div className="flex items-center justify-between gap-4 rounded-xl border border-primary p-4">
            <div className="flex flex-col gap-1">
              <span className="text-sm font-medium text-primary">Account</span>
              <span className="text-sm text-secondary">Sign out of your account on this device.</span>
            </div>
            <button
              type="button"
              onClick={() => {
                void leave.mutateAsync()
              }}
              className="shrink-0 cursor-pointer rounded-full border border-danger/40 px-3 py-1.5 text-sm font-medium text-danger transition-colors hover:bg-hover-secondary"
            >
              Sign out
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}
