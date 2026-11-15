import * as MCPConnectService from '../../../bindings/github.com/anyscapelabs/argus/Services/MCPConnect/service.js'
import type { ActionResult } from '../../../bindings/github.com/anyscapelabs/argus/Services/Shared/models.js'

export type MCPConnectDecision = 'connect' | 'deny' | 'cancel'

export type MCPConnectState = 'requested' | 'authorizing' | 'connected' | 'failed' | 'denied' | 'expired'

export type MCPConnectRequestView = {
  request_id: string
  agent_id: string
  agent_name?: string
  server_id: string
  server_name: string
  description?: string
  icon?: string
  connection_id?: string
  label?: string
  expected_email?: string
  access_level?: AccessLevel
  // Set once the card has settled, so a restored card shows its last state
  // instead of offering Connect again.
  state?: MCPConnectState
  // Unix milliseconds, the instant the card joined the conversation. The
  // transcript anchors the card here so it does not drift to the bottom as new
  // messages arrive. 0 when the backend did not send one.
  created_at: number
}

export type MCPConnectStatus = {
  request_id: string
  agent_id: string
  server_id: string
  server_name: string
  state: MCPConnectState
  error?: string
  tool_count?: number
  created_at?: number
}

export type MCPConnectDecisionView = {
  request_id: string
  agent_id: string
  decision: MCPConnectDecision
  server_id: string
}

export async function respondMCPConnect(requestID: string, decision: MCPConnectDecision): Promise<ActionResult> {
  const res = await MCPConnectService.Respond(requestID, decision)
  if (res?.error) throw new Error(res.error.message)
  return res
}

export async function pendingMCPConnects(agentID: string): Promise<MCPConnectRequestView[]> {
  const res = await (MCPConnectService as any).Pending(agentID)
  return res ?? []
}

export type IntegrationView = {
  id: string
  name: string
  description?: string
  icon?: string
  connected?: boolean
  connection_id?: string
  connection_count: number
  tools?: string[]
}

export async function listIntegrations(): Promise<IntegrationView[]> {
  const [integrations, catalog] = await Promise.all([
    (MCPConnectService as any).Integrations(),
    listIntegrationCatalog(),
  ])
  const byServer = new Map(catalog.map((item) => [item.server_id, item]))
  return (integrations ?? []).map((item: any) => {
    const accounts = byServer.get(item.id)?.connections ?? []
    return {
      ...item,
      tools: item.tools ?? [],
      connected: accounts.some((account) => account.status === 'connected'),
      connection_id: accounts.length === 1 ? accounts[0].connection_id : undefined,
      connection_count: accounts.length,
    }
  })
}

export async function registerMCPConnect(agentID: string, agentName: string, serverID: string): Promise<string> {
  return (await (MCPConnectService as any).Register(agentID, agentName, serverID, '', '', '', 'draft')) ?? ''
}

export async function disconnectIntegration(serverID: string): Promise<void> {
  const connector = (await listIntegrationCatalog()).find((item) => item.server_id === serverID)
  if (!connector || connector.connections.length === 0) return
  if (connector.connections.length !== 1) {
    throw new Error('This integration has multiple accounts. Disconnect an individual account from the account manager.')
  }
  await disconnectAccount(connector.connections[0].connection_id)
}


export type AccessLevel = 'read' | 'draft' | 'full'

export type AgentBinding = {
  agent_id: string
  handle: string
  agent_name: string
  access_level: AccessLevel
}

export type IntegrationConnection = {
  connection_id: string
  label: string
  account_email: string
  scopes: string
  status: 'connected' | 'needs_reauth' | 'revoked' | 'disconnecting'
  bindings: AgentBinding[]
}

export type ConnectorCatalogItem = {
  server_id: string
  display_name: string
  multi_account: boolean
  connections: IntegrationConnection[]
}

export async function listIntegrationCatalog(): Promise<ConnectorCatalogItem[]> {
  const res = await (MCPConnectService as any).ListCatalog()
  return (res ?? []).map((item: any) => ({
    server_id: item?.server_id ?? '',
    display_name: item?.display_name ?? '',
    multi_account: !!item?.multi_account,
    connections: (item?.connections ?? []).map(normalizeIntegrationConnection),
  }))
}

export async function requestAccountConnection(input: {
  agentID: string
  agentName: string
  serverID: string
  label?: string
  expectedEmail?: string
  connectionID?: string
  accessLevel: AccessLevel
}): Promise<string> {
  return (await (MCPConnectService as any).Register(
    input.agentID,
    input.agentName,
    input.serverID,
    input.label ?? '',
    input.expectedEmail ?? '',
    input.connectionID ?? '',
    input.accessLevel,
  )) ?? ''
}

export async function upsertAccountBinding(connectionID: string, agentID: string, accessLevel: AccessLevel) {
  await MCPConnectService.UpsertBotBinding(connectionID, agentID, accessLevel)
}

export async function removeAccountBinding(connectionID: string, agentID: string) {
  await MCPConnectService.RemoveBotBinding(connectionID, agentID)
}

export async function disconnectAccount(connectionID: string) {
  await (MCPConnectService as any).DisconnectConnection(connectionID)
}

export async function renameAccount(connectionID: string, label: string) {
  await (MCPConnectService as any).RenameConnection(connectionID, label)
}

export type DisconnectImpact = {
  connection: IntegrationConnection
}

function normalizeAccessLevel(level?: string): AccessLevel {
  return level === 'read' || level === 'full' ? level : 'draft'
}

function normalizeAgentBinding(binding: any): AgentBinding {
  return {
    agent_id: binding?.agent_id ?? '',
    handle: binding?.handle ?? '',
    agent_name: binding?.agent_name ?? '',
    access_level: normalizeAccessLevel(binding?.access_level),
  }
}

function normalizeIntegrationConnection(connection: any): IntegrationConnection {
  return {
    connection_id: connection?.connection_id ?? '',
    label: connection?.label ?? '',
    account_email: connection?.account_email ?? '',
    scopes: connection?.scopes ?? '',
    status: connection?.status ?? 'connected',
    bindings: (connection?.bindings ?? []).map(normalizeAgentBinding),
  }
}

export async function previewDisconnect(connectionID: string): Promise<DisconnectImpact> {
  const res = await MCPConnectService.PreviewDisconnect(connectionID)
  return {
    connection: normalizeIntegrationConnection(res.connection),
  }
}

// startAccountConnection opens the browser for an OAuth flow (or binds an
// existing account) and returns the raw Register result so the caller can
// distinguish duplicate labels, unknown connectors and already-bound accounts.
export async function startAccountConnection(input: {
  agentID: string
  agentName: string
  serverID: string
  label?: string
  expectedEmail?: string
  connectionID?: string
  accessLevel: AccessLevel
}): Promise<string> {
  const res = await requestAccountConnection(input)
  if (res !== 'shown' && res !== 'pending') return res
  let request = (await pendingMCPConnects(input.agentID)).find(
    (item) =>
      item.server_id === input.serverID &&
      (input.connectionID ? item.connection_id === input.connectionID : item.state !== 'connected'),
  )
  if (!request && res === 'shown') {
    await new Promise((resolve) => setTimeout(resolve, 150))
    request = (await pendingMCPConnects(input.agentID)).find(
      (item) =>
        item.server_id === input.serverID &&
        (input.connectionID ? item.connection_id === input.connectionID : item.state !== 'connected'),
    )
  }
  if (request) await respondMCPConnect(request.request_id, 'connect')
  return res
}
