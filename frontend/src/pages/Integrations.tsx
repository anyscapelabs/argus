import { useMemo, useState } from 'react'
import { IoAdd, IoChevronDown, IoSearch } from 'react-icons/io5'
import MCPConnectCard from '../components/mcp/MCPConnectCard'
import { useIntegrationConnect, useIntegrations } from '../domains/mcp/hooks'
import type { MCPConnectRequestView } from '../domains/mcp/api'

export default function Integrations() {
  const [search, setSearch] = useState('')
  const [tab, setTab] = useState<'discover' | 'yours'>('discover')
  const { data: integrations = [], isLoading } = useIntegrations()
  const { states, connect, clear, disconnect } = useIntegrationConnect()

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase()
    if (!q) return integrations
    return integrations.filter((it) =>
      (it.name || '').toLowerCase().includes(q) || (it.description || '').toLowerCase().includes(q),
    )
  }, [integrations, search])

  return (
    <div className="flex min-h-0 flex-1 flex-col bg-primary">
      <div className="mx-auto flex min-h-0 w-full max-w-3xl flex-1 flex-col px-6 pt-5">
        <h1 className="text-lg font-medium text-primary">Integrations</h1>
        <div className="mt-4 flex items-center justify-between gap-3">
          <div className="inline-flex shrink-0 items-center rounded-lg border border-primary bg-secondary p-0.5">
            <button
              type="button"
              onClick={() => setTab('discover')}
              className={`cursor-pointer rounded-md px-2 py-1 text-sm transition-colors ${
                tab === 'discover'
                  ? 'bg-hover-secondary text-primary'
                  : 'text-secondary hover:text-primary'
              }`}
            >
              Discover
            </button>
            <button
              type="button"
              onClick={() => setTab('yours')}
              className={`cursor-pointer rounded-md px-2 py-1 text-sm transition-colors ${
                tab === 'yours'
                  ? 'bg-hover-secondary text-primary'
                  : 'text-secondary hover:text-primary'
              }`}
            >
              Yours
            </button>
          </div>
          <div className="flex min-w-0 items-center gap-2">
            <div className="flex min-w-0 w-52 items-center gap-2 rounded-lg border border-primary px-2 py-1">
              <IoSearch size={16} className="shrink-0 text-secondary" />
              <input
                type="text"
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                placeholder="Search"
                className="min-w-0 w-full bg-transparent text-sm text-primary placeholder:text-secondary focus:outline-none"
              />
            </div>
            <button
              type="button"
              className="flex shrink-0 cursor-pointer items-center gap-2 rounded-full bg-accent px-2 py-1 text-sm font-medium text-black transition-colors hover:bg-hover-accent"
            >
              <span className="flex items-center gap-1">
                <IoAdd size={16} />
                Add
              </span>
              <IoChevronDown size={14} />
            </button>
          </div>
        </div>

        <div className="mt-4 min-h-0 flex-1 overflow-y-auto pb-6">
          <div className="mb-2 flex items-center gap-2">
            <h2 className="text-sm font-medium text-primary">Top Connectors</h2>
            <span className="rounded-full border border-primary bg-secondary px-2 py-1 text-sm text-secondary">
              {filtered.length}
            </span>
          </div>
          {isLoading ? (
            <div className="text-sm text-secondary">Loading integrations…</div>
          ) : filtered.length === 0 ? (
            <div className="text-sm text-secondary">No integrations found.</div>
          ) : (
            <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
              {filtered.map((it) => {
                const local = states[it.id]
                const state = local?.state ?? (it.connected ? 'connected' : 'requested')
                const request: MCPConnectRequestView = {
                  request_id: local?.requestId || it.id,
                  agent_id: 'default',
                  server_id: it.id,
                  server_name: it.name,
                  description: it.description,
                  icon: it.icon,
                  state: state as any,
                  created_at: 0,
                }
                return (
                  <MCPConnectCard
                    key={it.id}
                    request={request}
                    state={state as any}
                    error={local?.error}
                    onConnect={() => void connect(it.id)}
                    onDeny={state === 'failed' ? () => clear(it.id) : undefined}
                    onCancel={() => clear(it.id)}
                    onRemove={state === 'connected' ? () => void disconnect(it.id) : undefined}
                    denyLabel="Dismiss"
                    className="max-w-none"
                  />
                )
              })}
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
