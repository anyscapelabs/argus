import { useState } from 'react'
import { IoSearch } from 'react-icons/io5'
import { FiCpu } from 'react-icons/fi'
import { useChatModels } from '../../domains/models/hooks'

type ModelPickerProps = {
  selectedRef?: string | null
  onSelect?: (ref: string) => void
  onClose?: () => void
}

export default function ModelPicker({ selectedRef, onSelect, onClose }: ModelPickerProps) {
  const [search, setSearch] = useState('')
  const { data: chatModels = [] } = useChatModels()

  const filtered = chatModels.filter((m) =>
    (m.model?.name ?? m.ref).toLowerCase().includes(search.toLowerCase()),
  )

  const grouped = filtered.reduce<Record<string, typeof chatModels>>((acc, m) => {
    const key = m.provider_name || m.provider_id
    ;(acc[key] ??= []).push(m)
    return acc
  }, {})

  const handleSelect = (ref: string) => {
    onSelect?.(ref)
    onClose?.()
  }

  if (chatModels.length === 0) {
    return (
      <div className="w-58 rounded-xl border border-secondary bg-secondary p-3 text-xs text-secondary shadow-lg">
        No models selected. Configure providers in Settings.
      </div>
    )
  }

  return (
    <div className="w-58 rounded-xl border border-secondary bg-secondary shadow-lg">
      <div className="flex items-center gap-2 border-b border-secondary px-2 py-1">
        <IoSearch size={14} className="shrink-0 text-secondary" />
        <input
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder="Search…"
          className="flex-1 bg-transparent text-sm text-primary placeholder:text-secondary focus:outline-none"
        />
      </div>
      <div className="max-h-64 overflow-y-auto p-1">
        {Object.entries(grouped).map(([provider, models]) => (
          <div key={provider}>
            <div className="px-2 pt-1">
              <span className="text-[10px] font-medium uppercase tracking-widest text-secondary">
                {provider}
              </span>
            </div>
            <div className="flex flex-col">
              {models.map((m) => (
                <button
                  key={m.ref}
                  type="button"
                  onClick={() => handleSelect(m.ref)}
                  className={`flex items-center gap-2 rounded-md px-2 py-1 text-xs transition-colors hover:bg-hover-secondary ${
                    selectedRef === m.ref ? 'bg-hover-secondary text-primary' : 'text-primary'
                  }`}
                >
                  <FiCpu size={12} className="text-secondary" />
                  {m.model?.name ?? m.ref}
                </button>
              ))}
            </div>
          </div>
        ))}
        {filtered.length === 0 && (
          <div className="px-2 py-1 text-xs text-secondary">No matches</div>
        )}
      </div>
    </div>
  )
}