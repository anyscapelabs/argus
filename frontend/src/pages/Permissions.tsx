import { useEffect, useRef, useState, type ComponentType } from 'react'
import { IoChevronDown, IoExtensionPuzzleOutline, IoFolderOpenOutline, IoGlobeOutline, IoTerminalOutline } from 'react-icons/io5'
import { useAgents } from '../domains/models/hooks'
import {
  usePermissionPolicies,
  useResetPermissionPolicies,
  useSetPermissionPolicy,
} from '../domains/permission/hooks'
import type { PermissionPolicyMode } from '../domains/permission/api'

const GLOBAL_SCOPE = '*'

const CAPABILITIES: {
  tool: string
  title: string
  description: string
  icon: ComponentType<{ size?: number; className?: string }>
}[] = [
  {
    tool: 'execute_command',
    title: 'Terminal commands',
    description: 'Run commands and local developer tools on this device.',
    icon: IoTerminalOutline,
  },
  {
    tool: 'file_access',
    title: 'File access',
    description: 'Read, create, and edit files outside the current conversation.',
    icon: IoFolderOpenOutline,
  },
  {
    tool: 'browser_action',
    title: 'Browser actions',
    description: 'Submit forms, send messages, purchase, post, or delete on a website.',
    icon: IoGlobeOutline,
  },
  {
    tool: 'skill_install',
    title: 'Install skills',
    description: 'Enable new workflow instructions for an agent.',
    icon: IoExtensionPuzzleOutline,
  },
]

const GLOBAL_OPTIONS: { value: PermissionPolicyMode; label: string }[] = [
  { value: 'ask', label: 'Ask' },
  { value: 'allow', label: 'Allow' },
  { value: 'deny', label: 'Block' },
]

const AGENT_OPTIONS: { value: PermissionPolicyMode; label: string }[] = [
  { value: 'inherit', label: 'Use global' },
  { value: 'allow', label: 'Allow' },
  { value: 'deny', label: 'Block' },
]

export default function Permissions() {
  const [scope, setScope] = useState(GLOBAL_SCOPE)
  const { data: agents = [] } = useAgents()
  const { data: policies = [], isPending, error } = usePermissionPolicies(scope)
  const setPolicy = useSetPermissionPolicy()
  const reset = useResetPermissionPolicies()
  const isGlobal = scope === GLOBAL_SCOPE
  const activeAgent = agents.find((agent) => agent.id === scope)

  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-y-auto bg-primary">
      <div className="mx-auto flex w-full max-w-4xl flex-1 flex-col gap-7 px-5 pb-10 sm:px-8">
        <header className="flex max-w-2xl flex-col gap-1">
          <h1 className="text-2xl font-medium tracking-tight text-primary">Permissions</h1>
          <p className="text-sm leading-6 text-secondary">
            Control when Argus asks for approval.
          </p>
        </header>

        <section className="overflow-hidden rounded-2xl border border-primary">
          <div className="relative flex flex-col gap-4 p-4 sm:flex-row sm:items-center sm:justify-between after:absolute after:left-2 after:right-2 after:bottom-0 after:border-b after:border-primary">
            <div>
              <p className="text-sm font-medium text-primary">Permission scope</p>
              <p className="text-xs text-secondary">
                {isGlobal ? 'Default rules for all agents' : `Custom rules for ${activeAgent?.name ?? 'this agent'}`}
              </p>
            </div>
            <div className="flex w-fit items-center gap-1 rounded-full border border-secondary p-1">
              <button
                type="button"
                onClick={() => setScope(GLOBAL_SCOPE)}
                className={`shrink-0 cursor-pointer rounded-full px-3 py-1 text-xs font-medium transition-colors ${
                  isGlobal ? 'bg-accent text-accent-foreground' : 'text-secondary hover:text-primary'
                }`}
              >
                Global
              </button>
              <AgentScopeDropdown
                agents={agents}
                value={isGlobal ? '' : scope}
                onSelect={setScope}
              />
            </div>
          </div>

          {isPending ? (
            <div className="space-y-3 p-4" aria-label="Loading permissions">
              {[0, 1, 2, 3].map((item) => <div key={item} className="h-16 animate-pulse rounded-xl bg-secondary/50" />)}
            </div>
          ) : error ? (
            <p className="p-4 text-sm text-danger">Could not load permission settings.</p>
          ) : (
            <div>
              {CAPABILITIES.map((capability, index) => {
                const policy = policies.find((item) => item.tool === capability.tool)
                const value = (isGlobal ? policy?.global_mode : policy?.agent_mode) as PermissionPolicyMode | undefined
                const Icon = capability.icon
                return (
                  <div key={capability.tool} className={`flex flex-col gap-4 p-4 sm:flex-row sm:items-center sm:justify-between ${index > 0 ? 'relative before:absolute before:left-2 before:right-2 before:top-0 before:border-t before:border-primary' : ''}`}>
                    <div className="flex min-w-0 items-center gap-3">
                      <Icon size={20} className="shrink-0 text-secondary" />
                      <div className="min-w-0">
                        <div className="flex flex-wrap items-center gap-2">
                          <p className="text-sm font-medium text-primary">{capability.title}</p>
                          {!isGlobal && policy?.agent_mode === 'inherit' && (
                            <span className="text-xs text-secondary">Currently {modeLabel(policy.effective_mode)}</span>
                          )}
                        </div>
                        <p className="max-w-xl text-xs text-secondary">{capability.description}</p>
                      </div>
                    </div>
                    <PolicyControl
                      value={value ?? (isGlobal ? 'ask' : 'inherit')}
                      options={isGlobal ? GLOBAL_OPTIONS : AGENT_OPTIONS}
                      disabled={setPolicy.isPending}
                      label={`${capability.title} permission`}
                      onChange={(mode) => setPolicy.mutate({ agentID: scope, tool: capability.tool, mode })}
                    />
                  </div>
                )
              })}
            </div>
          )}
        </section>

        {(setPolicy.error || reset.error) && (
          <p role="alert" className="text-sm text-danger">That change could not be saved. Try again.</p>
        )}

        <section>
          <h2 className="mb-3 text-sm font-medium text-primary">Restore</h2>
          <div className="flex flex-col gap-4 rounded-2xl border border-primary p-4 sm:flex-row sm:items-center sm:justify-between">
            <div>
              <p className="text-sm font-medium text-primary">Restore defaults</p>
              <p className="text-xs text-secondary">
                {isGlobal ? 'Reset global permissions to Ask.' : "Remove this agent's permission overrides."}
              </p>
            </div>
            <button
              type="button"
              disabled={reset.isPending}
              onClick={() => reset.mutate(scope)}
              className="w-fit cursor-pointer rounded-full border border-secondary px-3 py-1.5 text-xs font-medium text-primary transition-colors hover:bg-hover-primary disabled:cursor-not-allowed disabled:opacity-50"
            >
              Restore
            </button>
          </div>
        </section>
      </div>
    </div>
  )
}

function PolicyControl({
  value,
  options,
  disabled,
  label,
  onChange,
}: {
  value: PermissionPolicyMode
  options: { value: PermissionPolicyMode; label: string }[]
  disabled: boolean
  label: string
  onChange: (mode: PermissionPolicyMode) => void
}) {
  return (
    <div role="radiogroup" aria-label={label} className="flex shrink-0 items-center gap-1 rounded-full border border-secondary p-1">
      {options.map((option) => (
        <button
          key={option.value}
          type="button"
          role="radio"
          aria-checked={value === option.value}
          disabled={disabled}
          onClick={() => onChange(option.value)}
          className={`cursor-pointer whitespace-nowrap rounded-full px-2.5 py-1 text-xs font-medium transition-colors disabled:cursor-not-allowed disabled:opacity-50 ${
            value === option.value ? 'bg-accent text-accent-foreground' : 'text-secondary hover:text-primary'
          }`}
        >
          {option.label}
        </button>
      ))}
    </div>
  )
}

function AgentScopeDropdown({
  agents,
  value,
  onSelect,
}: {
  agents: { id: string; name: string }[]
  value: string
  onSelect: (id: string) => void
}) {
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLDivElement | null>(null)
  const selected = agents.find((agent) => agent.id === value)

  useEffect(() => {
    if (!open) return
    const onPointerDown = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false)
    }
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setOpen(false)
    }
    document.addEventListener('mousedown', onPointerDown)
    document.addEventListener('keydown', onKeyDown)
    return () => {
      document.removeEventListener('mousedown', onPointerDown)
      document.removeEventListener('keydown', onKeyDown)
    }
  }, [open])

  return (
    <div ref={ref} className="relative">
      <button
        type="button"
        aria-haspopup="listbox"
        aria-expanded={open}
        onClick={() => setOpen((o) => !o)}
        className={`flex cursor-pointer items-center justify-between gap-1.5 rounded-full px-3 py-1 text-xs font-medium transition-colors ${
          value || open ? 'max-w-[10rem]' : ''
        } ${value ? 'bg-accent text-accent-foreground' : 'text-secondary hover:text-primary'}`}
      >
        <span className="truncate">{selected?.name ?? 'Agent'}</span>
        <IoChevronDown size={12} className={`shrink-0 transition-transform ${open ? 'rotate-180' : ''}`} />
      </button>
      {open && (
        <div
          role="listbox"
          aria-label="Choose an agent"
          className="absolute right-0 top-full z-20 mt-2 min-w-[10rem] overflow-hidden rounded-xl border border-primary bg-primary p-1 shadow-lg"
        >
          {agents.length === 0 ? (
            <p className="px-2.5 py-1.5 text-xs text-secondary">No agents</p>
          ) : (
            agents.map((agent) => (
              <button
                key={agent.id}
                type="button"
                role="option"
                aria-selected={value === agent.id}
                onClick={() => {
                  onSelect(agent.id)
                  setOpen(false)
                }}
                className={`flex w-full cursor-pointer items-center rounded-lg px-2.5 py-1.5 text-left text-xs font-medium transition-colors ${
                  value === agent.id ? 'bg-secondary text-primary' : 'text-secondary hover:bg-hover-primary hover:text-primary'
                }`}
              >
                <span className="truncate">{agent.name}</span>
              </button>
            ))
          )}
        </div>
      )}
    </div>
  )
}

function modeLabel(mode: string | undefined) {
  if (mode === 'allow') return 'allowed'
  if (mode === 'deny') return 'blocked'
  return 'ask every time'
}
