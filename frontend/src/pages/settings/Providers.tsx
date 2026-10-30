import { useState, useEffect, useRef } from 'react'
import { BsOpenai, BsGoogle } from 'react-icons/bs'
import { SiAnthropic, SiMistralai, SiPerplexity, SiReplicate, SiOpenrouter, SiOllama, SiDeepseek, SiX, SiVercel } from 'react-icons/si'
import { IoSearch } from 'react-icons/io5'
import { FiCpu } from 'react-icons/fi'
import { useProviders, useValidateAndSaveKey, useRemoveKey } from '../../domains/providers/hooks'
import { useProviderPicker, useSetProviderSelection, useProviderSelection } from '../../domains/models/hooks'

type Provider = {
  id: string
  name: string
  icon?: React.ComponentType<{ size?: number; className?: string }>
  letter?: string
}

const PROVIDERS: Provider[] = [
  { id: 'openrouter', name: 'OpenRouter', icon: SiOpenrouter },
  { id: 'ollama', name: 'Ollama', icon: SiOllama },
  { id: 'baseten', name: 'Baseten', letter: 'B' },
  { id: 'openai', name: 'OpenAI', icon: BsOpenai },
  { id: 'anthropic', name: 'Anthropic', icon: SiAnthropic },
  { id: 'google', name: 'Google Gemini', icon: BsGoogle },
  { id: 'groq', name: 'Groq', letter: 'G' },
  { id: 'mistral', name: 'Mistral', icon: SiMistralai },
  { id: 'cohere', name: 'Cohere', letter: 'C' },
  { id: 'perplexity', name: 'Perplexity', icon: SiPerplexity },
  { id: 'togetherai', name: 'Together AI', letter: 'T' },
  { id: 'replicate', name: 'Replicate', icon: SiReplicate },
  { id: 'deepseek', name: 'DeepSeek', icon: SiDeepseek },
  { id: 'cerebras', name: 'Cerebras', letter: 'C' },
  { id: 'deepinfra', name: 'Deep Infra', letter: 'D' },
  { id: 'xai', name: 'xAI', icon: SiX },
  { id: 'vercel', name: 'Vercel AI Gateway', icon: SiVercel },
]

export default function Providers() {
  const [active, setActive] = useState<string | null>(null)
  const [noAuth, setNoAuth] = useState(false)
  const [apiKey, setApiKey] = useState('')
  const [modelOpen, setModelOpen] = useState(false)
  const [selectedModels, setSelectedModels] = useState<string[]>([])
  const [modelSearch, setModelSearch] = useState('')
  const modelRef = useRef<HTMLDivElement>(null)

  const { data: providerList } = useProviders()
  const saveKey = useValidateAndSaveKey()
  const removeKey = useRemoveKey()
  const setSelection = useSetProviderSelection()
  const { data: selectionResult } = useProviderSelection(active)
  const { data: pickerResult } = useProviderPicker(active, {
    search: modelSearch,
    include_beta: false,
    tools_only: false,
    refresh_live: false,
  })

  const picker = pickerResult?.picker ?? null
  const models = picker?.sections?.flatMap((s) => s.models ?? []) ?? []
  const activeProvider = (providerList ?? []).find((p) => p.id === active)
  const keyHint = activeProvider?.key_hint || picker?.provider?.key_hint || 'sk-...'

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
    if (picker?.provider?.key_last4 === undefined) return
  }, [picker])

  const initializedFor = useRef<string | null>(null)
  useEffect(() => {
    if (!active) {
      initializedFor.current = null
      return
    }
    if (selectionResult && initializedFor.current !== active) {
      initializedFor.current = active
      setSelectedModels(selectionResult.refs ?? [])
    }
  }, [active, selectionResult])

  const handleAddProvider = () => {
    if (!active) return
    saveKey.mutate(
      { providerID: active, key: noAuth ? '' : apiKey },
      {
        onSuccess: (res) => {
          if (res?.error) {
            return
          }
          setApiKey('')
          setNoAuth(false)
          setSelectedModels([])
          setActive(null)
        },
      },
    )
  }

  const toggleModel = (id: string) => {
    const next = selectedModels.includes(id)
      ? selectedModels.filter((x) => x !== id)
      : [...selectedModels, id]
    setSelectedModels(next)
    if (active) {
      setSelection.mutate({ providerID: active, refs: next })
    }
  }

  return (
    <div className="flex flex-1 min-h-0 flex-col bg-primary">
      <div className="mx-auto w-full max-w-2xl p-2">
        <h1 className="text-lg font-medium text-primary">Providers</h1>
        <p className="text-sm text-secondary">Manage your AI providers and API keys</p>
        <div className="mt-4 border-t border-primary" />
        <h2 className="mt-6 text-sm font-medium text-primary">Connected Providers</h2>
        <div className="mt-4 border-t border-primary" />
        {(providerList ?? []).filter((p) => p.configured).map((p) => (
          <div key={p.id} className="flex items-center justify-between py-3">
            <div className="flex items-center gap-2">
              <div className="flex h-8 w-8 items-center justify-center rounded-full bg-hover-primary">
                {(() => {
                  const Icon = PROVIDERS.find((x) => x.id === p.id)?.icon
                  const Letter = PROVIDERS.find((x) => x.id === p.id)?.letter
                  if (Letter) return <span className="text-[10px] font-medium text-primary">{Letter}</span>
                  if (Icon) return <Icon size={14} />
                  return <span className="text-[10px] font-medium text-primary">{p.name.charAt(0).toUpperCase()}</span>
                })()}
              </div>
              <div className="flex flex-col">
                <span className="text-sm text-primary">{p.name}</span>
                <span className="text-xs text-secondary">{p.selected_model_count} models selected • API Key{p.key_last4 ? ` • ••••${p.key_last4}` : ''}</span>
              </div>
            </div>
            <div className="flex items-center gap-2">
              <button
                type="button"
                onClick={() => setActive(p.id)}
                className="rounded-lg bg-hover-primary px-2 py-1 text-xs font-medium text-secondary hover:text-primary"
              >
                Edit
              </button>
              <button
                type="button"
                onClick={() => removeKey.mutate(p.id)}
                className="rounded-lg bg-red-500/10 px-2 py-1 text-xs font-medium text-red-400 hover:bg-red-500/20"
              >
                Remove
              </button>
            </div>
          </div>
        ))}
        {(providerList ?? []).filter((p) => p.configured).length === 0 && (
          <p className="py-3 text-xs text-secondary">No providers connected yet.</p>
        )}
        <div className="border-t border-primary" />
        <h2 className="mt-8 text-sm font-medium text-primary">Add Custom Provider</h2>
        <p className="text-xs text-secondary">Add a custom provider</p>
        <div className="mt-4 border-t border-primary" />
        <div className="mt-4 flex flex-wrap gap-2">
          {PROVIDERS.map(({ id, name, icon: Icon, letter }) => {
            const isActive = active === id
            return (
              <button
                key={id}
                type="button"
                onClick={() => setActive((cur) => (cur === id ? null : id))}
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
              value={apiKey}
              onChange={(e) => setApiKey(e.target.value)}
              placeholder={keyHint}
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
                    {models.map((m: any) => {
                      const isSel = selectedModels.includes(m.ref)
                      return (
                        <div
                          key={m.ref}
                          className="flex items-center justify-between gap-2 rounded-md px-2 py-1 text-xs text-primary transition-colors hover:bg-hover-secondary"
                        >
                          <div className="flex items-center gap-2">
                            <FiCpu size={12} className="text-secondary" />
                            {m.name ?? m.id}
                          </div>
                          <button
                            type="button"
                            onClick={() => toggleModel(m.ref)}
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
            {saveKey.isPending && <span className="text-xs text-secondary">Saving…</span>}
            {saveKey.isError && <span className="text-xs text-red-400">Failed to save key</span>}
            <div className="flex justify-end">
              {activeProvider?.configured ? (
                <button
                  type="button"
                  onClick={() => {
                    setApiKey('')
                    setNoAuth(false)
                    setSelectedModels([])
                    setActive(null)
                  }}
                  className="rounded-full bg-accent px-3 py-1 text-xs font-medium text-black hover:bg-hover-accent"
                >
                  Done
                </button>
              ) : (
                <button
                  type="button"
                  onClick={handleAddProvider}
                  disabled={saveKey.isPending}
                  className="rounded-full bg-accent px-3 py-1 text-xs font-medium text-black hover:bg-hover-accent disabled:opacity-50"
                >
                  Add Provider
                </button>
              )}
            </div>
          </div>
        )}
      </div>
    </div>
  )
}