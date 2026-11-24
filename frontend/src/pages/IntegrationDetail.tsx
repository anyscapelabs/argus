import { useEffect, useMemo, useRef, useState } from 'react'
import { Events } from '@wailsio/runtime'
import { useQueryClient } from '@tanstack/react-query'
import {
  IoChevronDownOutline,
  IoChevronForwardOutline,
  IoCheckmark,
  IoClose,
  IoRefresh,
  IoWarningOutline,
} from 'react-icons/io5'
import { BsThreeDots } from 'react-icons/bs'
import { useIntegrationCatalog, useIntegrations } from '../domains/mcp/hooks'
import { useAgents } from '../domains/models/hooks'
import { AVATAR_COLORS } from '../components/AgentGem'
import {
  disconnectAccount,
  previewDisconnect,
  removeAccountBinding,
  removeCustomServer,
  renameAccount,
  startAccountConnection,
  upsertAccountBinding,
  type AccessLevel,
  type DisconnectImpact,
  type IntegrationConnection,
} from '../domains/mcp/api'

const ACCESS_LEVELS: { value: AccessLevel; label: string }[] = [
  { value: 'read', label: 'Read' },
  { value: 'draft', label: 'Draft' },
  { value: 'full', label: 'Full' },
]

type Banner = { kind: 'working' | 'connected' | 'failed'; message: string } | null

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : 'Something went wrong.'
}

export default function IntegrationDetail({
  integrationId,
  onBack,
}: {
  integrationId: string
  onBack?: () => void
}) {
  const qc = useQueryClient()
  const { data: integrations = [], isLoading: integrationsLoading } = useIntegrations()
  const { data: catalog = [], isLoading: catalogLoading, isError: catalogError } = useIntegrationCatalog()
  const { data: agents = [] } = useAgents()

  const integration = integrations.find((it) => it.id === integrationId)
  const connector = catalog.find((item) => item.server_id === integrationId)
  const connections = connector?.connections ?? []
  const isCustom = !!(connector?.custom || integration?.custom)
  // Google sign-in hands back an account email, so a label is optional there.
  // Dynamic-OAuth servers (Notion, Linear, …) reveal no identity, and an
  // unnamed second account cannot be told apart from the first — the label is
  // what keeps them distinct.
  const needsLabel = !isCustom && !!integration?.oauth_mode && integration.oauth_mode !== 'google'
  const name = integration?.name || connector?.display_name || integrationId
  const icon = integration?.icon || `/${integrationId}.png`
  const description = integration?.description
  const tools = integration?.tools ?? []

  const listedAgents = useMemo(
    () =>
      agents.length > 0
        ? agents
        : [{ id: 'default', name: 'Argus', avatar: AVATAR_COLORS[0], handle: 'argus', role: '', last_message: '', last_message_kind: '', last_message_at: 0 }],
    [agents],
  )

  const [banner, setBanner] = useState<Banner>(null)
  const [actionError, setActionError] = useState('')
  const [busyKey, setBusyKey] = useState<string | null>(null)
  const [disconnectPreview, setDisconnectPreview] = useState<DisconnectImpact | null>(null)
  const [disconnectAllOpen, setDisconnectAllOpen] = useState(false)
  const [menuOpen, setMenuOpen] = useState(false)
  const [openAccessKey, setOpenAccessKey] = useState<string | null>(null)
  const [openAddAgentConnId, setOpenAddAgentConnId] = useState<string | null>(null)
  const [connectTarget, setConnectTarget] = useState<{ mode: 'new' | 'reauth'; connection?: IntegrationConnection } | null>(null)
  const [labelInput, setLabelInput] = useState('')
  const [emailInput, setEmailInput] = useState('')
  const [connectAgent, setConnectAgent] = useState('default')
  const [connectAccess, setConnectAccess] = useState<AccessLevel>('draft')
  const [connectBusy, setConnectBusy] = useState(false)
  const [connectError, setConnectError] = useState('')
  const [renameTarget, setRenameTarget] = useState<IntegrationConnection | null>(null)
  const [renameInput, setRenameInput] = useState('')
  const [renameBusy, setRenameBusy] = useState(false)
  const [renameError, setRenameError] = useState('')
  const [removeOpen, setRemoveOpen] = useState(false)
  const [removeBusy, setRemoveBusy] = useState(false)
  const menuRef = useRef<HTMLDivElement | null>(null)
  const accessMenuRef = useRef<HTMLDivElement | null>(null)
  const addAgentMenuRef = useRef<HTMLDivElement | null>(null)

  const invalidate = async () => {
    await Promise.all([
      qc.invalidateQueries({ queryKey: ['mcp', 'catalog'] }),
      qc.invalidateQueries({ queryKey: ['mcp', 'integrations'] }),
    ])
  }

  useEffect(() => {
    if (!menuOpen) return
    const handler = (e: MouseEvent) => {
      if (menuRef.current && !menuRef.current.contains(e.target as Node)) setMenuOpen(false)
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [menuOpen])

  useEffect(() => {
    if (!openAccessKey) return
    const handler = (e: MouseEvent) => {
      if (accessMenuRef.current && !accessMenuRef.current.contains(e.target as Node)) {
        setOpenAccessKey(null)
      }
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [openAccessKey])

  useEffect(() => {
    if (!openAddAgentConnId) return
    const handler = (e: MouseEvent) => {
      if (addAgentMenuRef.current && !addAgentMenuRef.current.contains(e.target as Node)) {
        setOpenAddAgentConnId(null)
      }
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [openAddAgentConnId])

  useEffect(() => {
    const unsub = Events.On('mcp:connect:status', (event: any) => {
      const payload = (event?.data ?? event) as any
      if (!payload || payload.server_id !== integrationId) return
      if (payload.state === 'connected') {
        setBanner({ kind: 'connected', message: `${payload.server_name || name} account is connected.` })
      } else if (payload.state === 'failed' || payload.state === 'expired') {
        setBanner({ kind: 'failed', message: payload.error || 'The sign-in did not complete.' })
      } else if (payload.state === 'denied') {
        setBanner({ kind: 'failed', message: 'The sign-in was denied.' })
      }
      if (['connected', 'failed', 'denied', 'expired'].includes(payload.state)) {
        void invalidate()
      }
    })
    return () => unsub && unsub()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [integrationId, name])

  const openConnectModal = (mode: 'new' | 'reauth', connection?: IntegrationConnection) => {
    setConnectTarget({ mode, connection })
    setConnectError('')
    if (mode === 'reauth' && connection) {
      setLabelInput(connection.label)
      setEmailInput(connection.account_email)
      setConnectAgent(connection.bindings[0]?.agent_id || 'default')
      setConnectAccess((connection.bindings[0]?.access_level as AccessLevel) || 'draft')
    } else {
      setLabelInput('')
      setEmailInput('')
      setConnectAgent('default')
      setConnectAccess('draft')
    }
  }

  const submitConnect = async () => {
    if (!connectTarget) return
    if (!connectAgent) {
      setConnectError('Choose an agent.')
      return
    }
    if (needsLabel && connectTarget.mode === 'new' && !labelInput.trim()) {
      setConnectError('Name this account — this server reveals no email, so the label is how accounts stay apart.')
      return
    }
    setConnectBusy(true)
    setConnectError('')
    try {
      const agent = listedAgents.find((a) => a.id === connectAgent)
      const res = await startAccountConnection({
        agentID: connectAgent,
        agentName: agent?.name || 'Argus',
        serverID: integrationId,
        label: labelInput.trim(),
        expectedEmail: emailInput.trim(),
        connectionID: connectTarget.mode === 'reauth' ? connectTarget.connection?.connection_id : undefined,
        accessLevel: connectAccess,
      })
      if (res === 'unknown') {
        setConnectError('This connector is unavailable.')
      } else if (res === 'duplicate') {
        setConnectError('That label is already used by this integration.')
      } else if (res === 'pending') {
        setConnectError('Another sign-in is already in progress for this agent.')
      } else if (res === 'connected') {
        setConnectTarget(null)
        setBanner({ kind: 'connected', message: `${name}: agent bound to the account.` })
        await invalidate()
      } else {
        setConnectTarget(null)
        setBanner({ kind: 'working', message: 'Finish the sign-in in your browser — this page updates when it completes.' })
        await invalidate()
      }
    } catch (error) {
      setConnectError(errorMessage(error))
    } finally {
      setConnectBusy(false)
    }
  }

  const changeAccess = async (conn: IntegrationConnection, agentID: string, accessLevel: string) => {
    setBusyKey(`binding:${conn.connection_id}:${agentID}`)
    setActionError('')
    try {
      await upsertAccountBinding(conn.connection_id, agentID, accessLevel as AccessLevel)
      await invalidate()
    } catch (error) {
      setActionError(errorMessage(error))
    } finally {
      setBusyKey(null)
    }
  }

  const removeBinding = async (conn: IntegrationConnection, agentID: string) => {
    setBusyKey(`binding:${conn.connection_id}:${agentID}`)
    setActionError('')
    try {
      await removeAccountBinding(conn.connection_id, agentID)
      await invalidate()
    } catch (error) {
      setActionError(errorMessage(error))
    } finally {
      setBusyKey(null)
    }
  }

  const addBinding = async (conn: IntegrationConnection, agentID: string) => {
    if (!agentID) return
    setBusyKey(`binding:${conn.connection_id}:${agentID}`)
    setActionError('')
    try {
      await upsertAccountBinding(conn.connection_id, agentID, 'draft')
      await invalidate()
    } catch (error) {
      setActionError(errorMessage(error))
    } finally {
      setBusyKey(null)
    }
  }

  const requestDisconnect = async (conn: IntegrationConnection) => {
    setBusyKey(`preview:${conn.connection_id}`)
    setActionError('')
    try {
      setDisconnectPreview(await previewDisconnect(conn.connection_id))
    } catch (error) {
      setActionError(errorMessage(error))
    } finally {
      setBusyKey(null)
    }
  }

  const confirmDisconnect = async () => {
    if (!disconnectPreview) return
    setBusyKey('disconnect')
    setActionError('')
    try {
      await disconnectAccount(disconnectPreview.connection.connection_id)
      setDisconnectPreview(null)
      await invalidate()
    } catch (error) {
      setActionError(errorMessage(error))
    } finally {
      setBusyKey(null)
    }
  }

  const confirmDisconnectAll = async () => {
    setBusyKey('disconnectAll')
    setActionError('')
    try {
      for (const conn of connections) {
        await disconnectAccount(conn.connection_id)
      }
      setDisconnectAllOpen(false)
      await invalidate()
    } catch (error) {
      setActionError(errorMessage(error))
    } finally {
      setBusyKey(null)
    }
  }

  const submitRename = async () => {
    if (!renameTarget) return
    const label = renameInput.trim()
    setRenameError('')
    if (!label) {
      setRenameError('Enter a name for the account.')
      return
    }
    if (label === renameTarget.label) {
      setRenameTarget(null)
      return
    }
    setRenameBusy(true)
    try {
      await renameAccount(renameTarget.connection_id, label)
      setRenameTarget(null)
      await invalidate()
    } catch (error) {
      setRenameError(errorMessage(error))
    } finally {
      setRenameBusy(false)
    }
  }

  const confirmRemove = async () => {
    setRemoveBusy(true)
    setActionError('')
    try {
      await removeCustomServer(integrationId)
      setRemoveOpen(false)
      await invalidate()
      if (onBack) onBack()
    } catch (error) {
      setActionError(errorMessage(error))
      setRemoveOpen(false)
    } finally {
      setRemoveBusy(false)
    }
  }

  if (!integrationsLoading && !integration && !connector) {
    return (
      <div className="flex min-h-0 flex-1 flex-col items-center justify-center gap-3 bg-primary">
        <p className="text-sm text-secondary">Integration not found.</p>
        {onBack && (
          <button
            type="button"
            onClick={onBack}
            className="cursor-pointer rounded-full bg-accent px-3 py-1.5 text-sm font-medium text-accent-foreground transition-colors hover:bg-hover-accent"
          >
            Back to Integrations
          </button>
        )}
      </div>
    )
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col bg-primary">
      <div className="flex w-full items-center gap-1 px-6 pt-8 text-sm">
        <button
          type="button"
          onClick={onBack}
          disabled={!onBack}
          className="cursor-pointer rounded-full px-2 py-0.5 text-secondary transition-colors hover:bg-hover-secondary hover:text-primary disabled:cursor-default"
        >
          Integrations
        </button>
        <IoChevronForwardOutline size={14} className="text-secondary" />
        <span className="ml-1 font-medium text-primary">{name}</span>
      </div>
      <div className="mx-auto flex min-h-0 w-full max-w-3xl flex-1 flex-col gap-6 overflow-y-auto px-6 pb-8 pt-8">
        <div className="flex items-center gap-3">
          <div className="h-12 w-12 overflow-hidden rounded-xl border border-secondary">
            <img src={icon} alt={name} className="h-full w-full object-cover" />
          </div>
          <div className="flex min-w-0 flex-col">
            <span className="text-xl font-medium text-primary">{name}</span>
            {description ? <span className="text-sm text-secondary">{description}</span> : null}
          </div>
          <div className="ml-auto flex shrink-0 items-center gap-2">
            {isCustom ? (
              <button
                type="button"
                onClick={() => setRemoveOpen(true)}
                disabled={removeBusy}
                className="cursor-pointer rounded-full border border-danger/40 px-3 py-1.5 text-sm font-medium text-danger transition-colors hover:bg-hover-secondary disabled:cursor-not-allowed disabled:opacity-50"
              >
                {removeBusy ? 'Removing…' : 'Remove'}
              </button>
            ) : (
              <button
                type="button"
                onClick={() => setDisconnectAllOpen(true)}
                disabled={connections.length === 0 || busyKey === 'disconnectAll'}
                className="cursor-pointer rounded-full border border-danger/40 px-3 py-1.5 text-sm font-medium text-danger transition-colors hover:bg-hover-secondary disabled:cursor-not-allowed disabled:opacity-50"
              >
                {busyKey === 'disconnectAll' ? 'Disconnecting…' : 'Disconnect'}
              </button>
            )}
            <div ref={menuRef} className="relative">
              <button
                type="button"
                onClick={() => setMenuOpen((v) => !v)}
                aria-label="More actions"
                className="flex h-8 w-8 cursor-pointer items-center justify-center rounded-full bg-secondary text-secondary transition-colors hover:bg-hover-secondary hover:text-primary"
              >
                <BsThreeDots size={16} />
              </button>
              {menuOpen && (
                <div className="absolute right-0 z-20 mt-2 w-44 overflow-hidden rounded-xl border border-secondary bg-secondary p-1 shadow-lg">
                  <button
                    type="button"
                    onClick={() => {
                      setMenuOpen(false)
                      void invalidate()
                    }}
                    className="flex w-full cursor-pointer items-center gap-2 rounded-lg px-2 py-1.5 text-sm text-primary transition-colors hover:bg-hover-secondary"
                  >
                    <IoRefresh size={14} />
                    Refresh
                  </button>
                </div>
              )}
            </div>
          </div>
        </div>

        {banner && (
          <div
            role="status"
            className={`flex items-start gap-2 rounded-xl border px-3 py-2 text-sm ${
              banner.kind === 'connected'
                ? 'border-success/30 bg-success/10 text-success'
                : banner.kind === 'working'
                  ? 'border-secondary bg-secondary text-secondary'
                  : 'border-danger/30 bg-danger/10 text-danger'
            }`}
          >
            <span className="min-w-0 flex-1">{banner.message}</span>
            <button
              type="button"
              onClick={() => setBanner(null)}
              aria-label="Dismiss"
              className="cursor-pointer text-inherit opacity-70 transition-opacity hover:opacity-100"
            >
              <IoClose size={16} />
            </button>
          </div>
        )}
        {actionError && (
          <div role="alert" className="flex items-start gap-2 rounded-xl border border-danger/30 bg-danger/10 px-3 py-2 text-sm text-danger">
            <IoWarningOutline size={16} className="mt-0.5 shrink-0" />
            <span className="min-w-0 flex-1">{actionError}</span>
            <button type="button" onClick={() => setActionError('')} aria-label="Dismiss" className="cursor-pointer opacity-70 hover:opacity-100">
              <IoClose size={16} />
            </button>
          </div>
        )}

        {tools.length > 0 && (
          <div className="rounded-xl border border-secondary p-4">
            <h2 className="text-sm font-medium text-primary">Tools</h2>
            <div className="mt-3 flex flex-col gap-2">
              {tools.map((tool) => (
                <div key={tool} className="flex items-center gap-2 text-sm text-secondary">
                  <IoCheckmark size={16} className="shrink-0 text-primary" />
                  <span>{tool}</span>
                </div>
              ))}
            </div>
          </div>
        )}

        {isCustom ? (
          <div className="rounded-xl border border-secondary p-4">
            <h2 className="text-sm font-medium text-primary">Connection</h2>
            <p className="mt-2 flex items-center gap-2 text-sm text-secondary">
              {isCustom && integration?.connected ? (
                <>
                  <IoCheckmark size={16} className="shrink-0 text-primary" />
                  <span>Connected — its tools are available in chat.</span>
                </>
              ) : (
                <span>Not connected right now. It reconnects automatically when the server is reachable.</span>
              )}
            </p>
          </div>
        ) : (
        <div>
          <div className="flex items-center justify-between gap-2">
            <div className="flex items-center gap-2">
              <h2 className="text-lg font-medium text-primary">Connected Accounts</h2>
              <span className="rounded-full border border-secondary bg-secondary px-2 py-0.5 text-xs text-secondary">
                {connections.length}
              </span>
            </div>
            <button
              type="button"
              onClick={() => openConnectModal('new')}
              className="cursor-pointer rounded-full bg-accent px-3 py-1.5 text-sm font-medium text-accent-foreground transition-colors hover:bg-hover-accent"
            >
              Connect Account
            </button>
          </div>
          {catalogError ? (
            <div className="mt-3 rounded-xl border border-danger/30 bg-danger/10 p-4 text-sm text-danger">
              Could not load accounts.{' '}
              <button type="button" onClick={() => void invalidate()} className="cursor-pointer underline underline-offset-2 hover:text-danger">
                Try again
              </button>
            </div>
          ) : catalogLoading ? (
            <div className="mt-3 text-sm text-secondary">Loading accounts…</div>
          ) : connections.length === 0 ? (
            <div className="mt-3 flex flex-col items-start gap-3 rounded-xl border border-secondary p-4">
              <p className="text-sm text-secondary">No accounts connected yet.</p>
              <button
                type="button"
                onClick={() => openConnectModal('new')}
                className="cursor-pointer rounded-full bg-accent px-3 py-1.5 text-sm font-medium text-accent-foreground transition-colors hover:bg-hover-accent"
              >
                Connect account
              </button>
            </div>
          ) : (
            <div className="mt-3 flex flex-col gap-3">
              {connections.map((conn) => {
                const boundIds = new Set(conn.bindings.map((b) => b.agent_id))
                const unboundAgents = listedAgents.filter((a) => !boundIds.has(a.id))
                return (
                  <div key={conn.connection_id} className="rounded-xl border border-secondary p-4">
                    <div className="flex items-center gap-3">
                      <img
                        src={conn.account_email ? `https://unavatar.io/google/${encodeURIComponent(conn.account_email)}` : icon}
                        alt=""
                        className="h-8 w-8 shrink-0 rounded-full object-cover bg-secondary"
                        onError={(e) => {
                          e.currentTarget.onerror = null
                          e.currentTarget.src = icon
                        }}
                      />
                      <div className="flex min-w-0 flex-1 flex-col">
                        <span className="truncate text-sm font-medium text-primary">{conn.label || conn.account_email || 'Connected account'}</span>
                        <span className="truncate text-xs text-secondary">{conn.account_email || 'No account email on file'}</span>
                      </div>
                      {conn.status !== 'connected' && (
                        <button
                          type="button"
                          onClick={() => openConnectModal('reauth', conn)}
                          className="shrink-0 cursor-pointer rounded-full border border-warning/40 px-2.5 py-1 text-sm font-medium text-warning transition-colors hover:bg-warning/10"
                        >
                          Reconnect
                        </button>
                      )}
                      <button
                        type="button"
                        onClick={() => {
                          setRenameInput(conn.label)
                          setRenameError('')
                          setRenameTarget(conn)
                        }}
                        disabled={busyKey === `preview:${conn.connection_id}` || busyKey === 'disconnect'}
                        aria-label={`Rename ${conn.label || conn.account_email || 'account'}`}
                        className="shrink-0 cursor-pointer rounded-full border border-secondary px-2 py-1 text-sm font-medium text-primary transition-colors hover:bg-hover-secondary disabled:opacity-50"
                      >
                        Rename
                      </button>
                      <button
                        type="button"
                        onClick={() => void requestDisconnect(conn)}
                        disabled={busyKey === `preview:${conn.connection_id}` || busyKey === 'disconnect'}
                        className="shrink-0 cursor-pointer rounded-full border border-danger/40 px-2 py-1 text-sm font-medium text-danger transition-colors hover:bg-danger/10 disabled:opacity-50"
                      >
                        Disconnect
                      </button>
                    </div>
                    <div className="mt-3 pt-3">
                      <div className="flex items-center justify-between gap-2">
                        <span className="text-xs font-medium uppercase tracking-wide text-secondary">Agent access</span>
                        {unboundAgents.length > 0 && (
                          <div
                            ref={openAddAgentConnId === conn.connection_id ? addAgentMenuRef : null}
                            className="relative"
                          >
                            <button
                              type="button"
                              onClick={() =>
                                setOpenAddAgentConnId(
                                  openAddAgentConnId === conn.connection_id ? null : conn.connection_id,
                                )
                              }
                              disabled={busyKey?.startsWith(`binding:${conn.connection_id}`)}
                              className="flex cursor-pointer items-center gap-1 rounded-full border border-secondary bg-secondary px-2 py-1 text-sm text-primary transition-colors hover:bg-hover-secondary disabled:opacity-50"
                            >
                              <span>+ Add agent</span>
                              <IoChevronDownOutline size={14} className="text-secondary" />
                            </button>
                            {openAddAgentConnId === conn.connection_id && (
                              <div className="absolute right-0 z-20 mt-1 min-w-max rounded-xl border border-primary bg-primary p-1 shadow-lg">
                                {unboundAgents.map((agent) => (
                                  <button
                                    key={agent.id}
                                    type="button"
                                    onClick={() => {
                                      setOpenAddAgentConnId(null)
                                      void addBinding(conn, agent.id)
                                    }}
                                    className="flex w-full cursor-pointer items-center whitespace-nowrap rounded-lg px-2 py-1.5 text-left text-sm text-primary transition-colors hover:bg-hover-primary"
                                  >
                                    {agent.name}
                                  </button>
                                ))}
                              </div>
                            )}
                          </div>
                        )}
                      </div>
                      {conn.bindings.length === 0 ? (
                        <p className="mt-2 text-sm text-secondary">No agents are bound to this account yet.</p>
                      ) : (
                        <div className="mt-2 flex flex-col">
                          {conn.bindings.map((binding, index) => (
                            <div
                              key={binding.agent_id}
                              className={`flex items-center gap-2 ${index < conn.bindings.length - 1 ? 'border-b border-secondary py-2' : 'pt-2'}`}
                            >
                              <span className="min-w-0 flex-1 truncate text-sm text-primary">
                                {binding.agent_name} <span className="text-secondary">@{binding.handle}</span>
                              </span>
                              {(() => {
                                const accessKey = `${conn.connection_id}:${binding.agent_id}`
                                const isOpen = openAccessKey === accessKey
                                const current = ACCESS_LEVELS.find((level) => level.value === binding.access_level)?.label ?? 'Draft'
                                const disabled = busyKey === `binding:${conn.connection_id}:${binding.agent_id}`
                                return (
                                  <div
                                    ref={isOpen ? accessMenuRef : null}
                                    className="relative"
                                  >
                                    <button
                                      type="button"
                                      onClick={() => setOpenAccessKey(isOpen ? null : accessKey)}
                                      disabled={disabled}
                                      aria-label={`Access level for ${binding.agent_name}`}
                                      className="flex cursor-pointer items-center gap-1 rounded-full border border-secondary bg-secondary px-2 py-1 text-sm text-primary transition-colors hover:bg-hover-secondary disabled:opacity-50"
                                    >
                                      <span>{current}</span>
                                      <IoChevronDownOutline size={14} className="text-secondary" />
                                    </button>
                                    {isOpen && (
                                      <div className="absolute right-0 z-20 mt-1 min-w-[7rem] rounded-xl border border-primary bg-primary p-1 shadow-lg">
                                        {ACCESS_LEVELS.map((level) => (
                                          <button
                                            key={level.value}
                                            type="button"
                                            onClick={() => {
                                              setOpenAccessKey(null)
                                              if (level.value !== binding.access_level) {
                                                void changeAccess(conn, binding.agent_id, level.value)
                                              }
                                            }}
                                            className={`flex w-full cursor-pointer items-center rounded-lg px-2 py-1.5 text-left text-sm transition-colors hover:bg-hover-primary ${
                                              level.value === binding.access_level ? 'text-primary' : 'text-secondary'
                                            }`}
                                          >
                                            {level.label}
                                          </button>
                                        ))}
                                      </div>
                                    )}
                                  </div>
                                )
                              })()}
                              <button
                                type="button"
                                onClick={() => void removeBinding(conn, binding.agent_id)}
                                disabled={busyKey === `binding:${conn.connection_id}:${binding.agent_id}`}
                                aria-label={`Remove ${binding.agent_name} from this account`}
                                className="flex h-7 w-7 cursor-pointer items-center justify-center rounded-full bg-secondary text-secondary transition-colors hover:bg-hover-secondary hover:text-danger disabled:opacity-50"
                              >
                                <IoClose size={16} />
                              </button>
                            </div>
                          ))}
                        </div>
                      )}
                    </div>
                  </div>
                )
              })}
            </div>
          )}
        </div>
        )}

      </div>

      {connectTarget && (
        <div
          className="fixed inset-0 z-40 flex items-center justify-center bg-black/60 p-4"
          onMouseDown={() => !connectBusy && setConnectTarget(null)}
        >
          <div
            role="dialog"
            aria-modal="true"
            aria-label={connectTarget.mode === 'new' ? `Connect ${name}` : `Reconnect ${name}`}
            className="flex w-full max-w-md flex-col gap-4 rounded-2xl border border-secondary bg-secondary p-5"
            onMouseDown={(e) => e.stopPropagation()}
          >
            <div className="flex items-center justify-between">
              <h3 className="text-sm font-medium text-primary">
                {connectTarget.mode === 'new' ? 'Connect' : 'Reconnect'} {name}
              </h3>
              <button
                type="button"
                onClick={() => setConnectTarget(null)}
                disabled={connectBusy}
                aria-label="Close"
                className="flex h-7 w-7 cursor-pointer items-center justify-center rounded-full text-secondary transition-colors hover:bg-hover-secondary hover:text-primary disabled:opacity-50"
              >
                <IoClose size={16} />
              </button>
            </div>
            <p className="text-xs text-secondary">
              {connectTarget.mode === 'new'
                ? 'Argus opens your browser to sign in. The account is shared machine-wide; choose which agent uses it and how much it may do.'
                : 'Sign in again to refresh this account. Agent bindings and access levels are kept.'}
            </p>
            <div className="flex flex-col gap-3">
              <label className="flex flex-col gap-1 text-xs text-secondary">
                Label{' '}
                {needsLabel ? (
                  <span className="text-secondary">(required — e.g. “Personal”, “Work”)</span>
                ) : (
                  <span className="text-secondary/60">(optional — e.g. “Personal”, “Work”)</span>
                )}
                <input
                  autoFocus
                  value={labelInput}
                  onChange={(e) => setLabelInput(e.target.value)}
                  placeholder={emailInput || 'Account label'}
                  className="rounded-lg border border-secondary bg-primary px-3 py-2 text-sm text-primary outline-none placeholder:text-secondary focus:border-tertiary"
                />
              </label>
              {integration?.oauth_mode === 'google' && (
              <label className="flex flex-col gap-1 text-xs text-secondary">
                Expected email <span className="text-secondary/60">(optional — sign-in is pinned to this account)</span>
                <input
                  type="email"
                  value={emailInput}
                  onChange={(e) => setEmailInput(e.target.value)}
                  placeholder="you@example.com"
                  className="rounded-lg border border-secondary bg-primary px-3 py-2 text-sm text-primary outline-none placeholder:text-secondary focus:border-tertiary"
                />
              </label>
              )}
              <div className="grid grid-cols-2 gap-3">
                <label className="flex flex-col gap-1 text-xs text-secondary">
                  Agent
                  <select
                    value={connectAgent}
                    onChange={(e) => setConnectAgent(e.target.value)}
                    className="cursor-pointer rounded-lg border border-secondary bg-primary px-2 py-2 text-sm text-primary outline-none focus:border-tertiary"
                  >
                    {listedAgents.map((agent) => (
                      <option key={agent.id} value={agent.id}>
                        @{agent.handle} ({agent.name})
                      </option>
                    ))}
                  </select>
                </label>
                <label className="flex flex-col gap-1 text-xs text-secondary">
                  Access
                  <select
                    value={connectAccess}
                    onChange={(e) => setConnectAccess(e.target.value as AccessLevel)}
                    className="cursor-pointer rounded-lg border border-secondary bg-primary px-2 py-2 text-sm text-primary outline-none focus:border-tertiary"
                  >
                    {ACCESS_LEVELS.map((level) => (
                      <option key={level.value} value={level.value}>
                        {level.label}
                      </option>
                    ))}
                  </select>
                </label>
              </div>
            </div>
            {connectError && (
              <p role="alert" className="text-xs text-danger">
                {connectError}
              </p>
            )}
            <div className="flex items-center justify-end gap-2">
              <button
                type="button"
                onClick={() => setConnectTarget(null)}
                disabled={connectBusy}
                className="cursor-pointer rounded-full border border-tertiary px-3 py-1.5 text-sm font-medium text-primary transition-colors hover:bg-hover-secondary disabled:opacity-50"
              >
                Cancel
              </button>
              <button
                type="button"
                onClick={() => void submitConnect()}
                disabled={connectBusy}
                className="cursor-pointer rounded-full bg-accent px-3 py-1.5 text-sm font-medium text-accent-foreground transition-colors hover:bg-hover-accent disabled:opacity-50"
              >
                {connectBusy ? 'Opening browser…' : 'Continue to sign in'}
              </button>
            </div>
          </div>
        </div>
      )}

      {disconnectPreview && (
        <div
          className="fixed inset-0 z-40 flex items-center justify-center bg-black/60 p-4"
          onMouseDown={() => busyKey !== 'disconnect' && setDisconnectPreview(null)}
        >
          <div
            role="dialog"
            aria-modal="true"
            aria-label={`Disconnect ${disconnectPreview.connection.label || 'account'}`}
            className="flex w-full max-w-md flex-col gap-4 rounded-2xl border border-secondary bg-secondary p-5"
            onMouseDown={(e) => e.stopPropagation()}
          >
            <div className="flex items-center justify-between">
              <h3 className="text-sm font-medium text-primary">Disconnect account</h3>
              <button
                type="button"
                onClick={() => setDisconnectPreview(null)}
                disabled={busyKey === 'disconnect'}
                aria-label="Close"
                className="flex h-7 w-7 cursor-pointer items-center justify-center rounded-full text-secondary transition-colors hover:bg-hover-secondary hover:text-primary disabled:opacity-50"
              >
                <IoClose size={16} />
              </button>
            </div>
            <p className="text-sm text-secondary">
              Disconnecting {disconnectPreview.connection.label || disconnectPreview.connection.account_email || 'this account'} stops all its tools.
              {disconnectPreview.connection.bindings.length > 0 ? ' The following agents lose access:' : ' No agents are bound to it.'}
            </p>
            {disconnectPreview.connection.bindings.length > 0 && (
              <ul className="flex flex-col gap-1 rounded-xl border border-secondary bg-primary p-3 text-sm text-primary">
                {disconnectPreview.connection.bindings.map((binding) => (
                  <li key={binding.agent_id} className="flex items-center justify-between gap-2">
                    <span>
                      {binding.agent_name} <span className="text-secondary">@{binding.handle}</span>
                    </span>
                    <span className="text-xs text-secondary">{binding.access_level}</span>
                  </li>
                ))}
              </ul>
            )}
            <div className="flex items-center justify-end gap-2">
              <button
                type="button"
                onClick={() => setDisconnectPreview(null)}
                disabled={busyKey === 'disconnect'}
                className="cursor-pointer rounded-full border border-tertiary px-3 py-1.5 text-sm font-medium text-primary transition-colors hover:bg-hover-secondary disabled:opacity-50"
              >
                Cancel
              </button>
              <button
                type="button"
                onClick={() => void confirmDisconnect()}
                disabled={busyKey === 'disconnect'}
                className="cursor-pointer rounded-full bg-danger px-3 py-1.5 text-sm font-medium text-danger-foreground transition-colors hover:bg-danger/80 disabled:opacity-50"
              >
                {busyKey === 'disconnect' ? 'Disconnecting…' : 'Disconnect'}
              </button>
            </div>
          </div>
        </div>
      )}

      {disconnectAllOpen && (
        <div
          className="fixed inset-0 z-40 flex items-center justify-center bg-black/60 p-4"
          onMouseDown={() => busyKey !== 'disconnectAll' && setDisconnectAllOpen(false)}
        >
          <div
            role="dialog"
            aria-modal="true"
            aria-label={`Disconnect all ${name} accounts`}
            className="flex w-full max-w-md flex-col gap-4 rounded-2xl border border-secondary bg-secondary p-5"
            onMouseDown={(e) => e.stopPropagation()}
          >
            <div className="flex items-center justify-between">
              <h3 className="text-sm font-medium text-primary">Disconnect all accounts</h3>
              <button
                type="button"
                onClick={() => setDisconnectAllOpen(false)}
                disabled={busyKey === 'disconnectAll'}
                aria-label="Close"
                className="flex h-7 w-7 cursor-pointer items-center justify-center rounded-full text-secondary transition-colors hover:bg-hover-secondary hover:text-primary disabled:opacity-50"
              >
                <IoClose size={16} />
              </button>
            </div>
            <p className="text-sm text-secondary">
              This signs out of every {name} account on this machine:
            </p>
            <ul className="flex max-h-48 flex-col gap-1 overflow-y-auto rounded-xl border border-secondary bg-primary p-3 text-sm text-primary">
              {connections.map((conn) => (
                <li key={conn.connection_id} className="flex items-center justify-between gap-2">
                  <span className="truncate">{conn.label || conn.account_email || 'Connected account'}</span>
                  <span className="shrink-0 text-xs text-secondary">
                    {conn.bindings.length} agent{conn.bindings.length === 1 ? '' : 's'} bound
                  </span>
                </li>
              ))}
            </ul>
            <div className="flex items-center justify-end gap-2">
              <button
                type="button"
                onClick={() => setDisconnectAllOpen(false)}
                disabled={busyKey === 'disconnectAll'}
                className="cursor-pointer rounded-full border border-tertiary px-3 py-1.5 text-sm font-medium text-primary transition-colors hover:bg-hover-secondary disabled:opacity-50"
              >
                Cancel
              </button>
              <button
                type="button"
                onClick={() => void confirmDisconnectAll()}
                disabled={busyKey === 'disconnectAll'}
                className="cursor-pointer rounded-full bg-danger px-3 py-1.5 text-sm font-medium text-danger-foreground transition-colors hover:bg-danger/80 disabled:opacity-50"
              >
                {busyKey === 'disconnectAll' ? 'Disconnecting…' : 'Disconnect all'}
              </button>
            </div>
          </div>
        </div>
      )}
      {renameTarget && (
        <div
          className="fixed inset-0 z-40 flex items-center justify-center bg-black/60 p-4"
          onMouseDown={() => !renameBusy && setRenameTarget(null)}
        >
          <div
            role="dialog"
            aria-modal="true"
            aria-label="Rename account"
            className="flex w-full max-w-md flex-col gap-4 rounded-2xl border border-secondary bg-secondary p-5"
            onMouseDown={(e) => e.stopPropagation()}
          >
            <div className="flex items-center justify-between">
              <h3 className="text-sm font-medium text-primary">Rename account</h3>
              <button
                type="button"
                onClick={() => setRenameTarget(null)}
                disabled={renameBusy}
                aria-label="Close"
                className="flex h-7 w-7 cursor-pointer items-center justify-center rounded-full text-secondary transition-colors hover:bg-hover-secondary hover:text-primary disabled:opacity-50"
              >
                <IoClose size={16} />
              </button>
            </div>
            <p className="text-xs text-secondary">
              The name shown for this {name} account on this machine and in agent access lists.
            </p>
            <label className="flex flex-col gap-1 text-xs text-secondary">
              Label
              <input
                autoFocus
                value={renameInput}
                onChange={(e) => setRenameInput(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter' && !renameBusy) void submitRename()
                }}
                placeholder={renameTarget.account_email || 'Account label'}
                className="rounded-lg border border-secondary bg-primary px-3 py-2 text-sm text-primary outline-none placeholder:text-secondary focus:border-tertiary"
              />
            </label>
            {renameError && (
              <p role="alert" className="text-xs text-danger">
                {renameError}
              </p>
            )}
            <div className="flex items-center justify-end gap-2">
              <button
                type="button"
                onClick={() => setRenameTarget(null)}
                disabled={renameBusy}
                className="cursor-pointer rounded-full border border-tertiary px-3 py-1.5 text-sm font-medium text-primary transition-colors hover:bg-hover-secondary disabled:opacity-50"
              >
                Cancel
              </button>
              <button
                type="button"
                onClick={() => void submitRename()}
                disabled={renameBusy}
                className="cursor-pointer rounded-full bg-accent px-3 py-1.5 text-sm font-medium text-accent-foreground transition-colors hover:bg-hover-accent disabled:opacity-50"
              >
                {renameBusy ? 'Saving…' : 'Save'}
              </button>
            </div>
          </div>
        </div>
      )}

      {removeOpen && (
        <div
          className="fixed inset-0 z-40 flex items-center justify-center bg-black/60 p-4"
          onMouseDown={() => !removeBusy && setRemoveOpen(false)}
        >
          <div
            role="dialog"
            aria-modal="true"
            aria-label={`Remove ${name}`}
            className="flex w-full max-w-md flex-col gap-4 rounded-2xl border border-secondary bg-secondary p-5"
            onMouseDown={(e) => e.stopPropagation()}
          >
            <div className="flex items-center justify-between">
              <h3 className="text-sm font-medium text-primary">Remove {name}</h3>
              <button
                type="button"
                onClick={() => setRemoveOpen(false)}
                disabled={removeBusy}
                aria-label="Close"
                className="flex h-7 w-7 cursor-pointer items-center justify-center rounded-full text-secondary transition-colors hover:bg-hover-secondary hover:text-primary disabled:opacity-50"
              >
                <IoClose size={16} />
              </button>
            </div>
            <p className="text-sm text-secondary">
              This forgets the server and stops its tools. Any sessions it holds are signed out.
            </p>
            <div className="flex items-center justify-end gap-2">
              <button
                type="button"
                onClick={() => setRemoveOpen(false)}
                disabled={removeBusy}
                className="cursor-pointer rounded-full border border-tertiary px-3 py-1.5 text-sm font-medium text-primary transition-colors hover:bg-hover-secondary disabled:opacity-50"
              >
                Cancel
              </button>
              <button
                type="button"
                onClick={() => void confirmRemove()}
                disabled={removeBusy}
                className="cursor-pointer rounded-full bg-danger px-3 py-1.5 text-sm font-medium text-danger-foreground transition-colors hover:bg-danger/80 disabled:opacity-50"
              >
                {removeBusy ? 'Removing…' : 'Remove server'}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
