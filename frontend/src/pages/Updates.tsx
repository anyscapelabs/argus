import { useState } from 'react'
import { IoCheckmarkCircle, IoRefresh } from 'react-icons/io5'

export default function Updates() {
  const [checking, setChecking] = useState(false)
  const [lastChecked, setLastChecked] = useState<string | null>(null)

  const handleCheck = () => {
    if (checking) return
    setChecking(true)
    setTimeout(() => {
      setChecking(false)
      setLastChecked(new Date().toLocaleString())
    }, 1200)
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-y-auto bg-primary">
      <div className="mx-auto flex w-full max-w-3xl flex-1 flex-col gap-6 px-6 pb-8">
        <div className="flex flex-col gap-1">
          <h1 className="text-xl font-medium text-primary">Updates</h1>
          <p className="text-sm text-secondary">Check for updates and see version information.</p>
        </div>

        <div className="flex flex-col gap-3">
          <div className="flex items-center justify-between gap-4 rounded-xl border border-primary px-3 py-2">
            <div className="flex flex-col gap-1">
              <span className="text-sm font-medium text-primary">Current version</span>
              <span className="text-sm text-secondary">
                <span className="font-mono text-primary">0.1.0</span>
              </span>
            </div>
            <button
              type="button"
              onClick={handleCheck}
              disabled={checking}
              className="flex shrink-0 cursor-pointer items-center gap-1.5 rounded-full border border-secondary bg-secondary px-2 py-1 text-sm font-medium text-primary transition-colors hover:bg-hover-secondary disabled:cursor-not-allowed disabled:opacity-50"
            >
              <IoRefresh size={14} className={checking ? 'animate-spin' : ''} />
              {checking ? 'Checking…' : 'Check for updates'}
            </button>
          </div>

          <div className="rounded-xl border border-primary px-3 py-2">
            <div className="flex items-start gap-2">
              <IoCheckmarkCircle size={18} className="mt-0.5 shrink-0 text-success" />
              <div className="flex flex-col gap-1">
                <span className="text-sm font-medium text-primary">You’re up to date</span>
                <span className="text-sm text-secondary">
                  Argus 0.1.0 is the latest version.
                  {lastChecked ? ` Last checked ${lastChecked}.` : ''}
                </span>
              </div>
            </div>
          </div>
        </div>

        <div className="flex flex-col gap-2">
          <h2 className="text-sm font-medium text-primary">What’s new</h2>
          <div className="rounded-xl border border-primary p-4">
            <p className="text-sm text-secondary">Release notes for this version will appear here.</p>
          </div>
        </div>
      </div>
    </div>
  )
}
