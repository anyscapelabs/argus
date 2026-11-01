import { Browser } from '@wailsio/runtime'
import type { MCPConnectRequestView } from '../../domains/mcp/api'

type MCPConnectCardProps = {
  request: MCPConnectRequestView
  onConnect: () => void
  onDeny: () => void
}

export default function MCPConnectCard({ request, onConnect, onDeny }: MCPConnectCardProps) {
  const handleConnect = () => {
    if (request.auth_url) {
      Browser.OpenURL(request.auth_url).catch(() => {})
    }
    onConnect()
  }

  return (
    <div className="w-full max-w-sm min-h-10 p-2 rounded-xl border border-secondary bg-secondary flex items-center gap-3">
      <div className="h-10 w-10 rounded-lg border border-secondary overflow-hidden">
        <img src={request.icon || '/gmail.png'} alt={request.server_name} className="h-full w-full object-cover" />
      </div>
      <div className="flex flex-1 flex-col">
        <span className="text-sm font-medium text-primary">{request.server_name}</span>
        <span className="text-xs font-medium text-secondary">{request.description || 'Connect this integration'}</span>
      </div>
      <div className="flex items-center gap-2">
        <button onClick={onDeny} className="px-3 py-1 rounded-full text-sm font-medium text-secondary hover:text-primary cursor-pointer">Deny</button>
        <button onClick={handleConnect} className="px-3 py-1 rounded-full bg-accent text-black text-sm font-medium hover:bg-hover-accent cursor-pointer">Connect</button>
      </div>
    </div>
  )
}
