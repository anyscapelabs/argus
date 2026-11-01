type MCPConnectionCardProps = {
  icon?: string
  name?: string
  description?: string
  onConnect?: () => void
}

export default function MCPConnectionCard({
  icon = '/gmail.png',
  name = 'Gmail',
  description = 'Let Argus send, read, and manage emails',
  onConnect,
}: MCPConnectionCardProps) {
  return (
    <div className="w-full max-w-sm min-h-10 p-2 rounded-xl border border-secondary bg-secondary flex items-center gap-3">
      <div className="h-10 w-10 rounded-lg border border-secondary overflow-hidden">
        <img src={icon} alt={name} className="h-full w-full object-cover" />
      </div>
      <div className="flex flex-1 flex-col">
        <span className="text-sm font-medium text-primary">{name}</span>
        <span className="text-xs font-medium text-secondary">{description}</span>
      </div>
      <button
        onClick={onConnect}
        className="px-3 py-1 rounded-full bg-accent text-black text-sm font-medium hover:bg-hover-accent cursor-pointer"
      >
        Connect
      </button>
    </div>
  )
}
