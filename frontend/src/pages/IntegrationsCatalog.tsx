import React, { useState } from 'react';

export type AccessLevel = 'read' | 'draft' | 'full';

export interface AgentBinding {
  agent_id: string;
  handle: string;
  agent_name: string;
  access_level: AccessLevel;
}

export interface IntegrationConnection {
  connection_id: string;
  label: string;
  account_email: string;
  status: 'connected' | 'expired' | 'error';
  bindings: AgentBinding[];
}

export interface ConnectorCatalogItem {
  server_id: string;
  display_name: string;
  multi_account: boolean;
  connections: IntegrationConnection[];
}

export interface AgentSummary {
  id: string;
  handle: string;
  name: string;
}

interface Props {
  items: ConnectorCatalogItem[];
  agents: AgentSummary[];
  onConnectAccount: (serverId: string, label: string, expectedEmail: string) => Promise<void>;
  onUpsertBinding: (connectionId: string, agentId: string, level: AccessLevel) => Promise<void>;
  onRemoveBinding: (connectionId: string, agentId: string) => Promise<void>;
  onDisconnectAccount: (serverId: string, connectionId: string) => Promise<void>;
}

export function IntegrationsPage({
  items,
  agents,
  onConnectAccount,
  onUpsertBinding,
  onRemoveBinding,
  onDisconnectAccount,
}: Props) {
  const [managedServerId, setManagedServerId] = useState<string | null>(null);
  const [connectModalServer, setConnectModalServer] = useState<ConnectorCatalogItem | null>(null);
  const [labelInput, setLabelInput] = useState('');
  const [emailInput, setEmailInput] = useState('');
  const [busyKey, setBusyKey] = useState<string | null>(null);

  const managedConnector = items.find((c) => c.server_id === managedServerId) ?? null;

  const handleStartConnect = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!connectModalServer) return;
    setBusyKey('oauth');
    try {
      await onConnectAccount(connectModalServer.server_id, labelInput.trim(), emailInput.trim());
      setConnectModalServer(null);
      setLabelInput('');
      setEmailInput('');
    } finally {
      setBusyKey(null);
    }
  };

  return (
    <div className="p-6 space-y-6">
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
        {items.map((item) => {
          const hasConnections = item.connections.length > 0;
          const uniqueHandles = Array.from(
            new Set(item.connections.flatMap((c) => c.bindings.map((b) => `@${b.handle}`)))
          );

          return (
            <div
              key={item.server_id}
              className="rounded-xl border border-white/10 bg-white/[0.03] backdrop-blur-md p-4 flex flex-col justify-between gap-4"
            >
              <div className="flex items-start justify-between gap-3">
                <div>
                  <h3 className="text-sm font-semibold text-white">{item.display_name}</h3>
                  <p className="text-xs text-zinc-400 mt-0.5">
                    {hasConnections
                      ? `${item.connections.length} account${item.connections.length > 1 ? 's' : ''} connected`
                      : 'Not connected'}
                  </p>
                </div>

                {hasConnections ? (
                  <button
                    onClick={() => setManagedServerId(item.server_id)}
                    className="px-3 py-1.5 text-xs font-medium rounded-lg border border-white/15 bg-white/10 text-white hover:bg-white/15 transition"
                  >
                    Manage
                  </button>
                ) : (
                  <button
                    onClick={() => setConnectModalServer(item)}
                    className="px-3 py-1.5 text-xs font-medium rounded-lg bg-white text-black hover:bg-zinc-200 transition"
                  >
                    Connect
                  </button>
                )}
              </div>

              {hasConnections && (
                <div className="flex flex-wrap gap-1.5 pt-2 border-t border-white/5">
                  {uniqueHandles.length > 0 ? (
                    uniqueHandles.map((handle) => (
                      <span
                        key={handle}
                        className="px-2 py-0.5 text-[11px] font-mono rounded-md bg-white/[0.06] text-zinc-200 border border-white/10"
                      >
                        {handle}
                      </span>
                    ))
                  ) : (
                    <span className="text-[11px] text-zinc-500">No bots assigned</span>
                  )}
                </div>
              )}
            </div>
          );
        })}
      </div>

      {managedConnector && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/75 backdrop-blur-sm p-4">
          <div className="w-full max-w-xl rounded-2xl border border-white/15 bg-[#090d16] p-5 shadow-2xl space-y-5">
            <div className="flex items-center justify-between">
              <div>
                <h2 className="text-base font-semibold text-white">
                  Manage {managedConnector.display_name}
                </h2>
                <p className="text-xs text-zinc-400">
                  Assign accounts to specific bots or remove bot access without disconnecting.
                </p>
              </div>
              <button
                onClick={() => setManagedServerId(null)}
                className="text-xs text-zinc-400 hover:text-white px-2 py-1"
              >
                Close
              </button>
            </div>

            <div className="space-y-4 max-h-[60vh] overflow-y-auto pr-1">
              {managedConnector.connections.map((conn) => {
                const boundIds = new Set(conn.bindings.map((b) => b.agent_id));
                const unboundAgents = agents.filter((a) => !boundIds.has(a.id));

                return (
                  <div
                    key={conn.connection_id}
                    className="rounded-xl border border-white/10 bg-white/[0.03] p-4 space-y-3"
                  >
                    <div className="flex items-center justify-between gap-2">
                      <div>
                        <div className="text-sm font-medium text-white">
                          {conn.label || conn.account_email || 'Connected Account'}
                        </div>
                        {conn.account_email && (
                          <div className="text-xs font-mono text-zinc-400">{conn.account_email}</div>
                        )}
                      </div>
                      <button
                        disabled={busyKey === conn.connection_id}
                        onClick={async () => {
                          setBusyKey(conn.connection_id);
                          try {
                            await onDisconnectAccount(managedConnector.server_id, conn.connection_id);
                          } finally {
                            setBusyKey(null);
                          }
                        }}
                        className="px-2.5 py-1 text-[11px] rounded-md border border-red-500/30 text-red-300 hover:bg-red-500/15"
                      >
                        Revoke Account
                      </button>
                    </div>

                    <div className="space-y-2">
                      {conn.bindings.length === 0 && (
                        <p className="text-xs text-zinc-500">Unassigned — attach a bot below.</p>
                      )}
                      {conn.bindings.map((bind) => (
                        <div
                          key={bind.agent_id}
                          className="flex items-center justify-between gap-2 rounded-lg bg-black/40 border border-white/5 px-3 py-2"
                        >
                          <div className="flex items-center gap-2">
                            <span className="text-xs font-mono text-white">@{bind.handle}</span>
                            <span className="text-xs text-zinc-400">{bind.agent_name}</span>
                          </div>
                          <div className="flex items-center gap-2">
                            <select
                              value={bind.access_level}
                              onChange={(e) =>
                                onUpsertBinding(
                                  conn.connection_id,
                                  bind.agent_id,
                                  e.target.value as AccessLevel
                                )
                              }
                              className="rounded-md bg-zinc-900 border border-white/10 px-2 py-1 text-xs text-zinc-200"
                            >
                              <option value="read">Read</option>
                              <option value="draft">Draft</option>
                              <option value="full">Full</option>
                            </select>
                            <button
                              onClick={() => onRemoveBinding(conn.connection_id, bind.agent_id)}
                              className="px-2 py-1 text-xs rounded-md bg-white/5 text-zinc-300 hover:bg-red-500/20 hover:text-red-200"
                            >
                              Remove
                            </button>
                          </div>
                        </div>
                      ))}
                    </div>

                    {unboundAgents.length > 0 && (
                      <div className="pt-1 flex items-center gap-2">
                        <select
                          defaultValue=""
                          onChange={(e) => {
                            if (!e.target.value) return;
                            void onUpsertBinding(conn.connection_id, e.target.value, 'draft');
                            e.target.value = '';
                          }}
                          className="w-full rounded-lg bg-zinc-900 border border-white/10 px-3 py-1.5 text-xs text-zinc-300"
                        >
                          <option value="" disabled>
                            + Add bot to this account...
                          </option>
                          {unboundAgents.map((a) => (
                            <option key={a.id} value={a.id}>
                              @{a.handle} ({a.name})
                            </option>
                          ))}
                        </select>
                      </div>
                    )}
                  </div>
                );
              })}
            </div>

            {managedConnector.multi_account && (
              <div className="pt-3 border-t border-white/10 flex justify-end">
                <button
                  onClick={() => {
                    const target = managedConnector;
                    setManagedServerId(null);
                    setConnectModalServer(target);
                  }}
                  className="px-3 py-1.5 text-xs font-medium rounded-lg bg-white text-black hover:bg-zinc-200"
                >
                  + Connect Another Account
                </button>
              </div>
            )}
          </div>
        </div>
      )}

      {connectModalServer && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/75 backdrop-blur-sm p-4">
          <form
            onSubmit={handleStartConnect}
            className="w-full max-w-md rounded-2xl border border-white/15 bg-[#090d16] p-5 space-y-4"
          >
            <h2 className="text-base font-semibold text-white">
              Connect {connectModalServer.display_name}
            </h2>
            <div className="space-y-3">
              <div>
                <label className="block text-xs text-zinc-400 mb-1">Account Label</label>
                <input
                  type="text"
                  placeholder="e.g. Support Inbox or Legal"
                  value={labelInput}
                  onChange={(e) => setLabelInput(e.target.value)}
                  className="w-full rounded-lg bg-black/50 border border-white/10 px-3 py-2 text-xs text-white"
                />
              </div>
              <div>
                <label className="block text-xs text-zinc-400 mb-1">
                  Expected Email (pins Google account selector)
                </label>
                <input
                  type="email"
                  placeholder="support@anyscapelabs.com"
                  value={emailInput}
                  onChange={(e) => setEmailInput(e.target.value)}
                  className="w-full rounded-lg bg-black/50 border border-white/10 px-3 py-2 text-xs text-white"
                />
              </div>
            </div>
            <div className="flex justify-end gap-2 pt-2">
              <button
                type="button"
                onClick={() => setConnectModalServer(null)}
                className="px-3 py-1.5 text-xs text-zinc-400 hover:text-white"
              >
                Cancel
              </button>
              <button
                type="submit"
                disabled={busyKey === 'oauth'}
                className="px-3 py-1.5 text-xs font-medium rounded-lg bg-white text-black"
              >
                Continue to OAuth
              </button>
            </div>
          </form>
        </div>
      )}
    </div>
  );
}
