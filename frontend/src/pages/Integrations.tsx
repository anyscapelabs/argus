import { FiChevronLeft } from 'react-icons/fi'

// Placeholder page. The connector list lands here — one-click Gmail, Drive and
// Calendar through the existing connect card — so the page exists while that
// work does not.
export default function Integrations({ onBack }: { onBack?: () => void }) {
  return (
    <div className="flex min-h-0 flex-1 flex-col bg-primary">
      <div className="flex items-center gap-2 px-6 pt-5">
        {onBack && (
          <button
            type="button"
            onClick={onBack}
            className="flex h-8 items-center gap-1 rounded-lg text-grey-300 transition-colors hover:bg-hover-primary hover:text-primary"
          >
            <FiChevronLeft size={16} />
            <span className="text-sm">Back</span>
          </button>
        )}
        <h1 className="text-lg font-medium text-primary">Integrations</h1>
      </div>
      <div className="flex flex-1" />
    </div>
  )
}
