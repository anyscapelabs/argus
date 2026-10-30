import { IoSearch } from 'react-icons/io5'
import { FiCpu } from 'react-icons/fi'

const PROVIDER = 'OpenAI'
const MODELS = ['GPT-4o', 'GPT-4o mini', 'GPT-4 Turbo', 'o1-preview']

type ModelPickerProps = {
  onSelect?: (model: string) => void
  onClose?: () => void
}

export default function ModelPicker({ onSelect, onClose }: ModelPickerProps) {
  const handleSelect = (model: string) => {
    onSelect?.(model)
    onClose?.()
  }

  return (
    <div className="w-58 rounded-xl border border-secondary bg-secondary shadow-lg">
      <div className="flex items-center gap-2 border-b border-secondary px-2 py-1">
        <IoSearch size={14} className="shrink-0 text-secondary" />
        <input
          type="text"
          placeholder="Search…"
          className="flex-1 bg-transparent text-sm text-primary placeholder:text-secondary focus:outline-none"
        />
      </div>
      <div className="px-2 pt-1">
        <span className="text-[10px] font-medium uppercase tracking-widest text-secondary">
          {PROVIDER}
        </span>
      </div>
      <div className="flex flex-col p-1">
        {MODELS.map((m) => (
          <button
            key={m}
            type="button"
            onClick={() => handleSelect(m)}
            className="flex items-center gap-2 rounded-md px-2 py-1 text-xs text-primary transition-colors hover:bg-hover-secondary"
          >
            <FiCpu size={12} className="text-secondary" />
            {m}
          </button>
        ))}
      </div>
    </div>
  )
}