export default function Providers() {
  return (
    <div className="flex flex-1 min-h-0 flex-col bg-primary">
      <div className="mx-auto w-full max-w-2xl p-2">
        <h1 className="text-lg font-medium text-primary">Providers</h1>
        <p className="text-sm text-secondary">Manage your AI providers and API keys</p>
        <div className="mt-4 border-t border-primary" />
        <h2 className="mt-6 text-sm font-medium text-primary">Connected Providers</h2>
        <div className="mt-4 border-t border-primary" />
        <div className="flex items-center justify-between py-3">
          <div className="flex items-center gap-2">
            <div className="h-8 w-8 rounded-full bg-hover-primary" />
            <div className="flex flex-col">
              <span className="text-sm text-primary">OpenRouter</span>
              <span className="text-xs text-secondary">2 models • API Key</span>
            </div>
          </div>
          <div className="flex items-center gap-2">
            <button className="rounded-lg bg-red-500/10 px-2 py-1 text-xs font-medium text-red-400 hover:bg-red-500/20">Remove</button>
            <button className="rounded-lg bg-hover-primary px-2 py-1 text-xs font-medium text-secondary hover:text-primary">Edit</button>
            <button className="rounded-lg bg-hover-primary px-2 py-1 text-xs font-medium text-secondary hover:text-primary">Clear Keys</button>
          </div>
        </div>
        <div className="border-t border-primary" />
        <h2 className="mt-8 text-sm font-medium text-primary">Add Custom Provider</h2>
        <p className="text-xs text-secondary">Add a custom OpenAI-compatible provider</p>
        <div className="mt-4 border-t border-primary" />
      </div>
    </div>
  )
}
