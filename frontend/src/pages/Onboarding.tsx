import { useRef, useState, type FormEvent } from 'react'
import { FiArrowLeft, FiX } from 'react-icons/fi'
import { openDashboard } from '../domains/account/api'
import { usePairing } from '../domains/account/hooks'

type OnboardingProps = {
  onBack: () => void
  onAuthenticated: () => void
}

export default function Onboarding({ onBack, onAuthenticated }: OnboardingProps) {
  const inputRef = useRef<HTMLInputElement>(null)
  const [key, setKey] = useState('')
  const [verifying, setVerifying] = useState(false)
  const { pairing, save } = usePairing(true)

  const busy = save.isPending || verifying

  const connect = async (e?: FormEvent) => {
    e?.preventDefault()
    const trimmed = key.trim()
    if (!trimmed || busy) return

    setVerifying(true)
    try {
      await save.mutateAsync(trimmed)
      onAuthenticated()
    } catch {
      // Surfacing handled via `save.error` / `pairing.message`.
    } finally {
      setVerifying(false)
    }
  }

  const pasteFromClipboard = async () => {
    try {
      const clip = await navigator.clipboard.readText()
      if (!clip) return
      setKey(clip.trim())
      inputRef.current?.focus()
    } catch {
      inputRef.current?.focus()
    }
  }

  const errorMessage = save.isError
    ? save.error instanceof Error
      ? save.error.message
      : String(save.error)
    : pairing.status === 'invalid'
      ? pairing.message
      : null

  return (
    <div className="relative flex flex-1 flex-col items-center justify-center px-6 pb-12">
      <button
        type="button"
        onClick={onBack}
        disabled={busy}
        className="absolute left-4 top-2 flex cursor-pointer items-center gap-1.5 rounded-lg px-2.5 py-1.5 text-xs font-medium text-secondary transition-colors hover:bg-hover-primary hover:text-primary disabled:pointer-events-none disabled:opacity-40"
      >
        <FiArrowLeft size={14} />
        Back
      </button>

      <div className="flex w-full max-w-[360px] flex-col items-center text-center">
        <img
          src="/argus-logo.svg"
          alt="Argus"
          className="h-10 w-10 select-none"
        />

        <h1 className="mt-4 text-xl font-medium tracking-tight text-primary">
          Connect your agent
        </h1>
        <p className="mt-1.5 text-sm leading-relaxed text-secondary">
          Paste a service account key from your Argus console to pair this device.
        </p>

        <form onSubmit={(e) => void connect(e)} className="mt-6 flex w-full flex-col gap-3 text-left">
          <div className="flex flex-col gap-1.5">
            <label htmlFor="service-account-key" className="text-xs font-medium text-secondary">
              Service account key
            </label>

            <div className="flex h-10 items-center gap-2 rounded-xl border border-secondary bg-secondary px-3 transition-colors focus-within:border-grey-500">
              <input
                ref={inputRef}
                id="service-account-key"
                type="text"
                autoFocus
                autoComplete="off"
                spellCheck={false}
                disabled={busy}
                value={key}
                onChange={(e) => setKey(e.target.value)}
                placeholder="argus_pk_..."
                className="min-w-0 flex-1 bg-transparent font-mono text-sm text-primary placeholder:text-grey-500 focus:outline-none disabled:opacity-50"
              />

              {key ? (
                <button
                  type="button"
                  aria-label="Clear key"
                  disabled={busy}
                  onClick={() => {
                    setKey('')
                    inputRef.current?.focus()
                  }}
                  className="flex h-6 w-6 shrink-0 cursor-pointer items-center justify-center rounded-md text-secondary transition-colors hover:bg-hover-secondary hover:text-primary disabled:opacity-40"
                >
                  <FiX size={14} />
                </button>
              ) : (
                <button
                  type="button"
                  disabled={busy}
                  onClick={() => void pasteFromClipboard()}
                  className="shrink-0 cursor-pointer rounded-md px-2 py-1 text-xs font-medium text-secondary transition-colors hover:bg-hover-secondary hover:text-primary disabled:opacity-40"
                >
                  Paste
                </button>
              )}
            </div>
          </div>

          {errorMessage && (
            <p role="alert" className="text-xs leading-relaxed text-red-400">
              {errorMessage}
            </p>
          )}

          <button
            type="submit"
            disabled={busy || !key.trim()}
            className="mt-1 flex h-10 w-full cursor-pointer items-center justify-center rounded-full bg-accent px-4 text-sm font-medium text-foreground transition-colors hover:bg-hover-accent disabled:cursor-not-allowed disabled:opacity-40"
          >
            {busy ? 'Connecting...' : 'Connect'}
          </button>
        </form>

        <p className="mt-4 text-xs text-secondary">
          Need a service account key?{' '}
          <button
            type="button"
            onClick={() => void openDashboard('/console/settings/service-accounts')}
            disabled={busy}
            className="cursor-pointer font-medium text-primary underline decoration-grey-500 underline-offset-4 transition-colors hover:decoration-primary disabled:opacity-40"
          >
            Open dashboard
          </button>
        </p>
      </div>
    </div>
  )
}
