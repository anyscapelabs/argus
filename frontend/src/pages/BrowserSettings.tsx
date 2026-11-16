import { FiArrowUpRight, FiSearch } from 'react-icons/fi'
import { LuMousePointerClick } from 'react-icons/lu'
import AgentAvatar from '../components/AgentAvatar'
import { useAgents } from '../domains/models/hooks'
import { usePermissionPolicies, useSetPermissionPolicy } from '../domains/permission/hooks'
import type { PermissionPolicyMode } from '../domains/permission/api'
import type { AgentSummary } from '../../bindings/github.com/anyscapelabs/argus/Services/Shared/models.js'

const BROWSER_TOOL = 'browser_use'

export default function BrowserSettings() {
  const { data: agents = [] } = useAgents()
  const { data: globalPolicies = [], isPending, error } = usePermissionPolicies('*')
  const setPolicy = useSetPermissionPolicy()
  const browserPolicy = globalPolicies.find((policy) => policy.tool === BROWSER_TOOL)
  const enabled = browserPolicy?.global_mode !== 'deny'

  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-y-auto bg-primary">
      <div className="mx-auto flex w-full max-w-4xl flex-1 flex-col gap-7 px-5 pb-10 sm:px-8">
        <header className="flex max-w-2xl flex-col gap-1">
          <h1 className="text-2xl font-medium tracking-tight text-primary">Browser</h1>
          <p className="text-sm leading-6 text-secondary">
            Control whether agents can use the browser. Web search stays available when it's off.
          </p>
        </header>

        <section className="rounded-2xl border border-primary p-2">
          <div className="flex flex-col gap-6 sm:flex-row sm:items-center sm:justify-between">
            <div className="flex items-center gap-3">
              <LuMousePointerClick size={24} className={enabled ? 'text-info' : 'text-secondary'} />
              <div>
                <h2 className="text-sm font-medium text-primary">Browser use</h2>
                <p className="text-xs text-secondary">
                  {enabled ? 'Available by default to all agents' : 'Blocked by default for all agents'}
                </p>
              </div>
            </div>
            {isPending ? (
              <div className="h-5 w-9 animate-pulse rounded-full bg-secondary" />
            ) : (
              <Toggle
                checked={enabled}
                disabled={setPolicy.isPending}
                label="Browser use for all agents"
                onChange={(checked) => setPolicy.mutate({ agentID: '*', tool: BROWSER_TOOL, mode: checked ? 'allow' : 'deny' })}
              />
            )}
          </div>
          {error && <p className="relative mt-4 text-sm text-danger">Could not load browser settings.</p>}
        </section>

        <section>
          <div className="mb-3 flex items-end justify-between gap-4">
            <h2 className="text-sm font-medium text-primary">Agent access</h2>
            <span className="text-xs text-secondary">{agents.length} {agents.length === 1 ? 'agent' : 'agents'}</span>
          </div>
          <div className="overflow-hidden rounded-2xl border border-primary">
            {agents.length === 0 ? (
              <p className="p-4 text-sm text-secondary">Your agents will appear here once they are created.</p>
            ) : (
              agents.map((agent, index) => (
                <AgentBrowserRow key={agent.id} agent={agent} bordered={index > 0} globalEnabled={enabled} />
              ))
            )}
          </div>
        </section>

        {setPolicy.error && <p role="alert" className="text-sm text-danger">That change could not be saved. Try again.</p>}

        <div className="flex items-start gap-3 rounded-xl border border-primary px-4 py-3 text-sm text-secondary">
          <FiSearch size={15} className="mt-0.5 shrink-0" />
          <p className="leading-5">
            Turning this off stops interactive browser control, not read-only web search.
            <span className="ml-1 inline-flex items-center gap-1 text-primary">Sensitive actions remain separately controlled <FiArrowUpRight size={12} /></span>
          </p>
        </div>
      </div>
    </div>
  )
}

function AgentBrowserRow({ agent, bordered, globalEnabled }: { agent: AgentSummary; bordered: boolean; globalEnabled: boolean }) {
  const { data: policies = [], isPending } = usePermissionPolicies(agent.id)
  const setPolicy = useSetPermissionPolicy()
  const policy = policies.find((item) => item.tool === BROWSER_TOOL)
  const mode = (policy?.agent_mode ?? 'inherit') as PermissionPolicyMode
  const effective = mode === 'inherit' ? globalEnabled : mode === 'allow'

  return (
    <div className={`flex flex-col gap-3 p-2 sm:flex-row sm:items-center sm:justify-between ${bordered ? 'relative before:absolute before:left-2 before:right-2 before:top-0 before:border-t before:border-primary' : ''}`}>
      <div className="flex min-w-0 items-center gap-3">
        <AgentAvatar avatar={agent.avatar} name={agent.name} size={32} />
        <div className="min-w-0">
          <p className="truncate text-sm font-medium text-primary">{agent.name}</p>
          <p className="text-xs text-secondary">Browser {effective ? 'available' : 'blocked'}</p>
        </div>
      </div>
      {isPending ? (
        <div className="h-7 w-40 animate-pulse rounded-full bg-secondary" />
      ) : (
        <div role="radiogroup" aria-label={`Browser access for ${agent.name}`} className="flex w-fit items-center gap-1 rounded-full border border-secondary p-1">
          {([
            ['inherit', 'Use global'],
            ['allow', 'On'],
            ['deny', 'Off'],
          ] as const).map(([value, label]) => (
            <button
              key={value}
              type="button"
              role="radio"
              aria-checked={mode === value}
              disabled={setPolicy.isPending}
              onClick={() => setPolicy.mutate({ agentID: agent.id, tool: BROWSER_TOOL, mode: value })}
              className={`cursor-pointer rounded-full px-2.5 py-1 text-xs font-medium transition-colors disabled:cursor-not-allowed disabled:opacity-50 ${
                mode === value ? 'bg-accent text-accent-foreground' : 'text-secondary hover:text-primary'
              }`}
            >
              {label}
            </button>
          ))}
        </div>
      )}
    </div>
  )
}

function Toggle({ checked, disabled, label, onChange }: { checked: boolean; disabled: boolean; label: string; onChange: (next: boolean) => void }) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      aria-label={label}
      disabled={disabled}
      onClick={() => onChange(!checked)}
      className={`relative inline-flex h-5 w-9 shrink-0 cursor-pointer items-center rounded-full border transition-colors disabled:cursor-not-allowed disabled:opacity-50 ${
        checked ? 'border-accent bg-accent' : 'border-secondary bg-secondary'
      }`}
    >
      <span className={`h-3.5 w-3.5 rounded-full bg-accent-foreground transition-transform ${checked ? 'translate-x-[18px]' : 'translate-x-0.5'}`} />
    </button>
  )
}

