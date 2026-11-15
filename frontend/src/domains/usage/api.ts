import { listRequests } from '../chat/api'
import { listAgents } from '../models/api'
import type { AgentSummary, ModelRequestView } from '../../../bindings/github.com/anyscapelabs/argus/Services/Shared/models'

// The backend tracks usage per agent, so the account-wide page reads every
// agent's request log and stitches it together. 'default' is always included:
// ListAgents mirrors the sidebar and can be empty before an agent is created.
export interface UsageRows {
  agents: AgentSummary[]
  requestsByAgent: Record<string, ModelRequestView[]>
}

export async function fetchUsageRows(): Promise<UsageRows> {
  const summaries = await listAgents()
  const agents: AgentSummary[] = [...summaries]
  if (!agents.some((a) => a.id === 'default')) {
    agents.unshift({
      id: 'default',
      name: 'Main Agent',
      avatar: '',
      handle: 'default',
      role: '',
      last_message: '',
      last_message_kind: '',
      last_message_at: 0,
    })
  }
  const settled = await Promise.all(
    agents.map(async (agent) => {
      try {
        const res = await listRequests(agent.id, 1000)
        return res?.requests ?? []
      } catch {
        return []
      }
    }),
  )
  const requestsByAgent: Record<string, ModelRequestView[]> = {}
  agents.forEach((agent, i) => {
    requestsByAgent[agent.id] = settled[i]
  })
  return { agents, requestsByAgent }
}
