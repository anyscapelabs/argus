import { useProviders } from '../../domains/providers/hooks'
import { useAccount } from '../../domains/account/hooks'

export default function Providers() {
  const { data: providerList } = useProviders()
  const { data: account } = useAccount()
  const router = (providerList ?? []).find((p) => p.id === 'argus-router')

  return (
    <div className="flex flex-1 min-h-0 flex-col bg-primary">
      <div className="mx-auto w-full max-w-2xl p-2">
        <h1 className="text-lg font-medium text-primary">Account</h1>
        <p className="text-sm text-secondary">Your Argus Router plan and the models it allows</p>
        <div className="mt-4 border-t border-primary" />
        <div className="flex items-center justify-between py-3">
          <div className="flex flex-col">
            <span className="text-sm text-primary">{router?.name ?? 'Argus Router'}</span>
            <span className="text-xs text-secondary">
              {account ? `Plan: ${account.plan?.id ?? '—'}` : 'Pair your service-account key to see your plan'}
            </span>
          </div>
          <span className="text-xs text-secondary">
            {account ? `${account.allowlist?.length ?? 0} models allowed` : ''}
          </span>
        </div>
        <div className="border-t border-primary" />
        <p className="py-3 text-xs text-secondary">
          Models come straight from your plan. The picker lists exactly the models you are allowed to use.
        </p>
      </div>
    </div>
  )
}
