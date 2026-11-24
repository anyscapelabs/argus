import { IoCheckmark } from 'react-icons/io5'
import { useAccount } from '../domains/account/hooks'
import { openDashboard } from '../domains/account/api'

const PLAN_FEATURES: Record<string, string[]> = {
  free: [
    'Core chat and agent tools',
    '100 requests per day',
    'Integrations and browser use',
    'Add prepaid credits anytime',
  ],
  starter: [
    'Everything in Free',
    '500 requests per day',
    'Expanded model access',
  ],
  max: [
    'Everything in Starter',
    '2,000 requests per day',
    'Full model access',
  ],
  'max-plus': [
    'Everything in Max',
    '4,000 requests per day',
    'Priority support',
  ],
}

function featuresForPlan(plan: string): string[] {
  return PLAN_FEATURES[plan.toLowerCase()] ?? [
    'Core chat and agent tools',
    '100 requests per day',
    'Integrations and browser use',
  ]
}

export default function Billing() {
  const { data: account } = useAccount()
  const plan = account?.plan?.id ?? 'Free'
  const features = featuresForPlan(plan)

  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-y-auto bg-primary">
      <div className="mx-auto flex w-full max-w-4xl flex-1 flex-col gap-7 px-5 pb-10 sm:px-8">
        <header className="flex max-w-2xl flex-col gap-1">
          <h1 className="text-2xl font-medium tracking-tight text-primary">Billing</h1>
          <p className="text-sm leading-6 text-secondary">
            Manage your plan and billing.
          </p>
        </header>

        <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
          <div>
            <p className="text-sm font-medium text-primary">Current plan</p>
            <div className="mt-1 flex items-center gap-2">
              <span className="inline-flex items-center rounded-full border border-secondary bg-secondary px-2.5 py-0.5 text-xs font-medium text-primary">
                {plan}
              </span>
            </div>
          </div>
          <button
            type="button"
            onClick={() => void openDashboard('/console/settings/billing')}
            className="w-fit cursor-pointer rounded-full bg-accent px-4 py-1.5 text-sm font-medium text-accent-foreground transition-colors hover:opacity-90"
          >
            Upgrade
          </button>
        </div>

        <section>
          <h2 className="mb-3 text-sm font-medium text-primary">Plan features</h2>
          <ul className="flex flex-col gap-2">
            {features.map((feature) => (
              <li key={feature} className="flex items-center gap-2 text-sm text-secondary">
                <IoCheckmark size={16} className="shrink-0 text-primary" />
                <span>{feature}</span>
              </li>
            ))}
          </ul>
        </section>
      </div>
    </div>
  )
}
