import { useState, useEffect, useRef } from 'react'
import { BsOpenai, BsGoogle } from 'react-icons/bs'
import { SiAnthropic, SiMistralai, SiPerplexity, SiReplicate, SiOpenrouter, SiOllama, SiDeepseek, SiX, SiVercel } from 'react-icons/si'
import { IoSearch } from 'react-icons/io5'

type Provider = {
  name: string
  icon?: React.ComponentType<{ size?: number; className?: string }>
  letter?: string
}

const PROVIDERS: Provider[] = [
  { name: 'OpenRouter', icon: SiOpenrouter },
  { name: 'Ollama', icon: SiOllama },
  { name: 'Baseten', letter: 'B' },
  { name: 'OpenAI', icon: BsOpenai },
  { name: 'Anthropic', icon: SiAnthropic },
  { name: 'Google Gemini', icon: BsGoogle },
  { name: 'Groq', letter: 'G' },
  { name: 'Mistral', icon: SiMistralai },
  { name: 'Cohere', letter: 'C' },
  { name: 'Perplexity', icon: SiPerplexity },
  { name: 'Together AI', letter: 'T' },
  { name: 'Replicate', icon: SiReplicate },
  { name: 'Deepseek', icon: SiDeepseek },
  { name: 'Cerebras', letter: 'C' },
  { name: 'DeepInfra', letter: 'D' },
  { name: 'xAI', icon: SiX },
  { name: 'Vercel AI Gateway', icon: SiVercel },
]

export default function Providers() {
  const [active, setActive] = useState<string | null>(null)
  const [noAuth, setNoAuth] = useState(false)
  const [catalog, setCatalog] = useState<Record<string, any> | null>(null)
  const [modelOpen, setModelOpen] = useState(false)
  const [selectedModels, setSelectedModels] = useState<string[]>([])
  const [modelSearch, setModelSearch] = useState('')
  const modelRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    const handler = (e: MouseEvent) => {
      if (modelRef.current && !modelRef.current.contains(e.target as Node)) {
        setModelOpen(false)
      }
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [])

  useEffect(() => {
    let cancelled = false
    fetch('https://models.dev/api.json')
      .then((r) => r.json())
      .then((data) => {
        if (!cancelled) setCatalog(data)
      })
      .catch(() => {
        if (!cancelled) setCatalog(null)
      })
    return () => {
      cancelled = true
    }
  }, [])

  const normalize = (s: string) => s.toLowerCase().replace(/[^a-z0-9]/g, '')
  const providerEntry = catalog && active
    ? Object.entries(catalog).find(([key, p]) => {
        const name = normalize(String(p?.name ?? ''))
        const target = normalize(active)
        const keyNorm = normalize(key)
        return name === target || keyNorm === target || name.includes(target) || target.includes(name)
      })
    : null
  const models = providerEntry ? Object.values(providerEntry[1]?.models ?? {}) : []

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
        <p className="text-xs text-secondary">Add a custom provider</p>
        <div className="mt-4 border-t border-primary" />
        <div className="mt-4 flex flex-wrap gap-2">
          {PROVIDERS.map(({ name, icon: Icon, letter }) => {
            const isActive = active === name
            return (
              <button
                key={name}
                type="button"
                onClick={() => setActive((cur) => (cur === name ? null : name))}
                className={`flex items-center gap-2 rounded-full border px-3 py-1 text-xs transition-colors ${
                  isActive
                    ? 'border-primary bg-secondary text-primary'
                    : 'border-primary text-secondary hover:bg-hover-primary hover:text-primary'
                }`}
              >
                {letter ? (
                  <span className="flex h-4 w-4 shrink-0 items-center justify-center rounded-lg border border-primary text-[10px] font-medium text-primary">
                    {letter}
                  </span>
                ) : (
                  <span className="flex h-4 w-4 shrink-0 items-center justify-center">
                    {Icon && <Icon size={12} />}
                  </span>
                )}
                <span className="leading-none">{name}</span>
              </button>
            )
          })}
        </div>
        {active && (
          <div className="mt-4 flex flex-col gap-2 p-3">
            <label className="text-xs font-medium text-secondary">API Key</label>
            <input
              type="password"
              placeholder="sk-..."
              className="rounded-md border border-primary bg-primary px-2 py-1 text-sm text-primary placeholder:text-secondary focus:outline-none"
            />
            <div className="flex flex-col gap-0.5">
              <div className="flex items-center gap-2">
                <button
                  type="button"
                  onClick={() => setNoAuth((v) => !v)}
                  className={`relative h-5 w-9 shrink-0 rounded-full transition-colors ${
                    noAuth ? 'bg-accent-secondary' : 'bg-tertiary'
                  }`}
                >
                  <span
                    className={`absolute top-0.5 h-4 w-4 rounded-full bg-white transition-all ${
                      noAuth ? 'left-4' : 'left-0.5'
                    }`}
                  />
                </button>
                <span className="text-xs text-secondary">No auth required for local server</span>
              </div>
              <p className="pl-11 text-[10px] text-secondary">Enable for local servers like Ollama that don't require an API key.</p>
            </div>
            <label className="text-sm font-medium text-secondary">Model</label>
            <div ref={modelRef} className="relative flex items-center gap-2">
              <button
                type="button"
                onClick={() => setModelOpen((v) => !v)}
                className="rounded-full border border-primary px-3 py-1 text-xs text-secondary hover:bg-hover-primary hover:text-primary"
              >
                {selectedModels.length > 0 ? `${selectedModels.length} model${selectedModels.length > 1 ? 's' : ''} selected` : 'Select a model'}
              </button>
              {modelOpen && (
                <div className="absolute left-0 top-full z-50 mt-1 w-64 rounded-xl border border-secondary bg-secondary shadow-lg">
                  <div className="flex items-center gap-2 border-b border-secondary px-2 py-1">
                    <IoSearch size={14} className="shrink-0 text-secondary" />
                    <input
                      type="text"
                      value={modelSearch}
                      onChange={(e) => setModelSearch(e.target.value)}
                      placeholder="Search…"
                      className="flex-1 bg-transparent text-sm text-primary placeholder:text-secondary focus:outline-none"
                    />
                  </div>
                  <div className="flex max-h-50 flex-col overflow-y-auto p-1">
                    {models.length === 0 && (
                      <span className="px-2 py-1 text-xs text-secondary">No models available</span>
                    )}
                    {models
                      .filter((m: any) =>
                        (m.name ?? m.id ?? '').toLowerCase().includes(modelSearch.toLowerCase())
                      )
                      .map((m: any) => {
                        const isSel = selectedModels.includes(m.id)
                        return (
                          <div
                            key={m.id}
                            className="flex items-center justify-between gap-2 rounded-md px-2 py-1 text-xs text-primary transition-colors hover:bg-hover-secondary"
                          >
                            <div className="flex items-center gap-2">
                              <FiCpu size={12} className="text-secondary" />
                              {m.name ?? m.id}
                            </div>
                            <button
                              type="button"
                              onClick={() =>
                                setSelectedModels((prev) =>
                                  isSel ? prev.filter((id) => id !== m.id) : [...prev, m.id]
                                )
                              }
                              className={`relative h-5 w-9 shrink-0 rounded-full transition-colors ${
                                isSel ? 'bg-accent-secondary' : 'bg-tertiary'
                              }`}
                            >
                              <span
                                className={`absolute top-0.5 h-4 w-4 rounded-full bg-white transition-all ${
                                  isSel ? 'left-4' : 'left-0.5'
                                }`}
                              />
                            </button>
                          </div>
                        )
                      })}
                  </div>
                </div>
              )}
            </div>
            <div className="flex justify-end">
              <button
                type="button"
                className="rounded-full bg-accent px-3 py-1 text-xs font-medium text-black hover:bg-hover-accent"
              >
                Add Provider
              </button>
            </div>
          </div>
        )}
      </div>
    </div>
  )
}
