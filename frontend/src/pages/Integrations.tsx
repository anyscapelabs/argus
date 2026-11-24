import { useMemo, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { IoAdd, IoChevronDown, IoClose, IoSearch } from 'react-icons/io5'
import MCPConnectCard from '../components/mcp/MCPConnectCard'
import { useIntegrationConnect, useIntegrations } from '../domains/mcp/hooks'
import { addCustomServer, removeCustomServer, type IntegrationView } from '../domains/mcp/api'
import type { MCPConnectRequestView } from '../domains/mcp/api'

const CATEGORY_ORDER = ['Google Workspace', 'Productivity', 'Developer Tools', 'Custom']

type AddForm = {
  name: string
  transport: 'http' | 'stdio'
  url: string
  command: string
  args: string
  env: string
}

const emptyAddForm: AddForm = {
  name: '',
  transport: 'http',
  url: '',
  command: '',
  args: '',
  env: '',
}

function parseEnvLines(raw: string): Record<string, string> {
  const env: Record<string, string> = {}
  for (const line of raw.split('\n')) {
    const trimmed = line.trim()
    if (!trimmed) continue
    const eq = trimmed.indexOf('=')
    if (eq <= 0) continue
    const key = trimmed.slice(0, eq).trim()
    if (key) env[key] = trimmed.slice(eq + 1).trim()
  }
  return env
}

export default function Integrations({ onManage }: { onManage?: (serverId: string) => void }) {
  const qc = useQueryClient()
  const [search, setSearch] = useState('')
  const [tab, setTab] = useState<'discover' | 'yours'>('discover')
  const [addOpen, setAddOpen] = useState(false)
  const [form, setForm] = useState<AddForm>(emptyAddForm)
  const [addError, setAddError] = useState('')
  const [addBusy, setAddBusy] = useState(false)
  const { data: integrations = [], isLoading } = useIntegrations()
  const { states, connect, clear, disconnect } = useIntegrationConnect()

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase()
    const pool = tab === 'yours' ? integrations.filter((it) => it.connected || it.custom) : integrations
    if (!q) return pool
    return pool.filter((it) =>
      (it.name || '').toLowerCase().includes(q) || (it.description || '').toLowerCase().includes(q),
    )
  }, [integrations, search, tab])

  const groups = useMemo(() => {
    const byCategory = new Map<string, IntegrationView[]>()
    for (const it of filtered) {
      const category = it.custom ? 'Custom' : it.category || 'Other'
      const list = byCategory.get(category) ?? []
      list.push(it)
      byCategory.set(category, list)
    }
    const ordered = CATEGORY_ORDER.filter((c) => byCategory.has(c))
    for (const category of [...byCategory.keys()].sort()) {
      if (!ordered.includes(category)) ordered.push(category)
    }
    return ordered.map((category) => ({ category, items: byCategory.get(category)! }))
  }, [filtered])

  const totalCount = useMemo(
    () => groups.reduce((sum, group) => sum + group.items.length, 0),
    [groups],
  )

  const submitAdd = async () => {
    if (addBusy) return
    setAddError('')
    if (!form.name.trim()) {
      setAddError('Give the server a name.')
      return
    }
    if (form.transport === 'http' && !form.url.trim()) {
      setAddError('Enter the server URL.')
      return
    }
    if (form.transport === 'stdio' && !form.command.trim()) {
      setAddError('Enter the command to run.')
      return
    }
    setAddBusy(true)
    try {
      const args = form.args
        .split('\n')
        .map((line) => line.trim())
        .filter(Boolean)
      await addCustomServer({
        name: form.name.trim(),
        transport: form.transport,
        url: form.url.trim(),
        command: form.command.trim(),
        args,
        env: parseEnvLines(form.env),
      })
      setForm(emptyAddForm)
      setAddOpen(false)
      await qc.invalidateQueries({ queryKey: ['mcp', 'integrations'] })
      await qc.invalidateQueries({ queryKey: ['mcp', 'catalog'] })
    } catch (error) {
      setAddError(error instanceof Error ? error.message : 'Could not add the server.')
    } finally {
      setAddBusy(false)
    }
  }

  const removeCustom = async (id: string) => {
    try {
      await removeCustomServer(id)
      await qc.invalidateQueries({ queryKey: ['mcp', 'integrations'] })
      await qc.invalidateQueries({ queryKey: ['mcp', 'catalog'] })
    } catch {
      // the card keeps its last state; the server simply stays listed
    }
  }

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
              onClick={() => {
                setForm(emptyAddForm)
                setAddError('')
                setAddOpen(true)
              }}
              className="flex shrink-0 cursor-pointer items-center gap-2 rounded-full bg-accent px-2 py-1 text-sm font-medium text-accent-foreground transition-colors hover:bg-hover-accent"
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
          {isLoading ? (
            <div className="text-sm text-secondary">Loading integrations…</div>
          ) : totalCount === 0 ? (
            <div className="text-sm text-secondary">
              {tab === 'yours' ? 'Nothing connected yet — find a connector under Discover, or add your own MCP server.' : 'No integrations found.'}
            </div>
          ) : (
            groups.map((group) => (
              <div key={group.category} className="mb-4">
                <div className="mb-2 flex items-center gap-2">
                  <h2 className="text-sm font-medium text-primary">{group.category}</h2>
                  <span className="rounded-full border border-primary bg-secondary px-2 py-1 text-sm text-secondary">
                    {group.items.length}
                  </span>
                </div>
                <div className="grid grid-cols-1 gap-1 sm:grid-cols-2">
                  {group.items.map((it) => {
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
                      <div key={it.id} className="relative">
                        <MCPConnectCard
                          request={request}
                          state={state as any}
                          error={local?.error}
                          onConnect={() => void connect(it.id)}
                          onDeny={state === 'failed' ? () => clear(it.id) : undefined}
                          onCancel={() => clear(it.id)}
                          onManage={state === 'connected' ? () => (onManage ? onManage(it.id) : void disconnect(it.id)) : undefined}
                          denyLabel="Dismiss"
                          className="max-w-none"
                        />
                        {it.custom && (
                          <button
                            type="button"
                            onClick={() => void removeCustom(it.id)}
                            aria-label={`Remove ${it.name}`}
                            className="absolute right-2 top-2 flex h-6 w-6 cursor-pointer items-center justify-center rounded-full bg-secondary text-secondary transition-colors hover:bg-hover-secondary hover:text-danger"
                          >
                            <IoClose size={14} />
                          </button>
                        )}
                      </div>
                    )
                  })}
                </div>
              </div>
            ))
          )}
        </div>
      </div>

      {addOpen && (
        <div
          className="fixed inset-0 z-40 flex items-center justify-center bg-black/60 p-4"
          onMouseDown={() => !addBusy && setAddOpen(false)}
        >
          <div
            role="dialog"
            aria-modal="true"
            aria-label="Add MCP server"
            className="flex w-full max-w-md flex-col gap-4 rounded-2xl border border-secondary bg-secondary p-5"
            onMouseDown={(e) => e.stopPropagation()}
          >
            <div className="flex items-center justify-between">
              <h3 className="text-sm font-medium text-primary">Add MCP server</h3>
              <button
                type="button"
                onClick={() => setAddOpen(false)}
                disabled={addBusy}
                aria-label="Close"
                className="flex h-7 w-7 cursor-pointer items-center justify-center rounded-full text-secondary transition-colors hover:bg-hover-secondary hover:text-primary disabled:opacity-50"
              >
                <IoClose size={16} />
              </button>
            </div>
            <p className="text-xs text-secondary">
              Point Argus at any MCP server: a remote URL over streamable HTTP, or a local command that speaks MCP over stdin. Its tools appear in chat once the connection is up.
            </p>
            <div className="flex flex-col gap-3">
              <label className="flex flex-col gap-1 text-xs text-secondary">
                Name
                <input
                  autoFocus
                  value={form.name}
                  onChange={(e) => setForm({ ...form, name: e.target.value })}
                  placeholder="e.g. My tools"
                  className="rounded-lg border border-secondary bg-primary px-3 py-2 text-sm text-primary outline-none placeholder:text-secondary focus:border-tertiary"
                />
              </label>
              <div className="flex flex-col gap-1 text-xs text-secondary">
                Type
                <div className="inline-flex w-fit items-center rounded-lg border border-primary bg-primary p-0.5">
                  <button
                    type="button"
                    onClick={() => setForm({ ...form, transport: 'http' })}
                    className={`cursor-pointer rounded-md px-2 py-1 text-sm transition-colors ${
                      form.transport === 'http'
                        ? 'bg-hover-secondary text-primary'
                        : 'text-secondary hover:text-primary'
                    }`}
                  >
                    Remote server
                  </button>
                  <button
                    type="button"
                    onClick={() => setForm({ ...form, transport: 'stdio' })}
                    className={`cursor-pointer rounded-md px-2 py-1 text-sm transition-colors ${
                      form.transport === 'stdio'
                        ? 'bg-hover-secondary text-primary'
                        : 'text-secondary hover:text-primary'
                    }`}
                  >
                    Local command
                  </button>
                </div>
              </div>
              {form.transport === 'http' ? (
                <label className="flex flex-col gap-1 text-xs text-secondary">
                  URL
                  <input
                    value={form.url}
                    onChange={(e) => setForm({ ...form, url: e.target.value })}
                    placeholder="https://example.com/mcp"
                    className="rounded-lg border border-secondary bg-primary px-3 py-2 text-sm text-primary outline-none placeholder:text-secondary focus:border-tertiary"
                  />
                </label>
              ) : (
                <>
                  <label className="flex flex-col gap-1 text-xs text-secondary">
                    Command
                    <input
                      value={form.command}
                      onChange={(e) => setForm({ ...form, command: e.target.value })}
                      placeholder="e.g. npx"
                      className="rounded-lg border border-secondary bg-primary px-3 py-2 text-sm text-primary outline-none placeholder:text-secondary focus:border-tertiary"
                    />
                  </label>
                  <label className="flex flex-col gap-1 text-xs text-secondary">
                    Arguments <span className="text-secondary/60">(one per line)</span>
                    <textarea
                      value={form.args}
                      onChange={(e) => setForm({ ...form, args: e.target.value })}
                      rows={2}
                      placeholder={'-y\n@modelcontextprotocol/server-filesystem /tmp'}
                      className="resize-none rounded-lg border border-secondary bg-primary px-3 py-2 text-sm text-primary outline-none placeholder:text-secondary focus:border-tertiary"
                    />
                  </label>
                  <label className="flex flex-col gap-1 text-xs text-secondary">
                    Environment <span className="text-secondary/60">(KEY=VALUE, one per line)</span>
                    <textarea
                      value={form.env}
                      onChange={(e) => setForm({ ...form, env: e.target.value })}
                      rows={2}
                      placeholder={'API_KEY=…'}
                      className="resize-none rounded-lg border border-secondary bg-primary px-3 py-2 text-sm text-primary outline-none placeholder:text-secondary focus:border-tertiary"
                    />
                  </label>
                </>
              )}
            </div>
            {addError && (
              <p role="alert" className="text-xs text-danger">
                {addError}
              </p>
            )}
            <div className="flex items-center justify-end gap-2">
              <button
                type="button"
                onClick={() => setAddOpen(false)}
                disabled={addBusy}
                className="cursor-pointer rounded-full border border-tertiary px-3 py-1.5 text-sm font-medium text-primary transition-colors hover:bg-hover-secondary disabled:opacity-50"
              >
                Cancel
              </button>
              <button
                type="button"
                onClick={() => void submitAdd()}
                disabled={addBusy}
                className="cursor-pointer rounded-full bg-accent px-3 py-1.5 text-sm font-medium text-accent-foreground transition-colors hover:bg-hover-accent disabled:opacity-50"
              >
                {addBusy ? 'Adding…' : 'Add server'}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
