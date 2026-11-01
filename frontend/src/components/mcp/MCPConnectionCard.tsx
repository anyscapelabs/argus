export default function MCPConnectionCard() {
  return (
    <div className="w-full max-w-sm min-h-10 p-2 rounded-xl border border-secondary bg-secondary flex items-center gap-3">
      <div className="h-10 w-10 rounded-lg border border-secondary overflow-hidden">
        <img src="/gmail.png" alt="Gmail" className="h-full w-full object-cover" />
      </div>
      <div className="flex flex-1 flex-col">
        <span className="text-sm font-medium text-primary">Gmail</span>
        <span className="text-xs text-secondary">Let Argus send, read, and manage emails</span>
      </div>
      <button className="px-3 py-1 rounded-lg bg-accent text-black text-sm font-medium">Connect</button>
    </div>
  )
}
