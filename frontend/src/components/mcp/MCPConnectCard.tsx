import { FiCheck } from 'react-icons/fi'
import { IoAdd } from 'react-icons/io5'
import type { MCPConnectRequestView, MCPConnectState } from '../../domains/mcp/api'

type Props = {
  request: MCPConnectRequestView
  state: MCPConnectState
  error?: string
  onConnect: () => void
  onDeny?: () => void
  onCancel: () => void
  onRemove?: () => void
  denyLabel?: string
  className?: string
}

export default function MCPConnectCard({ request, state, error, onConnect, onDeny, onCancel, onRemove, denyLabel, className }: Props) {
  const waiting = state === 'authorizing'
  const failed = state === 'failed'
  const connected = state === 'connected'
  return (
    <div className={`w-full max-w-sm min-h-10 p-2 rounded-xl border border-secondary bg-secondary flex items-center gap-3 ${className ?? ''}`.trim()}>
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
      {connected ? (
        onRemove ? (
          <div className="flex items-center gap-2">
            <span
              aria-label="Connected"
              title="Connected"
              className="flex h-7 w-7 items-center justify-center rounded-full bg-accent text-black"
            >
              <FiCheck size={14} />
            </span>
            <button
              type="button"
              onClick={onRemove}
              className="cursor-pointer rounded-full px-2 py-1 text-sm font-medium text-secondary hover:text-primary"
            >
              Remove
            </button>
          </div>
        ) : (
          // The card stays on screen after a successful sign-in and the action
          // pill keeps its place, so nothing shifts under the chat.
          <span
            aria-label="Connected"
            title="Connected"
            className="flex h-7 w-7 items-center justify-center rounded-full bg-accent text-black"
          >
            <FiCheck size={14} />
          </span>
        )
      ) : waiting ? (
        <button onClick={onCancel} className="cursor-pointer rounded-full px-2 py-1 text-sm font-medium text-secondary hover:text-primary">
          Cancel
        </button>
      ) : (
        <div className="flex items-center gap-2">
          {onDeny ? (
            <button onClick={onDeny} className="cursor-pointer rounded-full px-2 py-1 text-sm font-medium text-secondary hover:text-primary">
              {denyLabel ?? (failed ? 'Dismiss' : 'Deny')}
            </button>
          ) : null}
          <button
            type="button"
            onClick={onConnect}
            aria-label={failed ? 'Retry' : 'Connect'}
            title={failed ? 'Retry' : 'Connect'}
            className="flex h-7 w-7 cursor-pointer items-center justify-center rounded-full bg-accent text-black hover:bg-hover-accent"
          >
            <IoAdd size={16} />
          </button>
        </div>
      )}
    </div>
  )
}
