import type { MCPConnectRequestView, MCPConnectState } from '../../domains/mcp/api'

type Props = {
  request: MCPConnectRequestView
  state: MCPConnectState
  error?: string
  onConnect: () => void
  onDeny: () => void
  onCancel: () => void
}

export default function MCPConnectCard({ request, state, error, onConnect, onDeny, onCancel }: Props) {
  const waiting = state === 'authorizing'
  const failed = state === 'failed'
  return (
    <div className="w-full max-w-sm min-h-10 p-2 rounded-xl border border-secondary bg-secondary flex items-center gap-3">
      <div className="h-10 w-10 rounded-lg border border-secondary overflow-hidden">
        <img
          src={request.icon || `/${request.server_id}.png`}
          alt={request.server_name}
          className="h-full w-full object-cover"
        />
      </div>
      <div className="flex flex-1 flex-col">
        <span className="text-sm font-medium text-primary">{request.server_name}</span>
        <span className="text-xs font-medium text-secondary">
          {waiting
            ? 'Finish signing in in your browser...'
            : failed
              ? error || 'Could not connect'
              : request.description || 'Connect this integration'}
        </span>
      </div>
      {waiting ? (
        <button onClick={onCancel} className="px-3 py-1 rounded-full text-sm font-medium text-secondary hover:text-primary cursor-pointer">
          Cancel
        </button>
      ) : (
        <div className="flex items-center gap-2">
          <button onClick={onDeny} className="px-3 py-1 rounded-full text-sm font-medium text-secondary hover:text-primary cursor-pointer">
            {failed ? 'Dismiss' : 'Deny'}
          </button>
          <button onClick={onConnect} className="px-3 py-1 rounded-full bg-accent text-black text-sm font-medium hover:bg-hover-accent cursor-pointer">
            {failed ? 'Retry' : 'Connect'}
          </button>
        </div>
      )}
    </div>
  )
}
