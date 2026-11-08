import { useRef, useState } from 'react'
import { openDashboard } from '../domains/account/api'
import { usePairing } from '../domains/account/hooks'

type SignInProps = {
  onAuthenticated: () => void
}

// The pairing screen. The argus-router backend mints agent keys on the web
// console; this window stores the pasted key in the OS keyring, proves it by
// calling Entitlements, and then the shell lets the agent screens in.
export default function SignIn({ onAuthenticated }: SignInProps) {
  const inputRef = useRef<HTMLInputElement>(null)
  const [key, setKey] = useState('')
  const { pairing, save } = usePairing(true)
  const [verifying, setVerifying] = useState(false)

  const busy = save.isPending || verifying

  const connect = async () => {
    if (!key.trim()) return
    setVerifying(true)
    try {
      await save.mutateAsync(key)
      onAuthenticated()
    } finally {
      setVerifying(false)
    }
  }

  return (
    <div className="flex flex-1 flex-col items-center justify-center gap-3 text-center">
      <div className="flex flex-col items-center gap-3">
        <img src="/logo.png" alt="Argus" className="h-16 w-16" />
        <h1 className="text-4xl text-primary" style={{ fontFamily: "'Geist Pixel Square'" }}>
          Sign in
        </h1>
      </div>
      <p className="mt-1 max-w-sm text-lg font-medium text-primary">
        Sign in to Argus by pairing this desktop agent with your account.
      </p>

      <div className="mt-4 flex w-full max-w-sm flex-col gap-2">
        <input
          ref={inputRef}
          id="agent-key"
          type="text"
          autoComplete="off"
          spellCheck={false}
          value={key}
          onChange={(e) => setKey(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter') void connect()
          }}
          placeholder="argus_pk_…"
          className="w-full rounded-lg border-none bg-transparent px-3 py-2 text-sm text-grey-100 placeholder-grey-500 outline-none"
        />
        {pairing.status === 'invalid' && (
          <p className="text-left text-sm text-red-400">{pairing.message}</p>
        )}

        <button
          type="button"
          onClick={() => void connect()}
          disabled={busy || !key.trim()}
          className="mt-2 flex cursor-pointer items-center justify-center rounded-full bg-accent px-3.5 py-2 text-sm font-medium text-black transition-colors hover:bg-hover-accent disabled:cursor-not-allowed disabled:opacity-50"
        >
          {busy ? 'Verifying…' : 'Connect'}
        </button>

        <p className="mt-1 text-sm text-grey-300">
          Don&apos;t have a pairing key?{' '}
          <button
            type="button"
            onClick={() => void openDashboard('/console/settings/service-accounts')}
            disabled={busy}
            className="cursor-pointer rounded-md px-1.5 py-0.5 text-grey-100 underline decoration-grey-500 underline-offset-2 transition-colors hover:bg-grey-800 disabled:cursor-not-allowed disabled:opacity-50"
          >
            Create it
          </button>
        </p>
      </div>

    </div>
  )
}
