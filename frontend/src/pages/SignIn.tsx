import { openDashboard } from '../domains/account/api'

type SignInProps = {
  onSignIn: () => void
  onCreateAccount: () => void
}

export default function SignIn({ onSignIn, onCreateAccount }: SignInProps) {
  const handleCreate = () => {
    void openDashboard('/console/settings/service-accounts')
    onCreateAccount()
  }

  return (
    <div className="flex flex-1 flex-col items-center justify-center px-6 pb-12">
      <div className="flex w-full max-w-xs flex-col items-center text-center">
        <div className="flex items-center justify-center gap-3">
          <img
            src="/argus-logo.svg"
            alt="Argus"
            className="h-9 w-9 shrink-0 select-none"
          />
          <h1
            className="text-[32px] leading-none tracking-tight text-primary"
            style={{ fontFamily: "'Geist Pixel Square', monospace" }}
          >
            Argus
          </h1>
        </div>

        <p className="mt-3 text-sm leading-relaxed text-secondary">
          Your always-on companion for everyday work.
        </p>

        <div className="mt-8 flex w-full flex-col gap-3">
          <button
            type="button"
            onClick={onSignIn}
            className="flex h-10 w-full cursor-pointer items-center justify-center rounded-full bg-accent px-4 text-sm font-medium text-foreground transition-colors hover:bg-hover-accent"
          >
            Sign in
          </button>

          <p className="pt-1 text-xs text-secondary">
            Don&apos;t have an account?{' '}
            <button
              type="button"
              onClick={handleCreate}
              className="cursor-pointer font-medium text-primary underline decoration-grey-500 underline-offset-4 transition-colors hover:decoration-primary"
            >
              Create one
            </button>
          </p>
        </div>
      </div>
    </div>
  )
}
